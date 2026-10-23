// Package viewcount 聚合文章浏览量，定时批量落库。
//
// 为什么不在请求里直接 views + 1：
//   - 热文每来一个访客就是一条 UPDATE，行锁串行化，把详情页拖成写瓶颈；
//   - 刷新一次算一次，数据本身没有意义（同一个人连点十次就是十次）；
//   - 每次写库都要拿一次连接，连接池会被无谓占用。
//
// 这里的做法是：命中计数先进内存，同 IP + 同文章在窗口期内只算一次，
// 每隔一段时间合并成一条 UPDATE 写回数据库。
//
// 代价是浏览量最多滞后一个 flush 周期，对 CMS 完全可接受。
package viewcount

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FlushFunc 把一批「文章 ID → 增量」写入数据库。
// 返回错误时计数会被放回队列，下一轮重试，不会丢。
type FlushFunc func(deltas map[uint64]int64) error

// Options 计数器配置
type Options struct {
	// Interval flush 间隔，0 取默认 10 秒
	Interval time.Duration
	// Window 同 IP 同文章的计数去重窗口，0 取默认 10 分钟；
	// 传负值表示关闭去重（每个请求都计，仅测试或特殊场景使用）
	Window time.Duration
	// Now 可注入时钟，仅测试使用
	Now func() time.Time
}

// Counter 浏览量聚合计数器，并发安全
type Counter struct {
	flush    FlushFunc
	interval time.Duration
	window   time.Duration
	now      func() time.Time

	mu      sync.Mutex
	pending map[uint64]int64     // 待落库增量
	seen    map[string]time.Time // "postID|ip" → 上次计数时间

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// New 创建计数器。此时尚未开始定时任务，需要显式调用 Run。
func New(flush FlushFunc, opt Options) *Counter {
	if opt.Interval <= 0 {
		opt.Interval = 10 * time.Second
	}
	// 注意：只有 0 才取默认值。负值表示「明确关闭去重」——
	// 早期版本把 0 当作关闭去重，同时又用 0 作为「未设置」的哨兵，
	// 两者冲突导致配置无法关闭去重，测试里 50 次命中被去重成 26 次。
	if opt.Window == 0 {
		opt.Window = 10 * time.Minute
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Counter{
		flush:    flush,
		interval: opt.Interval,
		window:   opt.Window,
		now:      opt.Now,
		pending:  make(map[uint64]int64),
		seen:     make(map[string]time.Time),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Hit 记录一次浏览。
//
// 同 IP 在同一篇文章上的重复访问，窗口期内只计一次（防刷新刷量）。
// 注意 NAT 场景：一个办公室出口 IP 相同，多人看同一篇只会算一次 ——
// 这是有意的取舍，宁可少算也不要被脚本刷成天文数字。
func (c *Counter) Hit(postID uint64, ip string) {
	if postID == 0 {
		return
	}
	now := c.now()
	key := fmt.Sprintf("%d|%s", postID, ip)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.window > 0 {
		if last, ok := c.seen[key]; ok && now.Sub(last) < c.window {
			return
		}
		c.seen[key] = now
	}
	c.pending[postID]++
}

// Flush 立即把待写增量落库，返回成功写入的文章条数。
// 失败时计数回到队列，等下一轮；日志只警告，不影响业务。
func (c *Counter) Flush() int {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return 0
	}
	batch := c.pending
	c.pending = make(map[uint64]int64, len(batch))
	c.mu.Unlock()

	if err := c.flush(batch); err != nil {
		slog.Warn("浏览量落库失败，将重试", "articles", len(batch), "err", err)
		c.mu.Lock()
		for id, d := range batch {
			c.pending[id] += d
		}
		c.mu.Unlock()
		return 0
	}
	return len(batch)
}

// Pending 当前待落库的文章数（诊断用）
func (c *Counter) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// Run 启动定时 flush。需自行放到 goroutine 里：go counter.Run()。
func (c *Counter) Run() {
	t := time.NewTicker(c.interval)
	defer func() {
		t.Stop()
		close(c.done)
	}()
	for {
		select {
		case <-t.C:
			c.Flush()
			c.prune()
		case <-c.stop:
			return
		}
	}
}

// prune 清理过期的去重记录，否则 seen 只增不减（每个 IP×文章一条）。
func (c *Counter) prune() {
	if c.window <= 0 {
		return // 未启用去重，没有记录需要清理
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, t := range c.seen {
		if now.Sub(t) >= c.window {
			delete(c.seen, k)
		}
	}
}

// Close 停止定时任务并做最后一次 flush。
// 进程退出前必须调用，否则最近一个周期的浏览数据会丢。
func (c *Counter) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
	// Run 未启动时 done 永远不会关闭，用 select 兜底避免卡死
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
	}
	if n := c.Flush(); n > 0 {
		slog.Info("已落库最后一批浏览量", "articles", n)
	}
}
