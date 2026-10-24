package router

import (
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
	"github.com/leo0315code/gocms/internal/handler"
	"github.com/leo0315code/gocms/internal/middleware"
	"github.com/leo0315code/gocms/internal/model"
	"github.com/leo0315code/gocms/internal/render"
	"github.com/leo0315code/gocms/internal/repository"
	"github.com/leo0315code/gocms/internal/security"
	"github.com/leo0315code/gocms/internal/seed"
	"github.com/leo0315code/gocms/internal/utils"
)

// 路由级端到端测试：走完整的中间件链（会话 → CSRF → 角色门禁），
// 验证 viewer 只读、uploads CSP、HSTS 三个安全行为在真实装配下生效。
//
// 与其他集成测试同一门控，派生独立库 <库>_router 避免并行互相清库：
//
//	GOCMS_TEST_DSN='root:123456@tcp(127.0.0.1:3306)/gocms_test?parseTime=true&charset=utf8mb4&loc=Local' \
//	  go test ./internal/router/ -v
type testEnv struct {
	repo *repository.Repo
	cfg  *config.Config
	rt   http.Handler
}

func newRouterEnv(t *testing.T, sessionSecure bool) *testEnv {
	t.Helper()
	dsn := os.Getenv("GOCMS_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 GOCMS_TEST_DSN，跳过路由集成测试")
	}
	utils.SetTrustProxy(false)

	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析 GOCMS_TEST_DSN 失败: %v", err)
	}
	dbName := parsed.DBName + "_router"
	noDB := parsed
	noDB.DBName = ""
	if err := database.EnsureDatabase(noDB.FormatDSN(), dbName); err != nil {
		t.Fatalf("创建独立测试库 %s 失败: %v", dbName, err)
	}
	parsed.DBName = dbName

	sqlDB, err := database.Connect(parsed.FormatDSN())
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
		SessionSecure: sessionSecure,
		UploadDir:     t.TempDir(),
	}
	// 已有已发布文章时 seed 只初始化设置、管理员与菜单，不灌演示内容
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
	ap := adminpath.New("admin")
	h := handler.New(cfg, repo, rnd, ap, cfg.UploadDir, "../../themes/default", "../../web")
	rt := Build(cfg, repo, h, ap, middleware.NewIPGuard(), cfg.UploadDir, "../../themes/default", "../../web")
	return &testEnv{repo: repo, cfg: cfg, rt: rt}
}

// mustUser 创建指定角色的用户
func mustUser(t *testing.T, repo *repository.Repo, username, role string) *model.User {
	t.Helper()
	hash, err := security.HashPassword("Passw0rd-x")
	if err != nil {
		t.Fatalf("哈希密码失败: %v", err)
	}
	u := &model.User{Username: username, Password: hash, Role: role, Status: 1}
	if _, err := repo.User.Create(u); err != nil {
		t.Fatalf("创建用户 %s 失败: %v", username, err)
	}
	return u
}

// loginSession 模拟已登录会话：创建会话并绑定用户（token 保持不变，
// 这样 Cookie 直接可用；CSRF token 与会话一致）。
func loginSession(t *testing.T, repo *repository.Repo, u *model.User, sessionName string) (*model.Session, string) {
	t.Helper()
	token := security.RandomToken(32)
	csrf := security.RandomToken(32)
	s := &model.Session{
		Token: token, CSRFToken: csrf, IP: "203.0.113.9",
		UserAgent: "router-test", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Sess.Create(s); err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	exp := time.Now().Add(time.Hour)
	if err := repo.Sess.BindUser(s.ID, u.ID, token, csrf, exp); err != nil {
		t.Fatalf("绑定会话失败: %v", err)
	}
	return s, sessionName + "=" + token
}

func do(t *testing.T, rt http.Handler, method, path, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	req.RemoteAddr = "203.0.113.9:5555"
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	return rec
}

// viewer 是只读角色：页面可看，但任何写 POST 必须在路由层被 403 拦下，
// 无论表单与 CSRF 是否有效——菜单隐藏只是视觉效果，不能当权限用。
func TestViewerIsReadOnly(t *testing.T) {
	env := newRouterEnv(t, false)

	viewer := mustUser(t, env.repo, "watcher", "viewer")
	editor := mustUser(t, env.repo, "writer", "editor")
	vs, vCookie := loginSession(t, env.repo, viewer, env.cfg.SessionName)

	// viewer 能打开列表页
	if rec := do(t, env.rt, "GET", "/admin/posts", vCookie, ""); rec.Code != http.StatusOK {
		t.Errorf("viewer 访问文章列表 = %d，期望 200", rec.Code)
	}

	// viewer 提交写表单：带合法 CSRF 仍必须 403
	form := url.Values{"title": {"越权文章"}, "status": {"published"}, "csrf_token": {vs.CSRFToken}}
	if rec := do(t, env.rt, "POST", "/admin/posts/save", vCookie, form.Encode()); rec.Code != http.StatusForbidden {
		t.Errorf("viewer POST /posts/save = %d，期望 403（body 前 80 字节：%s）",
			rec.Code, truncateStr(rec.Body.String(), 80))
	}

	// editor 同样的请求应正常进入 handler（303 跳转）
	es, eCookie := loginSession(t, env.repo, editor, env.cfg.SessionName)
	form.Set("csrf_token", es.CSRFToken)
	if rec := do(t, env.rt, "POST", "/admin/posts/save", eCookie, form.Encode()); rec.Code != http.StatusSeeOther {
		t.Errorf("editor POST /posts/save = %d，期望 303", rec.Code)
	}

	// 未登录 GET → 跳登录页；匿名 POST 则先被 CSRF 层拦下（同样被拒绝）
	if rec := do(t, env.rt, "GET", "/admin/posts", "", ""); rec.Code != http.StatusSeeOther {
		t.Errorf("匿名 GET /posts = %d，期望 303 跳登录", rec.Code)
	} else if !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login") {
		t.Errorf("匿名 GET 跳转目标 = %q，期望登录页", rec.Header().Get("Location"))
	}

	// viewer 对系统页（用户管理）同样 403；admin 专属路由本就有 RequireRole("admin")
	if rec := do(t, env.rt, "GET", "/admin/users", vCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer GET /users = %d，期望 403", rec.Code)
	}
}

// uploads 直出的文件带文档级 CSP：SVG 内嵌脚本在直接导航时不会执行。
func TestUploadsCSPHeader(t *testing.T) {
	env := newRouterEnv(t, false)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	p := filepath.Join(env.cfg.UploadDir, "probe.svg")
	if err := os.WriteFile(p, []byte(svg), 0o644); err != nil {
		t.Fatalf("写入测试 SVG 失败: %v", err)
	}

	rec := do(t, env.rt, "GET", "/uploads/probe.svg", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /uploads/probe.svg = %d，期望 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("uploads CSP = %q，期望 default-src 'none'", got)
	}

	// 主题静态资源不带该头（内容随主题发布，属可信输入）
	if rec := do(t, env.rt, "GET", "/assets/nonexistent.css", "", ""); rec.Code != http.StatusNotFound {
		t.Logf("/assets/ 探测返回 %d", rec.Code) // 文件不存在，仅确认不panic
	}
}

// viewer 冒烟：遍历全部内容页面，确保只读分支（写控件隐藏后）都能正常渲染，
// 不因模板条件改错而 500。
func TestViewerPagesRender(t *testing.T) {
	env := newRouterEnv(t, false)
	viewer := mustUser(t, env.repo, "watcher", "viewer")
	_, cookie := loginSession(t, env.repo, viewer, env.cfg.SessionName)

	// DELETE 清表不重置自增，ID 只能取真实值
	posts, _, _ := env.repo.Post.List(&repository.PostFilter{AdminView: true})
	if len(posts) == 0 {
		t.Fatal("占位文章缺失")
	}
	menus, _ := env.repo.Menu.List()
	if len(menus) == 0 {
		t.Fatal("种子菜单缺失")
	}

	pages := []string{
		"/admin", "/admin/posts", "/admin/posts/" + strconv.FormatUint(posts[0].ID, 10) + "/edit", "/admin/posts/new",
		"/admin/pages", "/admin/pages/new",
		"/admin/categories", "/admin/categories/new",
		"/admin/tags", "/admin/media", "/admin/menus", "/admin/menus/" + strconv.FormatUint(menus[0].ID, 10),
		"/admin/banners", "/admin/links", "/admin/messages", "/admin/profile",
	}
	for _, p := range pages {
		if rec := do(t, env.rt, "GET", p, cookie, ""); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d，期望 200", p, rec.Code)
		}
	}

	// editor 冒烟一页：写控件分支同样可渲染
	editor := mustUser(t, env.repo, "writer", "editor")
	_, eCookie := loginSession(t, env.repo, editor, env.cfg.SessionName)
	for _, p := range []string{"/admin/posts", "/admin/tags", "/admin/banners", "/admin/links"} {
		if rec := do(t, env.rt, "GET", p, eCookie, ""); rec.Code != http.StatusOK {
			t.Errorf("editor GET %s = %d，期望 200", p, rec.Code)
		}
	}
}

// 在线会话管理仅 admin：页面可见、踢下线生效；editor/viewer 全被 403。
func TestSessionsAdminOnly(t *testing.T) {
	env := newRouterEnv(t, false)

	admin := mustAdminRouter(t, env.repo)
	editor := mustUser(t, env.repo, "writer", "editor")
	viewer := mustUser(t, env.repo, "watcher", "viewer")

	as, aCookie := loginSession(t, env.repo, admin, env.cfg.SessionName)
	_, eCookie := loginSession(t, env.repo, editor, env.cfg.SessionName)
	_, vCookie := loginSession(t, env.repo, viewer, env.cfg.SessionName)

	if rec := do(t, env.rt, "GET", "/admin/sessions", aCookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("admin GET /sessions = %d，期望 200", rec.Code)
	}
	if rec := do(t, env.rt, "GET", "/admin/sessions", eCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("editor GET /sessions = %d，期望 403", rec.Code)
	}
	if rec := do(t, env.rt, "GET", "/admin/sessions", vCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer GET /sessions = %d，期望 403", rec.Code)
	}

	// admin 踢掉 editor 的会话：库里消失，该 Cookie 立即失效
	list, err := env.repo.Sess.ListActive()
	if err != nil {
		t.Fatalf("ListActive 失败: %v", err)
	}
	var editorSessionID uint64
	for _, s := range list {
		if s.UserID == editor.ID {
			editorSessionID = s.ID
		}
	}
	if editorSessionID == 0 {
		t.Fatal("editor 会话不在在线列表中")
	}
	kickForm := url.Values{"csrf_token": {as.CSRFToken}}
	rec := do(t, env.rt, "POST", "/admin/sessions/"+strconv.FormatUint(editorSessionID, 10)+"/kick", aCookie, kickForm.Encode())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("kick = %d，期望 303", rec.Code)
	}
	if _, err := env.repo.Sess.ByToken(strings.TrimPrefix(eCookie, env.cfg.SessionName+"=")); err == nil {
		t.Error("被踢会话仍可用 token 登录")
	}

	// editor 携带合法 CSRF 也不能踢人（admin-only 在角色层拦截）
	es2, eCookie2 := loginSession(t, env.repo, editor, env.cfg.SessionName)
	other, _ := loginSession(t, env.repo, mustUser(t, env.repo, "writer2", "editor"), env.cfg.SessionName)
	kickForm = url.Values{"csrf_token": {es2.CSRFToken}}
	if rec := do(t, env.rt, "POST", "/admin/sessions/"+strconv.FormatUint(other.ID, 10)+"/kick", eCookie2, kickForm.Encode()); rec.Code != http.StatusForbidden {
		t.Errorf("editor kick = %d，期望 403", rec.Code)
	}
}

func mustAdminRouter(t *testing.T, repo *repository.Repo) *model.User {
	t.Helper()
	u, err := repo.User.ByUsername("admin")
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	return u
}

// HSTS 只在 SessionSecure（生产 HTTPS）时下发。
func TestHSTSHeader(t *testing.T) {
	env := newRouterEnv(t, true)
	rec := do(t, env.rt, "GET", "/healthz", "", "")
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("SessionSecure=true 时应下发 Strict-Transport-Security")
	}

	env2 := newRouterEnv(t, false)
	rec2 := do(t, env2.rt, "GET", "/healthz", "", "")
	if got := rec2.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("SessionSecure=false 时不应下发 HSTS，实际 %q", got)
	}
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
