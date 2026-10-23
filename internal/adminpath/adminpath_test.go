package adminpath

import (
	"strings"
	"testing"
)

// 路径校验是「改错就把自己关在门外」的第一道闸，用例覆盖得比其他地方细。
func TestSanitize(t *testing.T) {
	ok := []struct{ in, want string }{
		{"manage", "manage"},
		{" Manage ", "manage"},
		{"/manage/", "manage"},
		{"ok_path-9", "ok_path-9"},
		{"BACKSTAGE99", "backstage99"},
		{"v2panel", "v2panel"},
	}
	for _, c := range ok {
		got, valid := Sanitize(c.in)
		if !valid {
			t.Errorf("Sanitize(%q) 应该通过，却被拒", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("Sanitize(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	// 全部是线上真实踩过或明确要拦的输入
	bad := []string{
		"",           // 空
		"a",          // 太短，且是保留字
		"c",          // 保留字（栏目前缀）
		"p",          // 保留字（单页前缀）
		"t",          // 保留字（标签前缀）
		"assets",     // 前台静态目录
		"uploads",    // 上传目录
		"search",     // 前台搜索
		"rss",        // 前台订阅
		"sitemap",    // 站点地图
		"healthz",    // 健康检查（本轮新增，必须占用）
		"with space", // 含空格
		"有中文",        // 非 ASCII
		"admin@x",    // 非法字符
		"-",          // 长度 1
		strings.Repeat("x", 33), // 超长
	}
	for _, c := range bad {
		if _, valid := Sanitize(c); valid {
			t.Errorf("Sanitize(%q) 应该被拒，却通过了", c)
		}
	}
}

// Validate 的返回值会直接显示给用户，必须说清楚错在哪，不能只给一句「参数错误」。
func TestValidateMessage(t *testing.T) {
	cases := map[string]string{
		"":          "不能为空",
		"a":         "至少 2 个字符",
		"assets":    "保留路径",
		"with sp":   "只能包含",
		strings.Repeat("y", 40): "最多 32 个字符",
	}
	for in, want := range cases {
		msg := Validate(in)
		if msg == "" {
			t.Errorf("Validate(%q) 应该返回错误，却通过了", in)
			continue
		}
		if !strings.Contains(msg, want) {
			t.Errorf("Validate(%q) = %q，期望包含 %q", in, msg, want)
		}
	}
	if msg := Validate("manage"); msg != "" {
		t.Errorf("Validate(\"manage\") 应该通过，却返回 %q", msg)
	}
}

func TestResolverSetAndLegacy(t *testing.T) {
	r := New("admin")
	if r.Path() != "admin" {
		t.Fatalf("初始路径 = %q，期望 admin", r.Path())
	}
	if r.URL() != "/admin" {
		t.Errorf("URL() = %q", r.URL())
	}

	// 改路径要成功，并把旧路径记进 legacy
	if !r.Set("manage") {
		t.Fatal("Set(manage) 应返回 true")
	}
	if r.Path() != "manage" {
		t.Errorf("Path() = %q，期望 manage", r.Path())
	}
	if got := r.Legacy(); len(got) != 1 || got[0] != "admin" {
		t.Errorf("Legacy() = %v，期望 [admin]", got)
	}

	// 设成同一个值不算变化，也不该污染 legacy
	if r.Set("manage") {
		t.Error("Set 相同值应返回 false")
	}
	if got := r.Legacy(); len(got) != 1 {
		t.Errorf("重复 Set 后 Legacy 长度 = %d，期望 1", len(got))
	}

	// 非法值不能改动当前路径
	if r.Set("assets") {
		t.Error("Set(assets) 应返回 false")
	}
	if r.Path() != "manage" {
		t.Errorf("非法 Set 后 Path() = %q，应保持 manage", r.Path())
	}

	// legacy 只保留最近 5 个，且不含当前路径
	for _, p := range []string{"p1x", "p2x", "p3x", "p4x", "p5x", "p6x", "p7x"} {
		r.Set(p)
	}
	legacy := r.Legacy()
	if len(legacy) > 5 {
		t.Errorf("Legacy 长度 = %d，应 <= 5", len(legacy))
	}
	for _, v := range legacy {
		if v == r.Path() {
			t.Errorf("Legacy 不应包含当前路径 %q", v)
		}
	}
}

func TestResolverJoin(t *testing.T) {
	r := New("manage")
	cases := []struct {
		elem []string
		want string
	}{
		{[]string{"posts"}, "/manage/posts"},
		{[]string{"posts", "new"}, "/manage/posts/new"},
		{[]string{"", "posts"}, "/manage/posts"},
		{[]string{"/posts/"}, "/manage/posts"},
		{[]string{"posts", "12", "edit"}, "/manage/posts/12/edit"},
	}
	for _, c := range cases {
		if got := r.Join(c.elem...); got != c.want {
			t.Errorf("Join(%v) = %q，期望 %q", c.elem, got, c.want)
		}
	}
	// 无参数等于后台根
	if got := r.Join(); got != "/manage" {
		t.Errorf("Join() = %q，期望 /manage", got)
	}
}

// Load 用于重启后恢复当前路径与历史路径，
// 之前漏了持久化导致重启后旧书签全 404，这个用例守住它。
func TestResolverLoad(t *testing.T) {
	r := New("admin")
	r.Load("backstage", []string{"admin", "manage", "backstage", "", "  "})

	if r.Path() != "backstage" {
		t.Fatalf("Path() = %q，期望 backstage", r.Path())
	}
	legacy := r.Legacy()
	if len(legacy) != 2 {
		t.Fatalf("Legacy() = %v，期望 2 条（去重、去空、不含当前）", legacy)
	}
	seen := map[string]bool{}
	for _, v := range legacy {
		if seen[v] {
			t.Errorf("Legacy 出现重复项 %q", v)
		}
		seen[v] = true
		if v == "backstage" {
			t.Error("Legacy 不应包含当前路径")
		}
	}

	// Load 的值非法时退回默认，不能让进程带着坏路径起来
	r2 := New("admin")
	r2.Load("assets", nil)
	if r2.Path() != Default() {
		t.Errorf("Load 非法值后 Path() = %q，期望默认 %q", r2.Path(), Default())
	}
}

func TestSilentSwitch(t *testing.T) {
	r := New("admin")
	if r.Silent() {
		t.Error("静默模式默认应为关闭")
	}
	r.SetSilent(true)
	if !r.Silent() {
		t.Error("SetSilent(true) 后应为开启")
	}
	r.SetSilent(false)
	if r.Silent() {
		t.Error("SetSilent(false) 后应为关闭")
	}
}
