package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/config"
	"github.com/example/gocms/internal/handler"
	"github.com/example/gocms/internal/middleware"
	"github.com/example/gocms/internal/repository"
)

// Build 构建完整路由（供首次装配与后台改路径后重建使用）。
//
// 后台前缀不是写死的：admin 路由在此按 ap.Path() 生成。
// 路径变更后调用 Dynamic.Reload() 重建整棵 mux，代价极小且只在改设置时发生一次。
func Build(cfg *config.Config, repo *repository.Repo, h *handler.Handler, ap *adminpath.Resolver, ipGuard *middleware.IPGuard, uploadDir, themeRoot, webRoot string) http.Handler {
	mux := http.NewServeMux()

	// ================= 健康检查 =================
	// 放在最前且不进后台前缀：负载均衡/监控不需要知道后台路径就能探活。
	// 会真的 Ping 一次数据库，DB 不可用返回 503，便于上游摘节点。
	mux.HandleFunc("GET /healthz", h.Healthz)

	// ================= 前台 =================
	mux.HandleFunc("GET /{$}", h.Home)
	mux.HandleFunc("GET /c/{slug}", h.Category)
	mux.HandleFunc("GET /a/{slug}", h.Post)
	mux.HandleFunc("GET /p/{slug}", h.Page)
	mux.HandleFunc("GET /t/{slug}", h.Tag)
	mux.HandleFunc("GET /search", h.Search)
	mux.HandleFunc("POST /message", h.MessageSubmit)
	mux.HandleFunc("GET /sitemap.xml", h.Sitemap)
	mux.HandleFunc("GET /robots.txt", h.Robots)
	mux.HandleFunc("GET /rss.xml", h.RSS)

	// ================= 后台 =================
	// ap 由中间件注入到 request context，handler 用 middleware.AdminPath(r) 取当前前缀。
	base := ap.URL() // 如 "/manage"

	mux.HandleFunc("GET "+base+"/login", h.LoginPage)
	mux.HandleFunc("POST "+base+"/login", h.LoginSubmit)
	mux.HandleFunc("POST "+base+"/logout", h.Logout)

	adminGuard := middleware.RequireAdmin()
	adminOnly := middleware.RequireRole("admin")
	auth := func(fn http.HandlerFunc) http.Handler { return adminGuard(fn) }
	authAdmin := func(fn http.HandlerFunc) http.Handler {
		return adminGuard(adminOnly(fn))
	}

	// 概览
	mux.Handle("GET "+base+"/{$}", auth(h.Dashboard))
	mux.Handle("GET "+base, auth(h.Dashboard))

	// 文章
	mux.Handle("GET "+base+"/posts", auth(h.PostList))
	mux.Handle("GET "+base+"/posts/new", auth(h.PostNew))
	mux.Handle("POST "+base+"/posts/save", auth(h.PostSave))
	mux.Handle("POST "+base+"/posts/bulk", auth(h.PostBulk))
	mux.Handle("GET "+base+"/posts/{id}/edit", auth(h.PostEdit))
	mux.Handle("POST "+base+"/posts/{id}/delete", auth(h.PostDelete))

	// 单页
	mux.Handle("GET "+base+"/pages", auth(h.PageList))
	mux.Handle("GET "+base+"/pages/new", auth(h.PageNew))
	mux.Handle("POST "+base+"/pages/save", auth(h.PageSave))
	mux.Handle("GET "+base+"/pages/{id}/edit", auth(h.PageEdit))
	mux.Handle("POST "+base+"/pages/{id}/delete", auth(h.PageDelete))

	// 栏目
	mux.Handle("GET "+base+"/categories", auth(h.CategoryList))
	mux.Handle("GET "+base+"/categories/new", auth(h.CategoryNew))
	mux.Handle("POST "+base+"/categories/save", auth(h.CategorySave))
	mux.Handle("GET "+base+"/categories/{id}/edit", auth(h.CategoryEdit))
	mux.Handle("POST "+base+"/categories/{id}/delete", auth(h.CategoryDelete))

	// 自定义字段
	mux.Handle("POST "+base+"/fields/save", auth(h.FieldSave))
	mux.Handle("POST "+base+"/fields/{id}/delete", auth(h.FieldDelete))

	// 标签
	mux.Handle("GET "+base+"/tags", auth(h.TagList))
	mux.Handle("POST "+base+"/tags/save", auth(h.TagSave))
	mux.Handle("POST "+base+"/tags/{id}/delete", auth(h.TagDelete))

	// 媒体
	mux.Handle("GET "+base+"/media", auth(h.MediaList))
	mux.Handle("POST "+base+"/media/upload", auth(h.MediaUpload))
	mux.Handle("POST "+base+"/media/{id}/delete", auth(h.MediaDelete))
	mux.Handle("GET "+base+"/media/picker.json", auth(h.MediaPickerJSON))
	mux.Handle("POST "+base+"/upload.json", auth(h.UploadJSON))

	// 菜单
	mux.Handle("GET "+base+"/menus", auth(h.MenuList))
	mux.Handle("POST "+base+"/menus/save", auth(h.MenuSave))
	mux.Handle("GET "+base+"/menus/{id}", auth(h.MenuDetail))
	mux.Handle("POST "+base+"/menus/{id}/delete", auth(h.MenuDelete))
	mux.Handle("POST "+base+"/menu-items/save", auth(h.MenuItemSave))
	mux.Handle("POST "+base+"/menu-items/{id}/delete", auth(h.MenuItemDelete))

	// 轮播
	mux.Handle("GET "+base+"/banners", auth(h.BannerList))
	mux.Handle("POST "+base+"/banners/save", auth(h.BannerSave))
	mux.Handle("POST "+base+"/banners/{id}/delete", auth(h.BannerDelete))

	// 友链
	mux.Handle("GET "+base+"/links", auth(h.LinkList))
	mux.Handle("POST "+base+"/links/save", auth(h.LinkSave))
	mux.Handle("POST "+base+"/links/{id}/delete", auth(h.LinkDelete))

	// 留言
	mux.Handle("GET "+base+"/messages", auth(h.MessageList))
	mux.Handle("POST "+base+"/messages/reply", auth(h.MessageReply))
	mux.Handle("POST "+base+"/messages/status", auth(h.MessageStatus))
	mux.Handle("POST "+base+"/messages/{id}/delete", auth(h.MessageDelete))

	// 个人资料
	mux.Handle("GET "+base+"/profile", auth(h.Profile))
	mux.Handle("POST "+base+"/profile", auth(h.ProfileSave))

	// 用户管理（仅 admin）
	mux.Handle("GET "+base+"/users", authAdmin(h.UserList))
	mux.Handle("POST "+base+"/users/save", authAdmin(h.UserSave))
	mux.Handle("POST "+base+"/users/{id}/delete", authAdmin(h.UserDelete))

	// 设置 / 主题（仅 admin）
	mux.Handle("GET "+base+"/settings", authAdmin(h.Settings))
	mux.Handle("POST "+base+"/settings", authAdmin(h.SettingsSave))
	mux.Handle("GET "+base+"/theme", authAdmin(h.ThemePage))
	mux.Handle("POST "+base+"/theme", authAdmin(h.ThemeSwitch))
	mux.Handle("GET "+base+"/stats.json", authAdmin(h.StatsJSON))

	// 操作日志（仅 admin）
	mux.Handle("GET "+base+"/audits", authAdmin(h.AuditList))
	mux.Handle("POST "+base+"/audits/purge", authAdmin(h.AuditPurge))

	// ================= 静态资源 =================
	mux.Handle("GET /assets/", http.StripPrefix("/assets/",
		noDirList(http.FileServer(http.Dir(filepath.Join(themeRoot, "assets"))))))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/",
		noDirList(http.FileServer(http.Dir(uploadDir)))))

	// 后台静态资源跟随前缀，这样后台路径不可猜测时，静态资源也不会暴露
	adminStatic := "/" + ap.Path() + "/assets/"
	mux.Handle("GET "+adminStatic, http.StripPrefix(adminStatic,
		noDirList(http.FileServer(http.Dir(filepath.Join(webRoot, "admin", "static"))))))

	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(themeRoot, "assets", "favicon.ico")
		if _, err := os.Stat(p); err == nil {
			http.ServeFile(w, r, p)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// 旧后台路径重定向（改过路径后，书签仍能用）。
	// 注意 /admin 本身要单独注册：只注册 "/admin/{rest...}" 时，
	// ServeMux 会把 "/admin" 当作子树根并 307 补斜杠，多一跳才能到新地址。
	for _, legacy := range ap.Legacy() {
		lp := "/" + legacy
		mux.Handle("GET "+lp, legacyRedirect(ap, h.NotFound))
		mux.Handle("POST "+lp, legacyRedirect(ap, h.NotFound))
		mux.Handle("GET "+lp+"/{rest...}", legacyRedirect(ap, h.NotFound))
		mux.Handle("POST "+lp+"/{rest...}", legacyRedirect(ap, h.NotFound))
	}

	// 伪静态与 404 兜底
	mux.HandleFunc("GET /{path...}", h.NotFound)
	mux.HandleFunc("POST /{path...}", func(w http.ResponseWriter, r *http.Request) {
		h.NotFound(w, r)
	})

	return middleware.Chain(stripHTMLExt(mux),
		middleware.Recover(cfg.IsDev),
		middleware.Logger(),
		middleware.SecurityHeaders(),
		// 静态资源缓存策略要在 Gzip 之前设头，避免被压缩路径覆盖
		middleware.StaticCache(ap),
		middleware.Gzip(),
		middleware.WithAdminPath(ap),
		// IP 白名单必须在 Session 之前：被拦的请求不该占用连接、也不该创建会话
		ipGuard.Middleware(ap),
		middleware.SessionMiddleware(cfg, repo),
		middleware.CSRFMiddleware(),
	)
}

// legacyRedirect 把旧后台路径 302 到新路径，保留子路径与查询串。
func legacyRedirect(ap *adminpath.Resolver, notFound http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 静默模式：旧路径必须表现得和任意不存在的页面一模一样，
		// 否则 302 的 Location 头等于把新后台地址免费告诉扫描器。
		if ap.Silent() {
			notFound(w, r)
			return
		}
		rest := r.PathValue("rest")
		target := ap.Join(rest)
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// stripHTMLExt 支持伪静态：/a/foo.html → /a/foo
// 必须在 ServeMux 匹配前改写路径，否则 /a/{slug} 会先捕获带 .html 的 slug
func stripHTMLExt(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, ".html") {
			trimmed := strings.TrimSuffix(p, ".html")
			for _, prefix := range []string{"/a/", "/c/", "/p/", "/t/"} {
				if strings.HasPrefix(trimmed, prefix) {
					r.URL.Path = trimmed
					r.URL.RawPath = ""
					break
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// noDirList 禁止目录浏览
func noDirList(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
