package router

import (
	"net/http"
	"sync/atomic"
)

// Dynamic 可在运行期替换内部 handler 的路由容器。
//
// 后台路径改过一次之后，mux 里注册的路由前缀就失效了，必须重建整棵 mux。
// http.Server.Handler 是固定的，所以外面套这一层：重建时原子替换内部指针，
// 正在处理的请求用旧 handler 跑完，新请求立刻走新 handler，无需重启、无需加锁。
type Dynamic struct {
	inner atomic.Value // http.Handler
	build func() http.Handler
}

// NewDynamic 创建容器并立刻构建一次
func NewDynamic(build func() http.Handler) *Dynamic {
	d := &Dynamic{build: build}
	d.inner.Store(build())
	return d
}

// ServeHTTP 实现 http.Handler
func (d *Dynamic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, _ := d.inner.Load().(http.Handler)
	if h == nil {
		http.Error(w, "服务未就绪", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// Reload 重建内部 handler
func (d *Dynamic) Reload() {
	d.inner.Store(d.build())
}
