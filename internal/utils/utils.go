package utils

import (
	"bytes"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/leo0315code/gocms/internal/security"
)

// ---------- 分页 ----------

// Pagination 分页信息
type Pagination struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
	Pages      []PageLink
}

// PageLink 页码链接
type PageLink struct {
	Num     int
	URL     string
	Current bool
}

// NewPagination 构建分页，baseURL 为不含 page 参数的地址
func NewPagination(page, perPage, total int, baseURL string) *Pagination {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	p := &Pagination{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
	}
	if p.HasPrev {
		p.PrevURL = pageURL(baseURL, page-1)
	}
	if p.HasNext {
		p.NextURL = pageURL(baseURL, page+1)
	}

	// 窗口：当前页前后各 2 页，首尾固定显示
	start := page - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > totalPages {
		end = totalPages
	}
	if end-start < 4 {
		start = end - 4
		if start < 1 {
			start = 1
		}
	}
	for i := start; i <= end; i++ {
		p.Pages = append(p.Pages, PageLink{Num: i, URL: pageURL(baseURL, i), Current: i == page})
	}
	return p
}

func pageURL(base string, page int) string {
	if page <= 1 {
		return base
	}
	if strings.Contains(base, "?") {
		return base + "&page=" + strconv.Itoa(page)
	}
	return base + "?page=" + strconv.Itoa(page)
}

// Offset SQL 偏移量
func (p *Pagination) Offset() int { return (p.Page - 1) * p.PerPage }

// ---------- 时间与文本 ----------

// FormatDate 格式化日期
func FormatDate(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("2006-01-02")
}

// FormatDateTime 格式化日期时间
func FormatDateTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("2006-01-02 15:04")
}

// TimeAgo 相对时间
func TimeAgo(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " 分钟前"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " 小时前"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " 天前"
	default:
		return t.Format("2006-01-02")
	}
}

// Excerpt 截断纯文本摘要（按 rune 计）
func Excerpt(s string, n int) string {
	s = security.SanitizePlain(s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// HumanSize 字节转可读大小
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB"}
	div := int64(unit)
	exp := 0
	for n/div >= unit && exp < len(units)-1 {
		div *= unit
		exp++
	}
	s := strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + " " + units[exp]
}

// ---------- Markdown ----------

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Table, extension.Strikethrough, extension.TaskList),
	goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
)

// MarkdownToHTML 转 HTML 并净化
func MarkdownToHTML(src string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(security.SanitizeHTML(buf.String()))
}

// RenderContent 依据 content_format 渲染正文
func RenderContent(format, content string) template.HTML {
	if format == "markdown" {
		return MarkdownToHTML(content)
	}
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return template.HTML(security.SanitizeHTML(content))
}

// ---------- HTTP ----------

// trustProxy 由 main 在启动时按配置设置。
// 默认 false：X-Real-IP / X-Forwarded-For 任何客户端都能伪造，
// 信任它们会让基于 IP 的限流与白名单彻底失效。
var trustProxy atomic.Bool

// SetTrustProxy 设置是否信任反向代理头（启动时调用一次）
func SetTrustProxy(v bool) { trustProxy.Store(v) }

// ClientIP 提取客户端 IP。
//
// 仅在 TRUST_PROXY=1 时才读取代理头；否则一律用 TCP 连接的对端地址，
// 避免攻击者用伪造头绕过登录限流或 IP 白名单。
//
// X-Forwarded-For 是追加式头：客户端可以在请求里自带任意个 IP，代理只把
// 自己看到的对端地址追加到末尾。所以最左侧完全由攻击者控制，可信的只有
// 最右侧一跳（信任代理写入的值）——取 parts[0] 等于信任攻击者的自报家门。
// 前提是只有一层可信代理；多层代理（如 CDN → Nginx）需要把 CDN 网段整体
// 纳入信任，由 Nginx 用 real_ip 模块先还原，再传单个真实 IP。
func ClientIP(r *http.Request) string {
	if trustProxy.Load() {
		// X-Real-IP 由代理直接覆盖写入（nginx: proxy_set_header X-Real-IP $remote_addr），
		// 不存在追加问题，可信；前提同样是代理确实覆盖而不是透传该头
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			parts := strings.Split(v, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return RemoteIP(r)
}

// RemoteIP 只取 TCP 连接对端地址，不读任何请求头。永远可信。
func RemoteIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// QueryInt 读取 int 查询参数
func QueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// QueryStr 读取字符串查询参数（已 trim）
func QueryStr(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

// FormInt 读取 int 表单值
func FormInt(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// FormUint 读取 uint 表单值
func FormUint(r *http.Request, key string, def uint64) uint64 {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// ParseUint 解析路径参数中的 uint
func ParseUint(s string) (uint64, bool) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// JSON 输出 JSON（用于回显 404 等场景）
func JSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// IntStr int 转字符串
func IntStr(n int) string { return strconv.Itoa(n) }

// UintStr uint64 转字符串
func UintStr(n uint64) string { return strconv.FormatUint(n, 10) }

// Add 模板函数
func Add(a, b int) int { return a + b }

// Sub 模板函数
func Sub(a, b int) int { return a - b }
