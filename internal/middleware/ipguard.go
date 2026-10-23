package middleware

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/security"
	"github.com/example/gocms/internal/utils"
)

// IPGuard 后台 IP 白名单守卫。
//
// 规则存于 atomic.Pointer，后台改完立即生效，不需要重启或重建路由。
// 覆盖范围包括当前后台路径、历史后台路径，以及后台静态资源。
type IPGuard struct {
	list atomic.Pointer[security.IPAllowList]
}

// NewIPGuard 创建守卫，默认不限制
func NewIPGuard() *IPGuard {
	g := &IPGuard{}
	g.list.Store(security.EmptyIPAllowList())
	return g
}

// Set 更新白名单（传 nil 视为不限制）
func (g *IPGuard) Set(l *security.IPAllowList) {
	if l == nil {
		l = security.EmptyIPAllowList()
	}
	g.list.Store(l)
}

// Allow 判断该 IP 是否放行（白名单为空时恒为 true）
func (g *IPGuard) Allow(ip string) bool {
	l := g.list.Load()
	if l == nil {
		return true
	}
	return l.Contains(ip)
}

// Enabled 是否配置了白名单
func (g *IPGuard) Enabled() bool {
	l := g.list.Load()
	return l != nil && !l.Empty()
}

// Middleware 返回中间件。
//
// 只拦截后台相关路径：当前入口、历史入口、后台静态资源。
// 前台页面永远放行——白名单是「限制谁能进后台」，不是「限制谁能看网站」。
func (g *IPGuard) Middleware(ap *adminpath.Resolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAdminRequest(r, ap) {
				next.ServeHTTP(w, r)
				return
			}

			// 后台响应一律禁止收录，纵深防御（页面里还有 meta noindex）
			w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")

			if g.Enabled() {
				ip := utils.ClientIP(r)
				if !g.Allow(ip) {
					slog.Warn("后台 IP 白名单拦截", "ip", ip, "path", r.URL.Path,
						"ua", r.UserAgent())
					writeIPDenied(w, ip)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isAdminRequest 判断请求是否命中后台（当前路径 / 历史路径）
func isAdminRequest(r *http.Request, ap *adminpath.Resolver) bool {
	p := r.URL.Path
	base := ap.URL() // "/manage"
	if p == base || strings.HasPrefix(p, base+"/") {
		return true
	}
	for _, legacy := range ap.Legacy() {
		lp := "/" + legacy
		if p == lp || strings.HasPrefix(p, lp+"/") {
			return true
		}
	}
	return false
}

// writeIPDenied 输出拒绝页。
//
// 刻意不返回 404：白名单通常用于企业内网，管理员在错误网段访问时
// 需要明确知道「是被限制」而不是「路径写错了」，否则排查成本很高。
func writeIPDenied(w http.ResponseWriter, ip string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	safeIP := template.HTMLEscapeString(ip)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>403 访问受限</title></head>` +
		`<body style="margin:0;min-height:100vh;display:grid;place-items:center;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC',sans-serif;` +
		`background:#f5f6f9;color:#111827">` +
		`<div style="max-width:420px;padding:28px;text-align:center">` +
		`<h1 style="font-size:18px;margin:0 0 10px">403 访问受限</h1>` +
		`<p style="font-size:13px;line-height:1.7;color:#4b5563;margin:0 0 8px">` +
		`后台仅允许来自特定 IP 的访问，当前 IP 不在允许范围内。</p>` +
		`<p style="font-size:12px;color:#9ca3af;margin:0">你的 IP：` + safeIP + `</p>` +
		`</div></body></html>`))
}
