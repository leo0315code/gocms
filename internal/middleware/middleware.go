package middleware

import (
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/config"
	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/repository"
	"github.com/example/gocms/internal/security"
	"github.com/example/gocms/internal/utils"
)

// 上下文 key
type ctxKey string

const (
	CtxSession   ctxKey = "session"
	CtxUser      ctxKey = "user"
	CtxAdminPath ctxKey = "admin_path"
)

// Middleware 中间件签名
type Middleware func(http.Handler) http.Handler

// Chain 顺序应用中间件（先声明的先执行）
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Recover panic 恢复
func Recover(isDev bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					if isDev {
						_, _ = w.Write([]byte("<h1>500 内部错误</h1><pre>" + templateEscape(fmt.Sprint(rec)) + "</pre>"))
					} else {
						_, _ = w.Write([]byte("<h1>500 服务器内部错误</h1>"))
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func templateEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// statusRecorder 记录状态码
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Logger 访问日志
func Logger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			// 静态资源不打日志噪音
			if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/uploads/") {
				return
			}
			slog.Info("http",
				"method", r.Method,
				"path", r.URL.RequestURI(),
				"status", rec.status,
				"bytes", rec.bytes,
				"ip", utils.ClientIP(r),
				"dur", time.Since(start).Round(time.Millisecond).String(),
			)
		})
	}
}

// SecurityHeaders 安全响应头
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			next.ServeHTTP(w, r)
		})
	}
}

// StaticCache 为静态资源设置缓存策略。
//
// 三类资源的内容更新频率不同，不能一刀切：
//   - /uploads/  文件名带时间戳且内容不可变 → 一年 + immutable，浏览器完全不回源
//   - /assets/   主题静态资源，换主题才会变 → 一小时，兼顾体验与更新
//   - 后台 assets 模板改动就重建、且文件名无指纹 → 强制协商缓存，靠 ETag/Last-Modified 走 304
//
// 后台路径可热切换，所以每次请求现取 ap.Path()，不能在这里缓存成常量。
func StaticCache(ap *adminpath.Resolver) Middleware {
	uploads := "public, max-age=31536000, immutable"
	themeAssets := "public, max-age=3600"
	adminAssets := "no-cache"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			switch {
			case strings.HasPrefix(p, "/uploads/"):
				w.Header().Set("Cache-Control", uploads)
			case strings.HasPrefix(p, "/assets/"):
				w.Header().Set("Cache-Control", themeAssets)
			case ap != nil && strings.HasPrefix(p, "/"+ap.Path()+"/assets/"):
				w.Header().Set("Cache-Control", adminAssets)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// gzipWriter 包装 gzip
// gzipMinSize 低于该字节数的响应不压缩。
//
// 小响应压缩后反而更大：gzip 头 + 尾固定 18~20 字节，
// /healthz 的 63 字节 JSON 压完变 88 字节，还白花一次压缩的 CPU。
const gzipMinSize = 512

// gzipWriter 包装 gzip，并延迟决定「压不压」。
//
// 不能一进中间件就设 Content-Encoding 再压：那时还不知道响应有多大。
// 这里先缓冲 gzipMinSize 字节，超了才真的压缩；没超就原样输出。
type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer

	// buf 缓冲「还没决定是否压缩」的前段数据
	buf []byte
	// status 暂存状态码，等决定压缩时再真正写头
	status      int
	wroteHeader bool
	decided     bool
	compressing bool

	// skipBody 标记「无实体响应」（304 / 204）。
	// 这类响应不能带 Content-Encoding，也不能写 gzip 尾字节，
	// 否则协商缓存（ETag / If-Modified-Since）会拿到脏响应。
	skipBody bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	g.status = code

	// 无实体响应：立即写头，标记跳过 body
	if code == http.StatusNotModified || code == http.StatusNoContent || code < http.StatusOK {
		g.skipBody = true
		g.decided = true
		g.Header().Del("Content-Type")
		g.Header().Del("Content-Encoding")
		g.Header().Del("Content-Length")
		g.ResponseWriter.WriteHeader(code)
	}
	// 其余情况压着不写，等 Write / finish 决定压不压
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.skipBody {
		return len(b), nil
	}

	if !g.decided {
		g.buf = append(g.buf, b...)
		if len(g.buf) < gzipMinSize {
			return len(b), nil
		}
		// 越过阈值，转压缩
		g.startCompress()
		return len(b), nil
	}
	if g.compressing {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// startCompress 开始压缩，并把已缓冲的数据吐出去
func (g *gzipWriter) startCompress() {
	g.decided = true
	g.compressing = true
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Del("Content-Length") // 压缩后长度未知
	g.ResponseWriter.WriteHeader(g.status)
	g.gz = gzip.NewWriter(g.ResponseWriter)
	_, _ = g.gz.Write(g.buf)
	g.buf = nil
}

// finish 收尾：未决的小响应原样输出，压缩过的补 gzip 尾。
func (g *gzipWriter) finish() {
	if g.skipBody {
		return
	}
	if !g.decided {
		// 小响应：不压缩
		g.decided = true
		g.ResponseWriter.WriteHeader(g.status)
		if len(g.buf) > 0 {
			_, _ = g.ResponseWriter.Write(g.buf)
		}
		g.buf = nil
		return
	}
	if g.compressing && g.gz != nil {
		_ = g.gz.Close()
	}
}

// Gzip 压缩响应（小响应跳过压缩）
func Gzip() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
				strings.HasPrefix(r.URL.Path, "/uploads/") {
				next.ServeHTTP(w, r)
				return
			}
			// Vary 无条件设置：即使本次没压缩，也不能让缓存把两种变体混用
			w.Header().Add("Vary", "Accept-Encoding")

			gw := &gzipWriter{ResponseWriter: w, status: http.StatusOK}
			defer gw.finish()
			next.ServeHTTP(gw, r)
		})
	}
}

// skipSessionPath 判断请求是否完全不需要会话。
//
// 用白名单而非黑名单：只有明确「不渲染模板、不需要 CSRF」的路径才跳过，
// 新增路由若没被列进来，行为与改造前一致（仍建会话），不会因为漏配而出错。
func skipSessionPath(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	switch {
	case p == "/healthz", p == "/favicon.ico", p == "/robots.txt":
		return true
	case strings.HasPrefix(p, "/assets/"), strings.HasPrefix(p, "/uploads/"):
		return true
	}
	// 后台静态资源形如 /{admin}/assets/app.css，前缀随后台路径变化
	if ap := AdminPathFrom(r); ap != nil {
		return strings.HasPrefix(p, "/"+ap.Path()+"/assets/")
	}
	return false
}

// SessionMiddleware 会话管理：读取/创建 session，注入用户
//
// 注意：静态资源与健康检查会跳过建会话。
// 它们既不渲染模板也不需要 CSRF，而原实现会给每个 CSS / 图片请求往 sessions 表插一行 ——
// 一个首页几十个静态资源，等于每次访问写几十条记录，纯属浪费。
func SessionMiddleware(cfg *config.Config, repo *repository.Repo) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipSessionPath(r) {
				next.ServeHTTP(w, r)
				return
			}
			var sess *model.Session

			if c, err := r.Cookie(cfg.SessionName); err == nil && c.Value != "" {
				if s, err := repo.Sess.ByToken(c.Value); err == nil {
					sess = s
				}
			}

			if sess == nil {
				sess = &model.Session{
					Token:     security.RandomToken(32),
					CSRFToken: security.RandomToken(32),
					IP:        utils.ClientIP(r),
					UserAgent: truncate(r.UserAgent(), 250),
					ExpiresAt: time.Now().Add(cfg.SessionLifetime),
				}
				if err := repo.Sess.Create(sess); err != nil {
					slog.Error("创建会话失败", "err", err)
				}
				http.SetCookie(w, &http.Cookie{
					Name:     cfg.SessionName,
					Value:    sess.Token,
					Path:     "/",
					HttpOnly: true,
					Secure:   cfg.SessionSecure,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   int(cfg.SessionLifetime.Seconds()),
				})
			}

			ctx := withValue(r, CtxSession, sess)
			if sess.UserID > 0 {
				if u, err := repo.User.ByID(sess.UserID); err == nil {
					ctx = withValue(ctx, CtxUser, u)
				}
			}
			next.ServeHTTP(w, ctx)
		})
	}
}

// RequireAdmin 要求已登录后台
func RequireAdmin() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if UserFrom(r) == nil {
				login := AdminPathFrom(r).Join("login")
				if isHTMX(r) {
					w.Header().Set("HX-Redirect", login)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				http.Redirect(w, r, login+"?next="+r.URL.RequestURI(), http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole 要求指定角色之一
func RequireRole(roles ...string) Middleware {
	allow := make(map[string]bool, len(roles))
	for _, r := range roles {
		allow[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFrom(r)
			if u == nil {
				http.Redirect(w, r, AdminPathFrom(r).Join("login"), http.StatusSeeOther)
				return
			}
			if !allow[u.Role] {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("<h1>403 权限不足</h1><p>当前账号无权访问该页面。</p>"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// truncate 按字节上限截断，但回退到 UTF-8 字符边界再下刀——
// 切在多字节字符中间会产生非法 UTF-8 序列，MySQL strict mode 会拒绝写入
// sessions 表，导致该请求永远无法建立持久会话。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// ---------- 上下文辅助 ----------

func withValue(r *http.Request, key ctxKey, val any) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), key, val))
}

// SessionFrom 取会话
func SessionFrom(r *http.Request) *model.Session {
	if v, ok := r.Context().Value(CtxSession).(*model.Session); ok {
		return v
	}
	return nil
}

// UserFrom 取登录用户
func UserFrom(r *http.Request) *model.User {
	if v, ok := r.Context().Value(CtxUser).(*model.User); ok {
		return v
	}
	return nil
}

// WithAdminPath 把当前后台路径解析器注入上下文。
//
// 必须在路由匹配前生效，且要在 RequireAdmin 之前，因为鉴权失败时要跳转到正确的登录页。
func WithAdminPath(ap *adminpath.Resolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, withValue(r, CtxAdminPath, ap))
		})
	}
}

// AdminPathFrom 取后台路径解析器；未注入时返回一个默认解析器，保证不会 nil panic。
func AdminPathFrom(r *http.Request) *adminpath.Resolver {
	if v, ok := r.Context().Value(CtxAdminPath).(*adminpath.Resolver); ok && v != nil {
		return v
	}
	return adminpath.New("")
}

// AdminURL 便捷方法：取后台根路径，如 "/manage"
func AdminURL(r *http.Request) string { return AdminPathFrom(r).URL() }

// CSRFMiddleware CSRF 校验（POST/PUT/DELETE 强制）
func CSRFMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				sess := SessionFrom(r)
				if sess == nil {
					http.Error(w, "会话无效，请刷新页面重试", http.StatusForbidden)
					return
				}
				token := r.FormValue("csrf_token")
				if token == "" {
					token = r.Header.Get("X-CSRF-Token")
				}
				if token == "" || !security.ConstantTimeEqual(token, sess.CSRFToken) {
					slog.Warn("CSRF 校验失败", "path", r.URL.Path, "ip", utils.ClientIP(r))
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`<h1>403 请求被拒绝</h1><p>表单令牌已失效，请返回上一页刷新后重试。</p><p><a href="javascript:history.back()">返回</a></p>`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit 简易内存限流（按 IP+路径前缀）
type rateEntry struct {
	count int
	reset time.Time
}

// Limiter 简单限流器（并发安全）
type Limiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	limit   int
	window  time.Duration
}

// NewLimiter 构造限流器
func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{entries: make(map[string]*rateEntry), limit: limit, window: window}
}

// Allow 是否放行
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = &rateEntry{count: 1, reset: now.Add(l.window)}
		return true
	}
	if e.count >= l.limit {
		return false
	}
	e.count++
	return true
}

// Reset 清空某个 key 的计数。
// 登录成功后必须调用，否则正常用户连续登录几次就会被自己的成功操作锁死。
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// Cleanup 清理过期条目，避免内存增长
func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, e := range l.entries {
		if now.After(e.reset) {
			delete(l.entries, k)
		}
	}
}
