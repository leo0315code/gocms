package handler

import (
	"database/sql"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/config"
	"github.com/example/gocms/internal/middleware"
	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/render"
	"github.com/example/gocms/internal/repository"
	"github.com/example/gocms/internal/security"
	"github.com/example/gocms/internal/utils"
)

// Handler 控制器集合
type Handler struct {
	cfg        *config.Config
	repo       *repository.Repo
	rnd        *render.Engine
	limiter    *middleware.Limiter
	loginLimit *middleware.Limiter

	// ap 后台路径解析器，用于生成正确的跳转地址与模板链接
	ap       *adminpath.Resolver
	reloader reloader
	ipGuard  *middleware.IPGuard

	uploadDir string
	themeRoot string
	webRoot   string

	siteMu     sync.RWMutex
	site       map[string]string
	siteLoaded time.Time
}

// New 创建控制器
func New(cfg *config.Config, repo *repository.Repo, rnd *render.Engine, ap *adminpath.Resolver, uploadDir, themeRoot, webRoot string) *Handler {
	h := &Handler{
		cfg:        cfg,
		repo:       repo,
		rnd:        rnd,
		limiter:    middleware.NewLimiter(5, 10*time.Minute),
		loginLimit: middleware.NewLimiter(5, 15*time.Minute),
		ap:         ap,
		uploadDir:  uploadDir,
		themeRoot:  themeRoot,
		webRoot:    webRoot,
	}
	h.refreshSite()
	return h
}

// adminBase 当前后台前缀，如 "/manage"
func (h *Handler) adminBase() string { return h.ap.URL() }

// reloader 路由重建钩子（由 main 装配，避免 handler 反向依赖 router 包）
type reloader interface{ Reload() }

// SetReloader 注入路由重建器。改后台路径后调用，使新前缀立即生效。
func (h *Handler) SetReloader(r reloader) { h.reloader = r }

// SetIPGuard 注入后台 IP 白名单守卫，使设置页保存后立即生效。
func (h *Handler) SetIPGuard(g *middleware.IPGuard) { h.ipGuard = g }

// CleanupLimits 清理过期限流记录。
// 按账号/按 IP 两个维度的 key 由外部输入决定，不清理会被大量随机用户名撑爆内存。
func (h *Handler) CleanupLimits() {
	h.limiter.Cleanup()
	h.loginLimit.Cleanup()
}

// adminPath 拼接后台子路径
func (h *Handler) adminPath(elem ...string) string { return h.ap.Join(elem...) }

// rebase 把代码里写死的 "/admin/xxx" 转成当前前缀下的真实地址。
//
// 这样 handler 里所有既有跳转都无需逐个改写：仍然可以写 /admin/posts，
// 由这里统一换成 /manage/posts。既减少改动面，也避免手改遗漏。
func (h *Handler) rebase(target string) string {
	base := h.adminBase()
	if base == "/admin" {
		return target
	}
	if target == "/admin" {
		return base
	}
	if strings.HasPrefix(target, "/admin/") || strings.HasPrefix(target, "/admin?") {
		return base + strings.TrimPrefix(target, "/admin")
	}
	return target
}

// refreshSite 刷新站点设置缓存
func (h *Handler) refreshSite() {
	m, err := h.repo.Set.All()
	if err != nil {
		m = map[string]string{}
	}
	h.siteMu.Lock()
	h.site = m
	h.siteLoaded = time.Now()
	h.siteMu.Unlock()
}

// RefreshSite 供外部（保存设置后）调用
func (h *Handler) RefreshSite() { h.refreshSite() }

// siteMap 取站点设置（5 分钟自动过期）
func (h *Handler) siteMap() map[string]string {
	h.siteMu.RLock()
	stale := time.Since(h.siteLoaded) > 5*time.Minute
	m := h.site
	h.siteMu.RUnlock()
	if stale || m == nil {
		h.refreshSite()
		h.siteMu.RLock()
		m = h.site
		h.siteMu.RUnlock()
	}
	return m
}

// ---------- SEO ----------

// SEO SEO 元信息
type SEO struct {
	Title       string
	Keywords    string
	Description string
	Canonical   string
	Image       string
	Type        string
	PublishedAt time.Time
	NoIndex     bool
}

// ---------- 前台视图 ----------

// View 前台页面数据
type View struct {
	Site       map[string]string
	SiteName   string
	SiteURL    string
	Logo       string
	Year       int
	SEO        SEO
	Nav        []*model.MenuItem
	FooterNav  []*model.MenuItem
	SidePages  []*model.Page
	User       *model.User
	CSRF       string
	Flash      *Flash
	CurrentURL string

	// 页面数据
	Posts      []*model.Post
	Post       *model.Post
	Category   *model.Category
	Categories []*model.Category
	CatTree    []*model.Category
	Page       *model.Page
	Banner     []*model.Banner
	Tags       []*model.Tag
	Tag        *model.Tag
	Links      []*model.Link
	Pagination *utils.Pagination
	Query      string
	Prev       *model.Post
	Next       *model.Post
	Related    []*model.Post
	HotPosts   []*model.Post
	Latest     []*model.Post
	Fields     []*model.PostFieldValue
	Breadcrumb []Crumb
	Total      int
	// 表单回显
	Form    map[string]string
	Success string
	Error   string
}

// Crumb 面包屑
type Crumb struct {
	Name string
	URL  string
}

// Flash 提示消息
type Flash struct {
	Type string // success | error | info
	Msg  string
}

func (h *Handler) newView(r *http.Request) *View {
	site := h.siteMap()
	sess := middleware.SessionFrom(r)
	csrf := ""
	if sess != nil {
		csrf = sess.CSRFToken
	}
	nav, _ := h.repo.Menu.ByKey("main")
	footerNav, _ := h.repo.Menu.ByKey("footer")
	pages, _ := h.repo.Page.ListNav()

	v := &View{
		Site:       site,
		SiteName:   site["site_name"],
		SiteURL:    strings.TrimRight(site["site_url"], "/"),
		Logo:       site["site_logo"],
		Year:       time.Now().In(h.cfg.Location).Year(),
		User:       middleware.UserFrom(r),
		CSRF:       csrf,
		SidePages:  pages,
		CurrentURL: r.URL.RequestURI(),
	}
	if v.SiteName == "" {
		v.SiteName = h.cfg.Name
	}
	if nav != nil {
		v.Nav = nav.Items
	}
	if footerNav != nil {
		v.FooterNav = footerNav.Items
	}
	if f := readFlash(r); f != nil {
		v.Flash = f
	}
	return v
}

// ---------- Flash 实现（Cookie） ----------

const flashCookie = "gocms_flash"

func setFlash(w http.ResponseWriter, typ, msg string) {
	if msg == "" {
		return
	}
	// Cookie 值必须是 ASCII 可见字符，中文消息需 URL 编码
	msg = strings.ReplaceAll(msg, "|", " ")
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    typ + "|" + url.QueryEscape(msg),
		Path:     "/",
		HttpOnly: true,
		MaxAge:   60,
		SameSite: http.SameSiteLaxMode,
	})
}

func readFlash(r *http.Request) *Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	parts := strings.SplitN(c.Value, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	msg := parts[1]
	if dec, err := url.QueryUnescape(msg); err == nil {
		msg = dec
	}
	return &Flash{Type: parts[0], Msg: msg}
}

// Redirect 带 flash 重定向
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, url, typ, msg string) {
	setFlash(w, typ, msg)
	// 统一在这里换前缀：handler 里沿用 /admin 字面量，
	// 由 rebase 转成当前后台路径，避免 142 处跳转逐一手改。
	http.Redirect(w, r, h.rebase(url), http.StatusSeeOther)
}

// ---------- 渲染 ----------

// renderFront 渲染前台页面
func (h *Handler) renderFront(w http.ResponseWriter, r *http.Request, tpl string, v *View) {
	if v == nil {
		v = h.newView(r)
	}
	body, err := h.rnd.Render(tpl, v)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "模板渲染失败", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 清除 flash cookie
	if v.Flash != nil {
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	}
	_, _ = w.Write(body)
}

// renderError 渲染错误页
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	v := h.newView(r)
	v.SEO.Title = msg
	v.Error = msg
	if err != nil && h.cfg.IsDev {
		v.Form = map[string]string{"detail": err.Error()}
	}
	body, rerr := h.rnd.Render("error", v)
	if rerr != nil {
		_, _ = w.Write([]byte("<h1>" + strconv.Itoa(status) + " " + msg + "</h1>"))
		return
	}
	_, _ = w.Write(body)
}

// notFound 404
func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "页面不存在", nil)
}

// NotFound 导出给路由兜底使用
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.notFound(w, r)
}

// ---------- 业务辅助 ----------

// userOf 当前后台用户
func userOf(r *http.Request) *model.User {
	return middleware.UserFrom(r)
}

// unboundedPostForm 解析表单（限制体积）
// 注意：解析后会消耗 Body，必须在读取 multipart 之前调用。
func parseForm(w http.ResponseWriter, r *http.Request, maxMB int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMB<<20)
	// 仅 multipart 表单才走 ParseMultipartForm，普通 urlencoded 表单直接 ParseForm
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(32 << 20)
	}
	return r.ParseForm()
}

// saveUpload 保存上传文件，返回相对路径与信息
type uploadResult struct {
	Name   string
	Path   string // 磁盘路径
	URL    string // 访问 URL
	Mime   string
	Ext    string
	Size   int64
	Width  int
	Height int
}

var allowedExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".zip": true, ".rar": true, ".7z": true, ".txt": true, ".mp4": true, ".mp3": true,
}

var mimeByExt = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".pdf": "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".zip":  "application/zip", ".rar": "application/vnd.rar", ".7z": "application/x-7z-compressed",
	".txt": "text/plain", ".mp4": "video/mp4", ".mp3": "audio/mpeg",
}

func (h *Handler) saveUpload(fh *multipart.FileHeader, subDir string) (*uploadResult, error) {
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !allowedExt[ext] {
		return nil, fmt.Errorf("不支持的文件类型：%s", ext)
	}
	if fh.Size > h.cfg.UploadMaxMB<<20 {
		return nil, fmt.Errorf("文件超过 %d MB 限制", h.cfg.UploadMaxMB)
	}

	now := time.Now()
	relDir := filepath.Join(subDir, now.Format("2006/01"))
	absDir := filepath.Join(h.uploadDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	base := security.SlugifyASCII(strings.TrimSuffix(fh.Filename, ext))
	if base == "" {
		base = "file"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	filename := fmt.Sprintf("%s-%s%s", base, security.RandomToken(6), ext)
	absPath := filepath.Join(absDir, filename)

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dst, err := os.Create(absPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(src); err != nil {
		os.Remove(absPath)
		return nil, err
	}

	urlPath := "/uploads/" + filepath.ToSlash(filepath.Join(relDir, filename))
	mime := mimeByExt[ext]
	if mime == "" {
		mime = "application/octet-stream"
	}

	// 图片解析宽高，便于后台列表展示比例
	var width, height int
	if strings.HasPrefix(mime, "image/") && ext != ".svg" {
		if f, err := os.Open(absPath); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				width, height = cfg.Width, cfg.Height
			}
			f.Close()
		}
	}

	return &uploadResult{
		Name:   fh.Filename,
		Path:   absPath,
		URL:    urlPath,
		Mime:   mime,
		Ext:    strings.TrimPrefix(ext, "."),
		Size:   fh.Size,
		Width:  width,
		Height: height,
	}, nil
}

// deleteUploaded 删除磁盘文件（防目录穿越）
func (h *Handler) deleteUploaded(urlPath string) {
	if !strings.HasPrefix(urlPath, "/uploads/") {
		return
	}
	rel := strings.TrimPrefix(urlPath, "/uploads/")
	abs := filepath.Join(h.uploadDir, filepath.FromSlash(rel))
	absClean := filepath.Clean(abs)
	rootClean := filepath.Clean(h.uploadDir)
	if !strings.HasPrefix(absClean, rootClean+string(os.PathSeparator)) {
		return
	}
	_ = os.Remove(absClean)
}

// safeSlug 保证 slug 唯一
func (h *Handler) uniqueSlug(base string, exists func(string) bool) string {
	slug := security.Slugify(base)
	if slug == "" {
		slug = security.RandomToken(4)
	}
	if !exists(slug) {
		return slug
	}
	for i := 2; i < 100; i++ {
		candidate := slug + "-" + strconv.Itoa(i)
		if !exists(candidate) {
			return candidate
		}
	}
	return slug + "-" + security.RandomToken(3)
}

// intOf 安全转 int
func intOf(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// nullTime 空时间
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
