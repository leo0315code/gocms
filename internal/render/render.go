package render

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/example/gocms/internal/utils"
)

// Engine 模板引擎，主题可热切换
type Engine struct {
	mu        sync.RWMutex
	themeRoot string // ./themes/<name>
	adminRoot string // ./web/admin/templates
	dev       bool

	theme *template.Template
	admin *template.Template
}

// New 创建引擎
func New(themeRoot, adminRoot string, dev bool) *Engine {
	return &Engine{themeRoot: themeRoot, adminRoot: adminRoot, dev: dev}
}

// SetTheme 切换主题并重载模板
func (e *Engine) SetTheme(themeRoot string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.themeRoot = themeRoot
	e.theme = nil
	return e.loadThemeLocked()
}

// ThemeRoot 当前主题根目录
func (e *Engine) ThemeRoot() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.themeRoot
}

var funcMap = template.FuncMap{
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	"render":   utils.RenderContent,
	"md":       utils.MarkdownToHTML,
	"excerpt":  utils.Excerpt,
	"date":     func(t any) string { return utils.FormatDate(toTime(t), nil) },
	"datetime": func(t any) string { return utils.FormatDateTime(toTime(t), nil) },
	"add":      func(a, b int) int { return a + b },
	"sub":      func(a, b int) int { return a - b },
	"seq": func(n int) []int {
		out := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, i)
		}
		return out
	},
	"eqs":       func(a, b string) bool { return a == b },
	"hasPrefix": func(s, p string) bool { return strings.HasPrefix(s, p) },
	"upper":     strings.ToUpper,
	"lower":     strings.ToLower,
	"trim":      strings.TrimSpace,
	"size":      utils.HumanSize,
	"bool":      func(v int) bool { return v == 1 },
	"default": func(def, v string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return v
	},
	"concat": func(parts ...string) string { return strings.Join(parts, "") },
	"firstChar": func(s string) string {
		r := []rune(strings.TrimSpace(s))
		if len(r) == 0 {
			return "G"
		}
		return string(r[0])
	},
	"iterate": func(n int) []int {
		out := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, i)
		}
		return out
	},
}

func toTime(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case *time.Time:
		if x == nil {
			return time.Time{}
		}
		return *x
	default:
		return time.Time{}
	}
}

// loadThemeLocked 加载主题模板（调用方需持锁）
func (e *Engine) loadThemeLocked() error {
	tplDir := filepath.Join(e.themeRoot, "templates")
	files, err := collectTemplates(tplDir)
	if err != nil {
		return fmt.Errorf("读取主题模板失败: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("主题模板为空: %s", tplDir)
	}
	t, err := template.New("root").Funcs(funcMap).ParseFiles(files...)
	if err != nil {
		return fmt.Errorf("解析主题模板失败: %w", err)
	}
	e.theme = t
	return nil
}

// loadAdminLocked 加载后台模板
func (e *Engine) loadAdminLocked() error {
	files, err := collectTemplates(e.adminRoot)
	if err != nil {
		return fmt.Errorf("读取后台模板失败: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("后台模板为空: %s", e.adminRoot)
	}
	t, err := template.New("root").Funcs(funcMap).ParseFiles(files...)
	if err != nil {
		return fmt.Errorf("解析后台模板失败: %w", err)
	}
	e.admin = t
	return nil
}

// collectTemplates 递归收集 .html 文件
func collectTemplates(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".html") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// Render 渲染主题页面：执行 "content" 定义并由 "layout" 包裹
func (e *Engine) Render(name string, data any) ([]byte, error) {
	e.mu.RLock()
	t := e.theme
	dev := e.dev
	themeRoot := e.themeRoot
	e.mu.RUnlock()

	if t == nil || dev {
		// 开发模式每次重载，支持改模板即时生效
		e.mu.Lock()
		if err := e.loadThemeLocked(); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		t = e.theme
		e.mu.Unlock()
	}

	_ = themeRoot
	var buf strings.Builder
	// 逻辑名不存在时回退到文件名（如 home → home.html）
	if t.Lookup(name) == nil && t.Lookup(name+".html") != nil {
		name = name + ".html"
	}
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("渲染模板 %s 失败: %w", name, err)
	}
	return []byte(buf.String()), nil
}

// RenderAdmin 渲染后台页面
func (e *Engine) RenderAdmin(name string, data any) ([]byte, error) {
	e.mu.RLock()
	t := e.admin
	dev := e.dev
	e.mu.RUnlock()

	if t == nil || dev {
		e.mu.Lock()
		if err := e.loadAdminLocked(); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		t = e.admin
		e.mu.Unlock()
	}

	var buf strings.Builder
	if t.Lookup(name) == nil && t.Lookup(name+".html") != nil {
		name = name + ".html"
	}
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("渲染后台模板 %s 失败: %w", name, err)
	}
	return []byte(buf.String()), nil
}

// HasTheme 判断模板是否存在
func (e *Engine) HasTheme(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.theme == nil {
		return false
	}
	return e.theme.Lookup(name) != nil
}

// HasAdmin 判断后台模板是否存在
func (e *Engine) HasAdmin(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.admin == nil {
		return false
	}
	return e.admin.Lookup(name) != nil
}

// Init 初次加载
func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadThemeLocked(); err != nil {
		return err
	}
	return e.loadAdminLocked()
}

// ThemeExists 主题目录是否存在且有模板
func ThemeExists(root string) bool {
	files, err := collectTemplates(filepath.Join(root, "templates"))
	return err == nil && len(files) > 0
}

// DirExists 目录存在性
func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
