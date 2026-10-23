package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// processStartedAt 进程启动时间，用于 /healthz 报告 uptime。
// 放在包级：同一进程内多次构建路由也不重置。
var processStartedAt = time.Now()

// healthResponse /healthz 的响应体。
//
// 刻意只暴露最小信息：不返回版本号以外的环境细节（DSN、路径、错误原文），
// 因为这个端点必须公开可访问，否则负载均衡和 systemd 探不到。
type healthResponse struct {
	Status   string `json:"status"`         // ok | degraded
	DB       string `json:"db"`             // ok | fail
	Uptime   int64  `json:"uptime_seconds"` // 进程已运行秒数
	DBCheck  int64  `json:"db_latency_ms"`  // 一次 Ping 的耗时
	FailHint string `json:"hint,omitempty"` // 失败时的排查指引，不含内部细节
}

// Healthz 健康检查端点。
//
// 用途：
//   - systemd / supervisor 判断进程是否真的能干活（而非只是端口在听）
//   - Nginx upstream 摘除故障节点
//   - 外部监控定时探活
//
// 与「端口能连上」的区别在于这里会真的 Ping 一次数据库，超时 800ms。
// 数据库断了要返回 503，让上游把流量摘走，而不是让用户看到 500。
//
// 该方法不建会话、不渲染模板，可被高频调用。
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{Status: "ok", DB: "ok"}

	ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
	defer cancel()

	start := time.Now()
	var err error
	if h.repo != nil && h.repo.DB != nil {
		err = h.repo.DB.PingContext(ctx)
	} else {
		err = errNoDB
	}
	resp.DBCheck = time.Since(start).Milliseconds()
	// 进程运行时长：运维判断「是否刚重启过」「是否稳定运行」最直接的指标
	resp.Uptime = int64(time.Since(processStartedAt).Seconds())

	code := http.StatusOK
	if err != nil {
		resp.Status = "degraded"
		resp.DB = "fail"
		resp.FailHint = "数据库不可用，检查 MySQL 是否运行、连接数是否耗尽"
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 健康检查结果不能被任何中间层缓存
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}

// errNoDB 数据库连接未初始化（正常启动流程下不会出现，属防御）
var errNoDB = errors.New("数据库连接未初始化")
