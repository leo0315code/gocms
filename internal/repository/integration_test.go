package repository

import (
	"os"
	"testing"
	"time"

	"github.com/example/gocms/internal/database"
	"github.com/example/gocms/internal/model"
)

// 仓储层集成测试：需要真实 MySQL，因为迁移 SQL 是 MySQL 方言（FULLTEXT、ON DUPLICATE KEY）。
//
// 运行方式（库会被清空，务必用独立的测试库）：
//
//	GOCMS_TEST_DSN='root:123456@tcp(127.0.0.1:3306)/gocms_test?parseTime=true&charset=utf8mb4&loc=Local' \
//	  go test ./internal/repository/ -v
//
// 未设置该变量时全部 Skip，保证没有数据库的环境（CI、新同事本地）仍能跑其余测试。
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("GOCMS_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 GOCMS_TEST_DSN，跳过仓储集成测试")
	}
	sqlDB, err := database.Connect(dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := database.Migrate(sqlDB); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	gdb, err := database.Open(sqlDB, false)
	if err != nil {
		t.Fatalf("初始化 GORM 失败: %v", err)
	}

	r := New(sqlDB, gdb)
	resetAll(t, r)
	return r
}

func resetAll(t *testing.T, r *Repo) {
	t.Helper()
	tables := []string{
		"post_tags", "post_field_values", "posts", "fields",
		"tags", "categories", "audit_logs", "sessions",
	}
	for _, tb := range tables {
		if err := r.Gorm.Exec("DELETE FROM " + tb).Error; err != nil {
			t.Fatalf("清空 %s 失败: %v", tb, err)
		}
	}
}

func mustCategory(t *testing.T, r *Repo, name, slug string, parentID uint64) uint64 {
	t.Helper()
	id, err := r.Cat.Create(&model.Category{
		ParentID: parentID, Name: name, Slug: slug, Type: "post",
	})
	if err != nil {
		t.Fatalf("创建栏目 %s 失败: %v", name, err)
	}
	return id
}

func mustPost(t *testing.T, r *Repo, title, slug string, catID uint64) uint64 {
	t.Helper()
	now := time.Now()
	id, err := r.Post.Create(&model.Post{
		CategoryID: catID, UserID: 1, Title: title, Slug: slug,
		Summary: "摘要", Content: "正文", ContentFormat: "markdown",
		Status: "published", PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("创建文章 %s 失败: %v", title, err)
	}
	return id
}

// 标签同步是最容易写错的地方：重复输入要合并、被移除的标签 count 要回收。
func TestSyncTagsDedupAndCount(t *testing.T) {
	r := newTestRepo(t)
	catID := mustCategory(t, r, "新闻", "news", 0)
	postID := mustPost(t, r, "标题", "post-1", catID)

	// 输入里 A 重复两次，应只落一条
	if err := r.Post.SyncTags(postID, []string{"标签A", "标签B", "标签A"}); err != nil {
		t.Fatalf("SyncTags 失败: %v", err)
	}
	tags, err := r.Post.TagsOf(postID)
	if err != nil {
		t.Fatalf("TagsOf 失败: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("关联标签数 = %d，期望 2（去重后）", len(tags))
	}

	for _, name := range []string{"标签A", "标签B"} {
		tag, err := r.Tag.BySlug(slugOfTag(t, r, name))
		if err != nil {
			t.Fatalf("查询标签 %s 失败: %v", name, err)
		}
		if tag.Count != 1 {
			t.Errorf("标签 %s 的 count = %d，期望 1", name, tag.Count)
		}
	}

	// 改成只留 A，B 的计数必须回收到 0（不能悬空）
	if err := r.Post.SyncTags(postID, []string{"标签A"}); err != nil {
		t.Fatalf("再次 SyncTags 失败: %v", err)
	}
	tags, _ = r.Post.TagsOf(postID)
	if len(tags) != 1 {
		t.Errorf("关联标签数 = %d，期望 1", len(tags))
	}
	b, err := r.Tag.ByID(tagIDOf(t, r, "标签B"))
	if err != nil {
		t.Fatalf("查询标签B 失败: %v", err)
	}
	if b.Count != 0 {
		t.Errorf("被移除后标签B 的 count = %d，期望 0", b.Count)
	}

	// 清空标签
	if err := r.Post.SyncTags(postID, nil); err != nil {
		t.Fatalf("清空标签失败: %v", err)
	}
	if tags, _ := r.Post.TagsOf(postID); len(tags) != 0 {
		t.Errorf("清空后仍有 %d 个标签", len(tags))
	}
}

// 栏目删除必须是一个事务：子栏目上提、文章置未分类、字段清空，缺一不可。
func TestCategoryDeleteCascade(t *testing.T) {
	r := newTestRepo(t)

	parent := mustCategory(t, r, "父栏目", "parent", 0)
	child := mustCategory(t, r, "子栏目", "child", parent)
	postID := mustPost(t, r, "文章", "post-cat", parent)

	if _, err := r.Field.Create(&model.Field{
		CategoryID: parent, Name: "spec", Label: "规格", Type: "text",
	}); err != nil {
		t.Fatalf("创建自定义字段失败: %v", err)
	}

	if err := r.Cat.Delete(parent); err != nil {
		t.Fatalf("删除栏目失败: %v", err)
	}

	// 1. 父栏目消失
	if _, err := r.Cat.ByID(parent); err == nil {
		t.Error("父栏目应已删除")
	}
	// 2. 子栏目上提为顶级，不能被连坐
	got, err := r.Cat.ByID(child)
	if err != nil {
		t.Fatalf("子栏目不应被删除: %v", err)
	}
	if got.ParentID != 0 {
		t.Errorf("子栏目 parent_id = %d，期望 0（上提）", got.ParentID)
	}
	// 3. 文章置为未分类，不能跟着删
	p, err := r.Post.ByID(postID)
	if err != nil {
		t.Fatalf("文章不应被删除: %v", err)
	}
	if p.CategoryID != 0 {
		t.Errorf("文章 category_id = %d，期望 0（未分类）", p.CategoryID)
	}
	// 4. 该栏目的自定义字段清空
	fields, err := r.Field.ListAll()
	if err != nil {
		t.Fatalf("ListAll 失败: %v", err)
	}
	for _, f := range fields {
		if f.CategoryID == parent {
			t.Errorf("栏目 %d 的字段 %q 应被删除", parent, f.Name)
		}
	}
}

// 文章删除要连带清掉标签关联与字段值，否则中间表会留孤儿行。
func TestPostDeleteCleansRelations(t *testing.T) {
	r := newTestRepo(t)
	catID := mustCategory(t, r, "新闻", "news", 0)
	postID := mustPost(t, r, "待删", "post-del", catID)
	if err := r.Post.SyncTags(postID, []string{"X"}); err != nil {
		t.Fatalf("SyncTags: %v", err)
	}
	if err := r.Field.SaveValues(postID, map[uint64]string{1: "v"}); err != nil {
		// 字段不存在时可能报错，不阻塞主线断言
		t.Logf("SaveValues 跳过: %v", err)
	}

	if err := r.Post.Delete(postID); err != nil {
		t.Fatalf("删除文章失败: %v", err)
	}

	var links int64
	if err := r.Gorm.Table("post_tags").Where("post_id = ?", postID).Count(&links).Error; err != nil {
		t.Fatalf("统计中间表失败: %v", err)
	}
	if links != 0 {
		t.Errorf("post_tags 残留 %d 行", links)
	}
	var vals int64
	if err := r.Gorm.Table("post_field_values").Where("post_id = ?", postID).Count(&vals).Error; err != nil {
		t.Fatalf("统计字段值失败: %v", err)
	}
	if vals != 0 {
		t.Errorf("post_field_values 残留 %d 行", vals)
	}
}

// 设置 upsert：同一批写两次不能产生重复行，且值要覆盖（这是「保存设置」的核心行为）。
func TestSettingBatchUpsert(t *testing.T) {
	r := newTestRepo(t)

	if err := r.Set.SetBatch(map[string]string{"site_name": "第一版", "site_url": "http://a"}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if got := r.Set.Get("site_name"); got != "第一版" {
		t.Errorf("site_name = %q，期望 第一版", got)
	}

	if err := r.Set.SetBatch(map[string]string{"site_name": "第二版"}); err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if got := r.Set.Get("site_name"); got != "第二版" {
		t.Errorf("覆盖后 site_name = %q，期望 第二版", got)
	}
	// 未被本批覆盖的键要保留
	if got := r.Set.Get("site_url"); got != "http://a" {
		t.Errorf("site_url 被误删，当前 = %q", got)
	}

	var n int64
	if err := r.Gorm.Table("settings").Where("`key` = ?", "site_name").Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Errorf("settings 中 site_name 出现 %d 行，期望 1（upsert 不能插重复）", n)
	}
}

// 审计写入后要能按操作人 / 动作 / 关键字筛出来，翻页条件不能丢。
func TestAuditCreateAndFilter(t *testing.T) {
	r := newTestRepo(t)

	entries := []*model.AuditLog{
		{Username: "admin", Action: "post.create", Target: "测试文章", Detail: "状态 draft", IP: "127.0.0.1"},
		{Username: "admin", Action: "post.delete", Target: "测试文章", Detail: "文章 ID 9", IP: "127.0.0.1"},
		{Username: "editor", Action: "auth.login", Target: "editor", Detail: "登录成功", IP: "10.0.0.2"},
	}
	for _, e := range entries {
		if err := r.Audit.Create(e); err != nil {
			t.Fatalf("写入审计失败: %v", err)
		}
	}

	list, total, err := r.Audit.List(&AuditFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("总数 = %d / 返回 %d，期望 3", total, len(list))
	}
	// 默认按时间倒序：最新一条在最前
	if list[0].Action != "auth.login" {
		t.Errorf("首条 action = %q，期望最新写入的 auth.login", list[0].Action)
	}

	if _, total, _ := r.Audit.List(&AuditFilter{Username: "admin"}); total != 2 {
		t.Errorf("按操作人筛选结果 = %d，期望 2", total)
	}
	if _, total, _ := r.Audit.List(&AuditFilter{Action: "post.create"}); total != 1 {
		t.Errorf("按动作筛选结果 = %d，期望 1", total)
	}
	if _, total, _ := r.Audit.List(&AuditFilter{Keyword: "测试"}); total != 2 {
		t.Errorf("按关键字筛选结果 = %d，期望 2", total)
	}

	// 清理过期日志不能把新日志带走
	if n := r.Audit.PurgeBefore(time.Now().Add(-time.Hour)); n != 0 {
		t.Errorf("PurgeBefore(过去一小时) 清理了 %d 行，应保留全部", n)
	}
	if n := r.Audit.PurgeBefore(time.Now().Add(time.Hour)); n != 3 {
		t.Errorf("PurgeBefore(未来一小时) 清理 %d 行，期望 3", n)
	}
}

// 浏览量批量累加：一条 UPDATE 处理整批，且必须精确加上增量而不是覆盖。
func TestAddViewsBatch(t *testing.T) {
	r := newTestRepo(t)
	catID := mustCategory(t, r, "新闻", "news", 0)
	p1 := mustPost(t, r, "文章1", "post-v1", catID)
	p2 := mustPost(t, r, "文章2", "post-v2", catID)

	// 先有一批基础值，验证是在原值上累加
	if err := r.Post.AddViews(map[uint64]int64{p1: 7, p2: 3}); err != nil {
		t.Fatalf("首次累加失败: %v", err)
	}
	if err := r.Post.AddViews(map[uint64]int64{p1: 5, p2: 10}); err != nil {
		t.Fatalf("二次累加失败: %v", err)
	}

	a, _ := r.Post.ByID(p1)
	b, _ := r.Post.ByID(p2)
	if a.Views != 12 {
		t.Errorf("文章1 views = %d，期望 12（7+5）", a.Views)
	}
	if b.Views != 13 {
		t.Errorf("文章2 views = %d，期望 13（3+10）", b.Views)
	}

	// 空 map 不应报错也不应执行 SQL
	if err := r.Post.AddViews(nil); err != nil {
		t.Errorf("空增量应静默返回，实际 %v", err)
	}
	// 不存在的 ID 不应影响其他行
	if err := r.Post.AddViews(map[uint64]int64{999999: 5}); err != nil {
		t.Errorf("不存在的 ID 不应报错，实际 %v", err)
	}
	if a, _ := r.Post.ByID(p1); a.Views != 12 {
		t.Errorf("无关更新后文章1 views = %d，期望仍为 12", a.Views)
	}

	// 总浏览量统计要跟着变
	if got := r.Post.TotalViews(); got != 25 {
		t.Errorf("TotalViews = %d，期望 25", got)
	}
}

// 测试搜索：≥2 字符走 ngram FULLTEXT 短语检索（子串命中、草稿不可见），
// 单字符低于 ngram 最小 token 长度应退回 LIKE 而不是空结果或报错。
// 关键字里的双引号不能破坏布尔模式语法。
func TestSearchFulltext(t *testing.T) {
	r := newTestRepo(t)
	catID := mustCategory(t, r, "新闻", "news", 0)
	now := time.Now()
	create := func(title, slug, content, status string) {
		p := &model.Post{
			CategoryID: catID, UserID: 1, Title: title, Slug: slug,
			Summary: "摘要", Content: content, ContentFormat: "html",
			Status: status,
		}
		if status == "published" {
			p.PublishedAt = &now
		}
		if _, err := r.Post.Create(p); err != nil {
			t.Fatalf("创建文章 %s 失败: %v", title, err)
		}
	}
	create("数字化转型实践", "post-ft-1", "本文介绍企业数字化转型的落地路径与工具选型。", "published")
	create("无关文章", "post-ft-2", "今天天气不错，适合出门走走。", "published")
	create("数字化转型草稿", "post-ft-3", "数字化转型", "draft")

	// InnoDB FULLTEXT 对刚提交的行存在极短的可见延迟（并发写入时更明显，
	// 本测试与 handler 包并行跑时可复现）。这里轮询等待而不是立即断言，
	// 等不到才是真回归。
	searchTotal := func(kw string) int {
		t.Helper()
		_, total, err := r.Post.Search(kw, 1, 10)
		if err != nil {
			t.Fatalf("搜索 %q 失败: %v", kw, err)
		}
		return total
	}
	waitHit := func(kw string, want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if got := searchTotal(kw); got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("等待 %q 命中 %d 条超时，当前 %d", kw, want, searchTotal(kw))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// 中文子串命中正文，且草稿不可见
	waitHit("数字化转型", 1)
	list, total, err := r.Post.Search("数字化转型", 1, 10)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Slug != "post-ft-1" {
		t.Fatalf("中文搜索 total = %d，期望仅命中 post-ft-1", total)
	}

	// 关键字带双引号：剥掉后仍按短语检索，不应报错也不应误召回
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, total, err = r.Post.Search(`数字化"转型`, 1, 10)
		if err != nil {
			t.Fatalf("带引号关键字搜索失败: %v", err)
		}
		if total == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if total != 1 {
		t.Errorf("带引号关键字 total = %d，期望 1", total)
	}

	// 单字符退回 LIKE 兜底
	_, total, err = r.Post.Search("气", 1, 10)
	if err != nil {
		t.Fatalf("单字符搜索失败: %v", err)
	}
	if total != 1 {
		t.Errorf("单字符搜索 total = %d，期望 1（LIKE 兜底命中 post-ft-2）", total)
	}
}

// 小工具：按名称找标签的行 ID / slug
func tagIDOf(t *testing.T, r *Repo, name string) uint64 {
	t.Helper()
	var tag model.Tag
	if err := r.Gorm.Where("name = ?", name).First(&tag).Error; err != nil {
		t.Fatalf("查询标签 %s 失败: %v", name, err)
	}
	return tag.ID
}

func slugOfTag(t *testing.T, r *Repo, name string) string {
	t.Helper()
	var tag model.Tag
	if err := r.Gorm.Where("name = ?", name).First(&tag).Error; err != nil {
		t.Fatalf("查询标签 %s 失败: %v", name, err)
	}
	return tag.Slug
}
