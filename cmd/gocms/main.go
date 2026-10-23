package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/config"
	"github.com/example/gocms/internal/database"
	"github.com/example/gocms/internal/handler"
	"github.com/example/gocms/internal/middleware"
	"github.com/example/gocms/internal/render"
	"github.com/example/gocms/internal/repository"
	"github.com/example/gocms/internal/router"
	"github.com/example/gocms/internal/security"
	"github.com/example/gocms/internal/seed"
	"github.com/example/gocms/internal/utils"
	"github.com/example/gocms/internal/viewcount"
)

func main() {
	// 日志
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// 配置
	config.LoadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		slog.Error("配置加载失败", "err", err)
		os.Exit(1)
	}
	// 是否信任反向代理头，必须在任何取 IP 的代码之前设置
	utils.SetTrustProxy(cfg.TrustProxy)
	if cfg.TrustProxy {
		slog.Info("已启用 TRUST_PROXY：客户端 IP 取自 X-Real-IP / X-Forwarded-For，请确认前置代理会覆盖该头")
	}

	// 建库
	if err := database.EnsureDatabase(cfg.DSNWithoutDB(), cfg.DBName); err != nil {
		slog.Error("创建数据库失败", "err", err)
		os.Exit(1)
	}

	db, err := database.Connect(cfg.DSN())
	if err != nil {
		slog.Error("连接数据库失败", "err", err, "dsn", maskDSN(cfg))
		os.Exit(1)
	}
	defer db.Close()

	// 迁移
	if err := database.Migrate(db); err != nil {
		slog.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	gdb, err := database.Open(db, cfg.IsDev)
	if err != nil {
		slog.Error("初始化 GORM 失败", "err", err)
		os.Exit(1)
	}

	repo := repository.New(db, gdb)

	// 种子数据
	if err := seed.Run(cfg, repo); err != nil {
		slog.Error("初始化数据失败", "err", err)
		os.Exit(1)
	}

	// 后台路径：库中配置优先，其次 .env，最后默认 admin
	adminPath := repo.Set.Get("admin_path")
	if adminPath == "" {
		// 首次启动：把 .env 的初始值落库，后台设置页才能显示当前生效值
		adminPath = cfg.AdminPath
		if err := repo.Set.Set("admin_path", adminPath); err != nil {
			slog.Warn("写入初始后台路径失败", "err", err)
		}
	}
	ap := adminpath.New(adminPath)
	if ap.Path() != strings.ToLower(strings.Trim(adminPath, "/")) {
		slog.Warn("配置的后台路径非法或与保留路径冲突，已回退默认值",
			"want", adminPath, "actual", ap.Path())
		_ = repo.Set.Set("admin_path", ap.Path())
	}
	// 恢复历史路径，保证改过路径后重启，旧书签仍能 302 到新地址
	if raw := repo.Set.Get("admin_path_legacy"); raw != "" {
		ap.Load(ap.Path(), strings.Split(raw, ","))
	}
	// 旧路径静默模式：开启后旧地址返回 404，不会把新入口暴露给扫描器
	ap.SetSilent(repo.Set.Get("admin_path_silent") == "1")

	// 上传目录
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		slog.Error("创建上传目录失败", "err", err)
		os.Exit(1)
	}

	// 当前主题
	themeName := repo.Set.Get("theme")
	if themeName == "" {
		themeName = cfg.Theme
	}
	themesRoot := "themes"
	themeRoot := filepath.Join(themesRoot, themeName)
	if !render.ThemeExists(themeRoot) {
		slog.Warn("主题不存在，回退到 default", "theme", themeName)
		themeName = "default"
		themeRoot = filepath.Join(themesRoot, themeName)
	}

	adminTemplates := filepath.Join("web", "admin", "templates")
	rnd := render.New(themeRoot, adminTemplates, cfg.IsDev)
	if err := rnd.Init(); err != nil {
		slog.Error("模板初始化失败", "err", err)
		os.Exit(1)
	}

	h := handler.New(cfg, repo, rnd, ap, cfg.UploadDir, themeRoot, "web")

	// 后台 IP 白名单：从库中恢复，运行期可改
	ipGuard := middleware.NewIPGuard()
	if raw := repo.Set.Get("admin_ip_allow"); raw != "" {
		list, invalid := security.ParseIPAllowList(raw)
		if len(invalid) > 0 {
			slog.Warn("后台 IP 白名单存在无法识别的条目，已忽略", "invalid", invalid)
		}
		if !list.Empty() {
			ipGuard.Set(list)
			slog.Info("后台 IP 白名单已启用", "rules", len(list.Rules()))
		}
	}
	h.SetIPGuard(ipGuard)

	// 浏览量聚合：同 IP 在 10 分钟内只算一次，每 10 秒批量落库一次。
	// 不用每次访问写库——热文的行锁会被打满，而且刷新就 +1 本身也没意义。
	views := viewcount.New(repo.Post.AddViews, viewcount.Options{
		Interval: 10 * time.Second,
		Window:   10 * time.Minute,
	})
	go views.Run()
	h.SetViewCounter(views)

	// 路由容器：后台改路径后可原子重建，无需重启
	rt := router.NewDynamic(func() http.Handler {
		return router.Build(cfg, repo, h, ap, ipGuard, cfg.UploadDir, themeRoot, "web")
	})
	// 让 handler 在改完路径后能触发重建
	h.SetReloader(rt)

	// 限流记录定期清理（key 含用户输入，不清理会被随机用户名撑爆内存）
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			h.CleanupLimits()
		}
	}()

	// 审计日志定期清理（保留 180 天），每小时检查一次
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			if n := h.PurgeOldAuditLogs(); n > 0 {
				slog.Info("已清理过期审计日志", "rows", n)
			}
		}
	}()

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           rt,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// 定时清理过期会话
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := repo.Sess.CleanExpired(); err == nil && n > 0 {
				slog.Info("清理过期会话", "count", n)
			}
		}
	}()

	go func() {
		slog.Info("GoCMS 已启动",
			"addr", "http://localhost"+addr,
			"admin", "http://localhost"+addr+ap.URL(),
			"env", cfg.Env,
			"theme", themeName,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("服务启动失败", "err", err)
			os.Exit(1)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("正在关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("关闭超时", "err", err)
	}
	// 必须放在 Shutdown 之后：先停止接收新请求，再把最后一批浏览量落库
	views.Close()
	slog.Info("已退出")
}

func maskDSN(cfg *config.Config) string {
	return cfg.DBUser + ":***@" + cfg.DBHost + ":" + cfg.DBPort + "/" + cfg.DBName
}
