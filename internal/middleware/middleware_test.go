package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leo0315code/gocms/internal/adminpath"
)

// 静态资源缓存策略：三类资源三种策略，写错会导致「改完样式不生效」或「白白回源」。
func TestStaticCache(t *testing.T) {
	ap := adminpath.New("manage")
	mw := StaticCache(ap)

	cases := []struct {
		path string
		want string
	}{
		{"/uploads/2026/10/abc.png", "public, max-age=31536000, immutable"},
		{"/assets/style.css", "public, max-age=3600"},
		{"/manage/assets/app.css", "no-cache"},
		{"/manage/assets/app.js", "no-cache"},
		// 页面本身不设，交给浏览器默认行为
		{"/", ""},
		{"/manage/posts", ""},
		{"/c/news", ""},
		// 旧路径下的资源不是合法静态入口，不该被标成可缓存
		{"/admin/assets/app.css", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))

		if got := rec.Header().Get("Cache-Control"); got != c.want {
			t.Errorf("%s 的 Cache-Control = %q，期望 %q", c.path, got, c.want)
		}
	}

	// 后台路径热切换后，新前缀的资源必须立刻走新策略（不能缓存在中间件里）
	ap.Set("vault77")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vault77/assets/app.css", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("路径切换后新前缀未生效：Cache-Control = %q", got)
	}
}

// 304 不能带 Content-Encoding，也不能有 body。
// 协商缓存一开始就是靠这个：浏览器发 If-Modified-Since，服务端回 304 空响应。
// 原实现里 gzip 会给 304 也挂上 Content-Encoding 并写入 gzip 尾字节，
// 部分代理/浏览器会把这种 304 当成损坏响应，导致缓存彻底失效。
func TestGzipSkipsNotModified(t *testing.T) {
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))

	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("状态码 = %d，期望 304", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("304 不应带 Content-Encoding，实际 %q", ce)
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("304 不应有响应体，实际 %q（%d 字节）", body, len(body))
	}
}

// 204 同理，不能塞 gzip 尾字节
func TestGzipSkipsNoContent(t *testing.T) {
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodDelete, "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Body.Len() != 0 {
		t.Errorf("204 不应有响应体，实际 %d 字节", rec.Body.Len())
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("204 不应带 Content-Encoding，实际 %q", ce)
	}
}

// 正常响应仍要压缩，别为了修 304 把压缩弄丢了
func TestGzipCompressesNormalResponse(t *testing.T) {
	payload := strings.Repeat("GoCMS ", 200)
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(payload))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q，期望 gzip", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("响应不是合法 gzip: %v", err)
	}
	defer zr.Close()
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(got) != payload {
		t.Errorf("解压后内容不一致：%d 字节 vs 原文 %d 字节", len(got), len(payload))
	}
}

// 小响应不压缩：gzip 头尾固定 ~20 字节，压 63 字节的 JSON 反而变 88 字节。
// 这个用例守住「不要为了压缩而让响应变大」。
func TestGzipSkipsSmallResponse(t *testing.T) {
	payload := `{"status":"ok","db":"ok","uptime_seconds":5,"db_latency_ms":0}` // 63 字节
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("小响应不应压缩，Content-Encoding = %q", ce)
	}
	if rec.Body.String() != payload {
		t.Errorf("响应体被改动：%q", rec.Body.String())
	}
	// 没压缩时不应丢掉原始 Content-Length 语义
	if rec.Body.Len() != len(payload) {
		t.Errorf("响应体长度 = %d，期望 %d", rec.Body.Len(), len(payload))
	}
	// Vary 无论如何都要有，否则缓存会混用压缩/未压缩两个变体
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("缺少 Vary: Accept-Encoding，实际 %q", rec.Header().Get("Vary"))
	}
}

// 分多次写入、累计超过阈值时必须压缩（缓冲逻辑不能只认第一次 Write）
func TestGzipCompressesAcrossWrites(t *testing.T) {
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		for i := 0; i < 40; i++ { // 40 × 64 = 2560 字节，单次都不足阈值
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("累计 2560 字节应压缩，Content-Encoding = %q", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("不是合法 gzip: %v", err)
	}
	defer zr.Close()
	got, _ := io.ReadAll(zr)
	if len(got) != 2560 {
		t.Errorf("解压后 %d 字节，期望 2560", len(got))
	}
}

// 只写状态码不写 body（如 302/303 跳转）：不能产生 gzip 头，也不能丢掉状态码
func TestGzipRedirectWithoutBody(t *testing.T) {
	h := Gzip()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/admin/login")
		w.WriteHeader(http.StatusSeeOther)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d，期望 303", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("无 body 的跳转不应带 Content-Encoding，实际 %q", ce)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("不应有响应体，实际 %d 字节", rec.Body.Len())
	}
}

// 静态资源与健康检查不建会话：否则一个首页几十个资源等于几十条 INSERT。
func TestSkipSessionPath(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/healthz", true},
		{http.MethodGet, "/assets/style.css", true},
		{http.MethodGet, "/uploads/2026/10/a.png", true},
		{http.MethodGet, "/favicon.ico", true},
		{http.MethodGet, "/robots.txt", true},
		{http.MethodHead, "/assets/style.css", true},
		// 页面不能跳过：前台留言表单要 CSRF，后台要用户信息
		{http.MethodGet, "/", false},
		{http.MethodGet, "/c/news", false},
		{http.MethodGet, "/admin/login", false},
		// 写请求一律不跳过（哪怕路径像静态资源）
		{http.MethodPost, "/assets/style.css", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		// 中间件链里 WithAdminPath 会先注入 ap，这里手工模拟
		r = withValue(r, CtxAdminPath, adminpath.New("admin"))
		if got := skipSessionPath(r); got != c.want {
			t.Errorf("skipSessionPath(%s %s) = %v，期望 %v", c.method, c.path, got, c.want)
		}
	}

	// 后台静态资源跟随当前路径
	r := withValue(httptest.NewRequest(http.MethodGet, "/manage/assets/app.css", nil), CtxAdminPath, adminpath.New("manage"))
	if !skipSessionPath(r) {
		t.Error("后台静态资源应跳过会话")
	}
	// 旧路径的后台资源不是有效入口，不跳过（会走 404，无需优化）
	r2 := withValue(httptest.NewRequest(http.MethodGet, "/admin/assets/app.css", nil), CtxAdminPath, adminpath.New("manage"))
	if skipSessionPath(r2) {
		t.Error("非当前后台前缀不应跳过会话")
	}
}
