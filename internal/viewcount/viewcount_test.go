package viewcount

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// 记录 flush 收到的批次，便于断言
type recorder struct {
	mu      sync.Mutex
	batches []map[uint64]int64
	failN   int // 前 N 次调用返回错误
}

func (r *recorder) Flush(d map[uint64]int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failN > 0 {
		r.failN--
		return errors.New("模拟写库失败")
	}
	cp := make(map[uint64]int64, len(d))
	for k, v := range d {
		cp[k] = v
	}
	r.batches = append(r.batches, cp)
	return nil
}

func (r *recorder) total(postID uint64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, b := range r.batches {
		n += b[postID]
	}
	return n
}

func (r *recorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.batches)
}

func TestHitAccumulatesAndFlushes(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Window: -1}) // Window=0 关闭去重

	c.Hit(1, "1.1.1.1")
	c.Hit(1, "2.2.2.2")
	c.Hit(1, "3.3.3.3")
	c.Hit(2, "1.1.1.1")

	if got := c.Pending(); got != 2 {
		t.Errorf("待落库文章数 = %d，期望 2", got)
	}
	if n := c.Flush(); n != 2 {
		t.Errorf("Flush 返回 %d，期望 2", n)
	}
	if rec.total(1) != 3 {
		t.Errorf("文章 1 累计 = %d，期望 3", rec.total(1))
	}
	if rec.total(2) != 1 {
		t.Errorf("文章 2 累计 = %d，期望 1", rec.total(2))
	}
	if c.Pending() != 0 {
		t.Error("flush 后队列应清空")
	}
	// 空队列不必再打数据库
	if n := c.Flush(); n != 0 {
		t.Errorf("空队列 Flush 返回 %d，期望 0", n)
	}
	if rec.calls() != 1 {
		t.Errorf("数据库调用次数 = %d，期望 1（空队列不应触发）", rec.calls())
	}
}

// 同 IP 刷十次只算一次 —— 这是浏览量能反映「多少人看」的前提
func TestHitDeduplicatesSameIP(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Window: 10 * time.Minute})

	for i := 0; i < 10; i++ {
		c.Hit(1, "1.1.1.1")
	}
	c.Flush()
	if got := rec.total(1); got != 1 {
		t.Errorf("同 IP 刷 10 次后计数 = %d，期望 1", got)
	}

	// 换 IP 视为新访客
	c.Hit(1, "2.2.2.2")
	c.Flush()
	if got := rec.total(1); got != 2 {
		t.Errorf("换 IP 后计数 = %d，期望 2", got)
	}
}

// 窗口过期后同一 IP 再次访问应重新计数
func TestHitWindowExpires(t *testing.T) {
	rec := &recorder{}
	clock := time.Now()
	c := New(rec.Flush, Options{
		Window: 10 * time.Minute,
		Now:    func() time.Time { return clock },
	})

	c.Hit(1, "1.1.1.1")
	c.Flush()
	if got := rec.total(1); got != 1 {
		t.Fatalf("首次计数 = %d，期望 1", got)
	}

	// 窗口内：不计
	clock = clock.Add(5 * time.Minute)
	c.Hit(1, "1.1.1.1")
	c.Flush()
	if got := rec.total(1); got != 1 {
		t.Errorf("窗口内重复访问计成了 %d，期望仍为 1", got)
	}

	// 过期后：计
	clock = clock.Add(6 * time.Minute)
	c.Hit(1, "1.1.1.1")
	c.Flush()
	if got := rec.total(1); got != 2 {
		t.Errorf("窗口过期后计数 = %d，期望 2", got)
	}
}

// 写库失败不能把计数吞掉，否则浏览量会莫名少一截
func TestFlushFailureRetainsCounts(t *testing.T) {
	rec := &recorder{failN: 1}
	c := New(rec.Flush, Options{Window: -1})

	c.Hit(7, "1.1.1.1")
	c.Hit(7, "2.2.2.2")

	if n := c.Flush(); n != 0 {
		t.Errorf("失败时 Flush 返回 %d，期望 0", n)
	}
	if got := c.Pending(); got != 1 {
		t.Errorf("失败后待落库文章数 = %d，期望 1（计数被放回）", got)
	}
	// 重试应成功，且增量不能翻倍
	if n := c.Flush(); n != 1 {
		t.Errorf("重试 Flush 返回 %d，期望 1", n)
	}
	if got := rec.total(7); got != 2 {
		t.Errorf("重试后累计 = %d，期望 2（不能重复累加）", got)
	}
}

func TestHitIgnoresZeroID(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Window: -1})
	c.Hit(0, "1.1.1.1")
	if c.Pending() != 0 {
		t.Error("postID=0 不应计入")
	}
}

// seen 必须定期清理，否则每个 IP×文章都会留一条记录，长期运行会吃内存
func TestPruneClearsExpiredSeen(t *testing.T) {
	clock := time.Now()
	c := New(func(map[uint64]int64) error { return nil }, Options{
		Window: time.Minute,
		Now:    func() time.Time { return clock },
	})

	for i := 0; i < 100; i++ {
		c.Hit(uint64(i+1), "1.1.1.1")
	}
	if got := len(c.seen); got != 100 {
		t.Fatalf("seen 记录数 = %d，期望 100", got)
	}

	clock = clock.Add(2 * time.Minute) // 全部过期
	c.prune()
	if got := len(c.seen); got != 0 {
		t.Errorf("过期清理后 seen = %d，期望 0", got)
	}
}

// 退出前必须落库，否则最近一个周期的数据全丢
func TestCloseFlushes(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Interval: time.Hour, Window: 0})
	go c.Run()

	c.Hit(3, "1.1.1.1")
	c.Close()

	if got := rec.total(3); got != 1 {
		t.Errorf("Close 后累计 = %d，期望 1", got)
	}
}

// Run 未启动时 Close 也不能卡死
func TestCloseWithoutRun(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Window: -1})
	c.Hit(5, "1.1.1.1")

	done := make(chan struct{})
	go func() {
		c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("未启动 Run 时 Close 卡住了")
	}
	if got := rec.total(5); got != 1 {
		t.Errorf("累计 = %d，期望 1", got)
	}
}

// 并发命中不能丢计数（前台是并发场景）
func TestConcurrentHits(t *testing.T) {
	rec := &recorder{}
	c := New(rec.Flush, Options{Window: -1})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Hit(1, "ip-"+string(rune('a'+i%26)))
		}(i)
	}
	wg.Wait()
	c.Flush()

	if got := rec.total(1); got != 50 {
		t.Errorf("并发 50 次累计 = %d，期望 50", got)
	}
}
