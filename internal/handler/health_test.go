package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/example/gocms/internal/repository"
)

// /healthz 的价值全在「数据库挂了要返回 503」。
// 如果 DB 断了还返回 200，负载均衡会把流量继续打到一个干不了活的节点上，
// 用户看到满屏 500，监控却一片绿。
func TestHealthzDegradedWhenDBDown(t *testing.T) {
	// 指向不存在的端口并立即关闭：Ping 必定失败，模拟 MySQL 掉线
	sqlDB, err := sql.Open("mysql", "root:x@tcp(127.0.0.1:1)/nodb")
	if err != nil {
		t.Fatalf("构造连接失败: %v", err)
	}
	_ = sqlDB.Close()

	h := &Handler{repo: &repository.Repo{DB: sqlDB}}
	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（%s）", err, rec.Body.String())
	}
	if body.Status != "degraded" || body.DB != "fail" {
		t.Errorf("响应体 = %+v，期望 status=degraded db=fail", body)
	}
	if body.FailHint == "" {
		t.Error("降级时应给出排查提示")
	}

	// 不能把内部错误原文吐出去（连接串、驱动报错都可能带账号信息）
	if leaked := rec.Body.String(); strings.Contains(leaked, "sql:") ||
		strings.Contains(leaked, "dial tcp") || strings.Contains(leaked, "root:") {
		t.Errorf("响应泄露了内部细节: %s", leaked)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("健康检查结果不能被缓存")
	}
}

// 连接未初始化时也不能 panic，要稳定返回 503
func TestHealthzWithoutDB(t *testing.T) {
	h := &Handler{repo: &repository.Repo{}}
	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q，期望 JSON", ct)
	}
}

// uptime 曾经永远是 0：字段定义了却没赋值，JSON 里看着正常，实际拿不到任何信息。
// 把启动时间往前拨 2 小时来断言它真的在被计算。
func TestHealthzUptime(t *testing.T) {
	orig := processStartedAt
	processStartedAt = time.Now().Add(-2 * time.Hour)
	t.Cleanup(func() { processStartedAt = orig })

	h := &Handler{repo: &repository.Repo{}}
	rec := httptest.NewRecorder()
	h.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Uptime < 7000 {
		t.Errorf("uptime_seconds = %d，进程已跑 2 小时应 > 7000", body.Uptime)
	}
}
