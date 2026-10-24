package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/leo0315code/gocms/internal/adminpath"
	"github.com/leo0315code/gocms/internal/config"
	"github.com/leo0315code/gocms/internal/database"
	"github.com/leo0315code/gocms/internal/middleware"
	"github.com/leo0315code/gocms/internal/model"
	"github.com/leo0315code/gocms/internal/render"
	"github.com/leo0315code/gocms/internal/repository"
	"github.com/leo0315code/gocms/internal/security"
	"github.com/leo0315code/gocms/internal/seed"
	"github.com/leo0315code/gocms/internal/utils"
)

// 后台核心写路径的端到端测试：登录 → 发文 → 改设置 → 上传。
// 与仓储集成测试同一套门控（真实 MySQL，跑真正的迁移与种子）：
//
//	GOCMS_TEST_DSN='root:123456@tcp(127.0.0.1:3306)/gocms_test?parseTime=true&charset=utf8mb4&loc=Local' \
//	  go test ./internal/handler/ -run TestWrite -v
//
// 未设置该变量时全部 Skip。注意：这些测试直接调用 handler，
// CSRF / 会话等中间件不在链路上，会话与用户按需注入 context；
// 中间件本身的行为由 middleware 包的测试覆盖。
// deriveTestDSN 从 GOCMS_TEST_DSN 派生一个独立的测试库（<原库名>_writeflow）。
// 写路径测试与仓储集成测试会以两个并行二进制运行（go test ./...），两边都要
// 清空重建表，共用一个库会互相清掉对方正在用的数据，必须分库。
func deriveTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GOCMS_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 GOCMS_TEST_DSN，跳过写路径集成测试")
	}
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析 GOCMS_TEST_DSN 失败: %v", err)
	}
	writeDB := parsed.DBName + "_writeflow"
	noDB := parsed
	noDB.DBName = ""
	if err := database.EnsureDatabase(noDB.FormatDSN(), writeDB); err != nil {
		t.Fatalf("创建独立测试库 %s 失败: %v", writeDB, err)
	}
	parsed.DBName = writeDB
	return parsed.FormatDSN()
}

func newWriteEnv(t *testing.T) (*Handler, *repository.Repo, *config.Config) {
	t.Helper()
	utils.SetTrustProxy(false)

	sqlDB, err := database.Connect(deriveTestDSN(t))
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
	repo := repository.New(sqlDB, gdb)

	// 写路径涉及的表全部清空，保证每个用例从干净状态开始
	for _, tb := range []string{
		"post_tags", "post_field_values", "posts", "fields", "tags", "categories",
		"audit_logs", "sessions", "users", "media", "settings", "menus", "menu_items",
	} {
		if err := repo.Gorm.Exec("DELETE FROM " + tb).Error; err != nil {
			t.Fatalf("清空 %s 失败: %v", tb, err)
		}
	}

	cfg := &config.Config{
		Env: "test", Name: "GoCMS", URL: "http://localhost:8090", IsDev: true,
		Location: time.Local, UploadMaxMB: 10, PageSize: 10,
		AdminUser: "admin", AdminPass: "Admin@123456",
		SessionName: "gocms_test_session", SessionLifetime: time.Hour,
		UploadDir: t.TempDir(),
	}
	// 先放一篇已发布文章，seed 才会跳过演示内容，只初始化设置定义、管理员与菜单
	now := time.Now()
	if _, err := repo.Post.Create(&model.Post{
		Title: "占位文章", Slug: "placeholder", Content: "占位", Status: "published", PublishedAt: &now,
	}); err != nil {
		t.Fatalf("创建占位文章失败: %v", err)
	}
	if err := seed.Run(cfg, repo); err != nil {
		t.Fatalf("初始化种子数据失败: %v", err)
	}

	rnd := render.New("../../themes/default", "../../web/admin/templates", false)
	if err := rnd.Init(); err != nil {
		t.Fatalf("初始化模板失败: %v", err)
	}
	h := New(cfg, repo, rnd, adminpath.New("admin"), cfg.UploadDir, "../../themes/default", "../../web")
	return h, repo, cfg
}

// ---------- 测试工具 ----------

// 注入会话 / 用户。handler 从 context 取，真实请求里由 SessionMiddleware 注入。
func withSession(r *http.Request, s *model.Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.CtxSession, s))
}

func withUser(r *http.Request, u *model.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.CtxUser, u))
}

func mustSession(t *testing.T, repo *repository.Repo) *model.Session {
	t.Helper()
	s := &model.Session{
		Token:     security.RandomToken(32),
		CSRFToken: security.RandomToken(32),
		IP:        "203.0.113.9",
		UserAgent: "writeflow-test",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Sess.Create(s); err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	return s
}

// formRequest 构造 urlencoded POST，显式指定 RemoteAddr 以隔离限流的 IP 维度
func formRequest(remoteAddr string, form url.Values) *http.Request {
	req := httptest.NewRequest("POST", "/admin/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	return req
}

// flashOf 从响应 Cookie 里取出 flash 消息（type|msg），没有则返回空串
func flashOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name != flashCookie {
			continue
		}
		parts := strings.SplitN(c.Value, "|", 2)
		if len(parts) != 2 {
			return parts[0]
		}
		msg, err := url.QueryUnescape(parts[1])
		if err != nil {
			t.Fatalf("flash 消息解码失败: %v", err)
		}
		return parts[0] + "|" + msg
	}
	return ""
}

func cookieValue(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func mustAdmin(t *testing.T, repo *repository.Repo) *model.User {
	t.Helper()
	u, err := repo.User.ByUsername("admin")
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	return u
}

// ---------- 登录 ----------

func TestWriteLoginFlow(t *testing.T) {
	h, repo, cfg := newWriteEnv(t)
	admin := mustAdmin(t, repo)

	// 正确密码：303 跳后台，会话令牌必须轮换（防会话固定）且绑定到该用户
	old := mustSession(t, repo)
	req := formRequest("203.0.113.9", url.Values{"username": {"admin"}, "password": {cfg.AdminPass}})
	rec := httptest.NewRecorder()
	h.LoginSubmit(rec, withSession(req, old))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("登录成功应 303，实际 %d，flash: %q", rec.Code, flashOf(t, rec))
	}
	if loc := rec.Header().Get("Location"); loc != "/admin" {
		t.Errorf("登录成功应跳到 %q，实际 %q", "/admin", loc)
	}
	newToken := cookieValue(t, rec, cfg.SessionName)
	if newToken == "" {
		t.Fatal("登录成功未下发会话 Cookie")
	}
	if newToken == old.Token {
		t.Error("登录成功后仍沿用旧令牌，存在会话固定风险")
	}
	sess, err := repo.Sess.ByToken(newToken)
	if err != nil {
		t.Fatalf("新令牌在库中不存在: %v", err)
	}
	if sess.UserID != admin.ID {
		t.Errorf("会话绑定 user_id = %d，期望 %d", sess.UserID, admin.ID)
	}

	// 错误密码：提示统一为「用户名或密码错误」，不泄露账号是否存在
	req = formRequest("203.0.113.10", url.Values{"username": {"admin"}, "password": {"wrong-pass"}})
	rec = httptest.NewRecorder()
	h.LoginSubmit(rec, withSession(req, mustSession(t, repo)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("登录失败应 303 回登录页，实际 %d", rec.Code)
	}
	if flash := flashOf(t, rec); !strings.Contains(flash, "用户名或密码错误") {
		t.Errorf("错误密码提示 = %q，期望统一的「用户名或密码错误」", flash)
	}

	// 禁用账号：即使密码正确也拒绝，且提示不能是「密码错误」
	hashed, err := security.HashPassword("Disabled@123")
	if err != nil {
		t.Fatalf("哈希密码失败: %v", err)
	}
	if _, err := repo.User.Create(&model.User{
		Username: "locked", Password: hashed, Role: "editor", Status: 0,
	}); err != nil {
		t.Fatalf("创建禁用账号失败: %v", err)
	}
	req = formRequest("203.0.113.11", url.Values{"username": {"locked"}, "password": {"Disabled@123"}})
	rec = httptest.NewRecorder()
	h.LoginSubmit(rec, withSession(req, mustSession(t, repo)))
	if flash := flashOf(t, rec); !strings.Contains(flash, "已被禁用") {
		t.Errorf("禁用账号提示 = %q，期望「该账号已被禁用」", flash)
	}

	// 三个维度限流：全新用户名 + 全新 IP（账号维度跨 IP 共享计数，
	// 上面的用例已经消耗过 admin 的账号配额），连错 5 次后第 6 次在验证前就被拦
	for i := 0; i < 5; i++ {
		req = formRequest("198.51.100.5", url.Values{"username": {"nosuchuser"}, "password": {"wrong-pass"}})
		rec = httptest.NewRecorder()
		h.LoginSubmit(rec, withSession(req, mustSession(t, repo)))
		if flash := flashOf(t, rec); !strings.Contains(flash, "用户名或密码错误") {
			t.Fatalf("第 %d 次尝试应正常失败（未限流），实际 flash: %q", i+1, flash)
		}
	}
	req = formRequest("198.51.100.5", url.Values{"username": {"nosuchuser"}, "password": {"whatever"}})
	rec = httptest.NewRecorder()
	h.LoginSubmit(rec, withSession(req, mustSession(t, repo)))
	if flash := flashOf(t, rec); !strings.Contains(flash, "登录失败次数过多") {
		t.Errorf("第 6 次尝试应被限流，实际 flash: %q", flash)
	}
	// 被限流的请求不得下发任何会话 Cookie
	if tok := cookieValue(t, rec, cfg.SessionName); tok != "" {
		t.Error("被限流的请求不应下发会话 Cookie")
	}
}

// ---------- 发文 ----------

func TestWritePostSave(t *testing.T) {
	h, repo, _ := newWriteEnv(t)
	admin := mustAdmin(t, repo)

	catID, err := repo.Cat.Create(&model.Category{Name: "公司新闻", Slug: "company-news", Type: "post"})
	if err != nil {
		t.Fatalf("创建栏目失败: %v", err)
	}

	form := url.Values{
		"title":          {"GoCMS 发布 v2"},
		"summary":        {"版本发布公告"},
		"content":        {"# 正文标题\n\n正文内容"},
		"content_format": {"markdown"},
		"status":         {"published"},
		"category_id":    {strconv.FormatUint(catID, 10)},
		"tags":           {"Go, 教程, Go"},
	}
	req := formRequest("203.0.113.9", form)
	rec := httptest.NewRecorder()
	h.PostSave(rec, withUser(req, admin))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("发文应 303，实际 %d，flash: %q", rec.Code, flashOf(t, rec))
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts" {
		t.Errorf("保存成功应跳到 /admin/posts，实际 %q", loc)
	}

	p, err := repo.Post.BySlug("gocms-发布-v2")
	if err != nil {
		t.Fatalf("slug 自动生成错误，查不到文章: %v", err)
	}
	if p.Status != "published" || p.ContentFormat != "markdown" {
		t.Errorf("status = %q, format = %q，期望 published / markdown", p.Status, p.ContentFormat)
	}
	if p.CategoryID != catID || p.UserID != admin.ID {
		t.Errorf("category_id = %d (期望 %d), user_id = %d (期望 %d)",
			p.CategoryID, catID, p.UserID, admin.ID)
	}
	if p.PublishedAt == nil {
		t.Error("发布状态的文章未写入发布时间")
	}
	// 标签去重：输入 Go/教程/Go，应只有 2 条关联
	tags, _ := repo.Post.TagsOf(p.ID)
	if len(tags) != 2 {
		t.Errorf("关联标签数 = %d，期望 2（重复输入已合并）", len(tags))
	}
	// 审计日志
	if _, total, _ := repo.Audit.List(&repository.AuditFilter{Action: AuditPostCreate}); total != 1 {
		t.Errorf("post.create 审计记录 = %d 条，期望 1", total)
	}

	// 同名文章 slug 自动避让
	rec = httptest.NewRecorder()
	h.PostSave(rec, withUser(formRequest("203.0.113.9", form), admin))
	if _, err := repo.Post.BySlug("gocms-发布-v2-2"); err != nil {
		t.Errorf("第二篇同名文章应避让为 gocms-发布-v2-2: %v", err)
	}

	// 空标题拒绝
	before, _, _ := repo.Post.List(&repository.PostFilter{AdminView: true})
	rec = httptest.NewRecorder()
	h.PostSave(rec, withUser(formRequest("203.0.113.9", url.Values{"title": {"  "}}), admin))
	if flash := flashOf(t, rec); !strings.Contains(flash, "标题不能为空") {
		t.Errorf("空标题提示 = %q", flash)
	}
	after, _, _ := repo.Post.List(&repository.PostFilter{AdminView: true})
	if len(after) != len(before) {
		t.Errorf("空标题不应新增文章：before %d / after %d", len(before), len(after))
	}
}

// ---------- 改设置 ----------

func TestWriteSettingsSave(t *testing.T) {
	h, repo, cfg := newWriteEnv(t)
	admin := mustAdmin(t, repo)

	// 基本分组：site_name 保存生效并留审计
	req := formRequest("203.0.113.9", url.Values{"group": {"general"}, "setting_site_name": {"新站名"}})
	rec := httptest.NewRecorder()
	h.SettingsSave(rec, withUser(req, admin))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("保存设置应 303，实际 %d，flash: %q", rec.Code, flashOf(t, rec))
	}
	if got := repo.Set.Get("site_name"); got != "新站名" {
		t.Errorf("site_name = %q，期望 新站名", got)
	}
	if _, total, _ := repo.Audit.List(&repository.AuditFilter{Action: AuditSettingsSave}); total != 1 {
		t.Errorf("settings.save 审计记录 = %d 条，期望 1", total)
	}

	// bool 项缺席 = 0：先置 1，再提交不含该字段的 display 分组
	if err := repo.Set.Set("comment_enabled", "1"); err != nil {
		t.Fatalf("预置 comment_enabled 失败: %v", err)
	}
	req = formRequest("203.0.113.9", url.Values{"group": {"display"}})
	rec = httptest.NewRecorder()
	h.SettingsSave(rec, withUser(req, admin))
	if got := repo.Set.Get("comment_enabled"); got != "0" {
		t.Errorf("复选框缺席时 comment_enabled = %q，期望 0", got)
	}
	// 同组非 bool 项缺席时不能被误清（theme 属于 display 组）
	if got := repo.Set.Get("theme"); got != "default" {
		t.Errorf("theme 被误清 = %q，期望 default", got)
	}

	// 改后台路径属于高危项：不输密码 / 密码错误都必须拒绝，值不落库
	secForm := func(confirm string) url.Values {
		f := url.Values{"group": {"security"}, "setting_admin_path": {"manage"}}
		if confirm != "" {
			f.Set("confirm_password", confirm)
		}
		return f
	}
	rec = httptest.NewRecorder()
	h.SettingsSave(rec, withUser(formRequest("203.0.113.9", secForm("")), admin))
	if flash := flashOf(t, rec); !strings.Contains(flash, "需要输入当前密码") {
		t.Errorf("未提供密码的提示 = %q", flash)
	}
	rec = httptest.NewRecorder()
	h.SettingsSave(rec, withUser(formRequest("203.0.113.9", secForm("wrong-pass")), admin))
	if flash := flashOf(t, rec); !strings.Contains(flash, "密码不正确") {
		t.Errorf("密码错误的提示 = %q", flash)
	}
	if h.ap.Path() != "admin" || repo.Set.Get("admin_path") != "" {
		t.Errorf("验证未通过时路径不得变更：ap=%q db=%q", h.ap.Path(), repo.Set.Get("admin_path"))
	}

	// 密码正确：路径立即生效，旧路径进 legacy，重启后旧书签才能跳转
	rec = httptest.NewRecorder()
	h.SettingsSave(rec, withUser(formRequest("203.0.113.9", secForm(cfg.AdminPass)), admin))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("通过验证后应 303，实际 %d，flash: %q", rec.Code, flashOf(t, rec))
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/manage/settings") {
		t.Errorf("改路径后应跳到新前缀 /manage/settings，实际 %q", loc)
	}
	if h.ap.Path() != "manage" {
		t.Errorf("ap.Path() = %q，期望 manage", h.ap.Path())
	}
	if got := repo.Set.Get("admin_path"); got != "manage" {
		t.Errorf("admin_path = %q，期望 manage", got)
	}
	if got := repo.Set.Get("admin_path_legacy"); got != "admin" {
		t.Errorf("admin_path_legacy = %q，期望 admin", got)
	}
}

// ---------- 上传 ----------

// 标准 1x1 PNG，用于验证宽高解析
const png1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func uploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", filename)
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("写入文件内容失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("收尾 multipart 失败: %v", err)
	}
	req := httptest.NewRequest("POST", "/admin/media/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "203.0.113.9"
	return req
}

func TestWriteMediaUpload(t *testing.T) {
	h, repo, cfg := newWriteEnv(t)
	admin := mustAdmin(t, repo)

	png, err := base64.StdEncoding.DecodeString(png1x1Base64)
	if err != nil {
		t.Fatalf("解码测试 PNG 失败: %v", err)
	}

	rec := httptest.NewRecorder()
	h.MediaUpload(rec, withUser(uploadRequest(t, "hello.png", png), admin))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("上传应 303，实际 %d，flash: %q", rec.Code, flashOf(t, rec))
	}
	if flash := flashOf(t, rec); !strings.Contains(flash, "已上传 1 个文件") {
		t.Errorf("上传提示 = %q", flash)
	}
	if got := repo.Media.Count(); got != 1 {
		t.Fatalf("media 记录数 = %d，期望 1", got)
	}
	list, _, _ := repo.Media.List("", 1, 10)
	m := list[0]
	if m.Ext != "png" || m.Mime != "image/png" || m.Width != 1 || m.Height != 1 {
		t.Errorf("媒体信息 = ext:%s mime:%s %dx%d，期望 png/image/png 1x1", m.Ext, m.Mime, m.Width, m.Height)
	}
	if m.UserID != admin.ID {
		t.Errorf("media user_id = %d，期望 %d", m.UserID, admin.ID)
	}
	if _, err := os.Stat(m.Path); err != nil {
		t.Errorf("文件未落盘: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(m.Path), filepath.Clean(cfg.UploadDir)+string(filepath.Separator)) &&
		filepath.Clean(m.Path) != filepath.Clean(cfg.UploadDir) {
		t.Errorf("文件路径 %q 落在上传目录之外", m.Path)
	}

	// 可执行后缀拒绝，不落库不落盘
	rec = httptest.NewRecorder()
	h.MediaUpload(rec, withUser(uploadRequest(t, "evil.exe", []byte("MZ...")), admin))
	if flash := flashOf(t, rec); !strings.Contains(flash, "不支持的文件类型") {
		t.Errorf("可执行文件提示 = %q", flash)
	}
	if got := repo.Media.Count(); got != 1 {
		t.Errorf("被拒文件不应入库，media 记录数 = %d", got)
	}

	// 未选择文件
	rec = httptest.NewRecorder()
	h.MediaUpload(rec, withUser(formRequest("203.0.113.9", url.Values{"x": {"1"}}), admin))
	if flash := flashOf(t, rec); !strings.Contains(flash, "请选择文件") {
		t.Errorf("空上传提示 = %q", flash)
	}
}
