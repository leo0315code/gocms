// Package adminpath 解析并缓存后台入口路径。
//
// 设计要点：
//   - 优先级：数据库 settings.admin_path > .env ADMIN_PATH > 默认 "admin"
//   - 运行时可改：后台设置页保存后调用 Set()，路由表随即重建，下一次请求即生效，无需重启
//   - 带缓存：避免每个请求都查库；用 atomic.Value 保证并发安全
//   - 路由表按当前路径重建，而不是改写 URL —— 这样 handler 里看到的路径就是用户看到的路径，
//     next= 跳转参数、当前 URL 高亮、模板链接都不会错位
package adminpath

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// 保留前缀：这些是一级路由，不能作为后台路径，否则会抢占前台页面
var reserved = map[string]bool{
	"":        true,
	"a":       true, // 文章 /a/{slug}
	"c":       true, // 栏目 /c/{slug}
	"p":       true, // 单页 /p/{slug}
	"t":       true, // 标签 /t/{slug}
	"search":  true,
	"message": true, // POST /message
	"sitemap": true,
	"robots":  true,
	"rss":     true,
	"healthz": true, // 健康检查端点，负载均衡/监控要固定访问，不能被后台占用
	"uploads": true,
	"assets":  true,
	"web":     true,
	"api":     true,
	"static":  true,
	"favicon": true,
}

// 只允许字母、数字、下划线、连字符
var validRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

const (
	defaultPath = "admin"
	minLen      = 2
	maxLen      = 32
)

// Resolver 后台路径解析器
type Resolver struct {
	path    atomic.Value // string，不含首尾斜杠，如 "manage"
	fromEnv string       // .env 里的初始值，作为「恢复默认」的兜底
	silent  atomic.Bool  // true = 旧路径直接走 404，不跳转（不泄露新路径）

	mu     sync.RWMutex
	legacy []string // 历史上用过的路径，用于旧地址重定向
}

// New 创建解析器。envPath 来自配置，非法时回退默认值。
func New(envPath string) *Resolver {
	r := &Resolver{fromEnv: defaultPath}
	if p, ok := Sanitize(envPath); ok {
		r.fromEnv = p
	}
	r.path.Store(r.fromEnv)
	return r
}

// Load 从持久化数据恢复：当前路径 + 历史路径。
// 历史路径必须一起恢复，否则重启后旧书签会 404。
func (r *Resolver) Load(current string, legacy []string) {
	if p, ok := Sanitize(current); ok {
		r.path.Store(p)
	}
	cur := r.Path()
	r.mu.Lock()
	r.legacy = r.legacy[:0]
	for _, v := range legacy {
		if c, ok := Sanitize(v); ok && c != cur {
			r.legacy = append(r.legacy, c)
		}
	}
	r.mu.Unlock()
}

// Path 当前后台路径（不含斜杠）
func (r *Resolver) Path() string {
	if v, ok := r.path.Load().(string); ok && v != "" {
		return v
	}
	return defaultPath
}

// URL 返回带前导斜杠的后台根路径，如 "/manage"
func (r *Resolver) URL() string { return "/" + r.Path() }

// Join 拼接后台子路径，如 Join("posts") -> "/manage/posts"
func (r *Resolver) Join(elem ...string) string {
	var b strings.Builder
	b.WriteString("/")
	b.WriteString(r.Path())
	for _, e := range elem {
		e = strings.Trim(e, "/")
		if e == "" {
			continue
		}
		b.WriteString("/")
		b.WriteString(e)
	}
	return b.String()
}

// Set 更新路径（已校验）。返回是否真的发生了变化。
func (r *Resolver) Set(p string) bool {
	clean, ok := Sanitize(p)
	if !ok {
		return false
	}
	old := r.Path()
	if clean == old {
		return false
	}
	r.path.Store(clean)

	r.mu.Lock()
	// 记下旧路径用于重定向，但不要与当前路径重复
	next := make([]string, 0, len(r.legacy)+1)
	next = append(next, old)
	for _, v := range r.legacy {
		if v != clean && v != old {
			next = append(next, v)
		}
	}
	// 只保留最近 5 个，避免无限增长
	if len(next) > 5 {
		next = next[:5]
	}
	r.legacy = next
	r.mu.Unlock()
	return true
}

// Legacy 历史后台路径列表（不含当前路径）
func (r *Resolver) Legacy() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cur := r.Path()
	out := make([]string, 0, len(r.legacy))
	for _, v := range r.legacy {
		if v != cur {
			out = append(out, v)
		}
	}
	return out
}

// EnvPath .env 中的初始值，供「恢复默认」使用
func (r *Resolver) EnvPath() string { return r.fromEnv }

// SetSilent 设置「旧路径静默模式」。
//
// 关闭时（默认）：旧路径 302 到新路径，书签友好，但 Location 头会把新路径告诉所有人。
// 开启时：旧路径返回与普通 404 完全一致的页面，不暴露新路径是否存在。
// 如果改路径的目的是「隐藏后台入口」，必须开启。
func (r *Resolver) SetSilent(v bool) { r.silent.Store(v) }

// Silent 是否处于旧路径静默模式
func (r *Resolver) Silent() bool { return r.silent.Load() }

// Default 内置默认值
func Default() string { return defaultPath }

// Sanitize 校验并规范化路径：去空白、去首尾斜杠、转小写。
// 校验失败返回 ("", false)。
func Sanitize(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	p = strings.ToLower(p)
	if len(p) < minLen || len(p) > maxLen {
		return "", false
	}
	if !validRe.MatchString(p) {
		return "", false
	}
	if reserved[p] {
		return "", false
	}
	return p, true
}

// Validate 返回给用户看的校验提示，空字符串表示通过
func Validate(p string) string {
	raw := strings.TrimSpace(strings.Trim(strings.TrimSpace(p), "/"))
	if raw == "" {
		return "后台路径不能为空"
	}
	if len(raw) < minLen {
		return "后台路径至少 2 个字符"
	}
	if len(raw) > maxLen {
		return "后台路径最多 32 个字符"
	}
	if !validRe.MatchString(raw) {
		return "后台路径只能包含字母、数字、下划线和连字符"
	}
	if reserved[strings.ToLower(raw)] {
		return "「" + raw + "」是系统保留路径，请换一个"
	}
	return ""
}
