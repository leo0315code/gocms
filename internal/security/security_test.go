package security

import (
	"strings"
	"testing"
)

// SafeRedirect 防开放重定向。
// 登录页的 next= 参数完全由用户控制，一旦放过外站地址，
// 后台就成了钓鱼跳板（受害者看到的是自己的域名，落地却是攻击者站）。
func TestSafeRedirect(t *testing.T) {
	const fallback = "/admin"

	allow := []string{
		"/admin",
		"/admin/posts",
		"/admin/posts?page=2&keyword=x",
		"/a/hello-world",
	}
	for _, in := range allow {
		if got := SafeRedirect(in, fallback); got != in {
			t.Errorf("SafeRedirect(%q) = %q，应原样返回", in, got)
		}
	}

	reject := []string{
		"",
		"http://evil.com",
		"https://evil.com/admin",
		"//evil.com",
		"//evil.com/admin",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"admin", // 无前导斜杠，会被当相对路径解析到 /a/admin 之类，直接拒
		"ftp://evil.com",
	}
	for _, in := range reject {
		if got := SafeRedirect(in, fallback); got != fallback {
			t.Errorf("SafeRedirect(%q) = %q，应回退到 %q", in, got, fallback)
		}
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	// 只关心「能否通过」，具体文案在 UI 上展示，不锁死避免改文案就红
	bad := []string{
		"",
		"abc1234",  // 7 位
		"abcdefgh", // 无数字
		"12345678", // 无字母
		"中文密码一二三四", // 无数字
	}
	for _, p := range bad {
		if err := ValidatePasswordStrength(p); err == nil {
			t.Errorf("密码 %q 应被判为过弱", p)
		}
	}

	good := []string{
		"abcd1234",
		"Admin@123456",
		"中文密码abc123",
	}
	for _, p := range good {
		if err := ValidatePasswordStrength(p); err != nil {
			t.Errorf("密码 %q 应通过，却报 %v", p, err)
		}
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("Admin@123456")
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("哈希格式不像 bcrypt: %q", hash)
	}
	if hash == "Admin@123456" {
		t.Fatal("哈希结果不能等于明文")
	}
	if !CheckPassword(hash, "Admin@123456") {
		t.Error("正确密码校验失败")
	}
	if CheckPassword(hash, "Tvner@357931") {
		t.Error("错误密码竟然通过了")
	}
	// 空哈希不能意外通过（脏数据兜底）
	if CheckPassword("", "") {
		t.Error("空哈希对空密码不应通过")
	}
	if CheckPassword("", "anything") {
		t.Error("空哈希不应通过任何密码")
	}
}

// 文章正文允许富文本，但必须掐掉脚本类内容。
func TestSanitizeHTML(t *testing.T) {
	cases := []struct {
		in       string
		mustDrop []string
		mustKeep []string
	}{
		{
			in:       `<p>正常段落</p><script>alert(1)</script>`,
			mustDrop: []string{"<script", "alert(1)"},
			mustKeep: []string{"正常段落"},
		},
		{
			in:       `<a href="javascript:alert(1)">点我</a>`,
			mustDrop: []string{"javascript:"},
			mustKeep: []string{"点我"},
		},
		{
			in:       `<img src="x" onerror="alert(1)">`,
			mustDrop: []string{"onerror"},
		},
		{
			in:       `<h2>标题</h2><ul><li>要点</li></ul>`,
			mustKeep: []string{"<h2>", "<li>", "标题", "要点"},
		},
	}
	for _, c := range cases {
		out := SanitizeHTML(c.in)
		for _, d := range c.mustDrop {
			if strings.Contains(strings.ToLower(out), strings.ToLower(d)) {
				t.Errorf("SanitizeHTML(%q) 应剔除 %q，实际输出 %q", c.in, d, out)
			}
		}
		for _, k := range c.mustKeep {
			if !strings.Contains(out, k) {
				t.Errorf("SanitizeHTML(%q) 应保留 %q，实际输出 %q", c.in, k, out)
			}
		}
	}
}

// Slugify 的契约（与实现一致，别按「转拼音」的直觉去假设）：
//   - 保留汉字，现代浏览器 URL 支持，转拼音成本高且易错
//   - 下划线、空格、标点都当分隔符压成单个连字符
//   - 全空时兜底随机串，绝不给空 slug（否则文章详情页路由会 404）
func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World": "hello-world",
		"  多  空格  ":   "多-空格",
		"Go 语言入门":     "go-语言入门",
		"a--b":        "a-b",
		"UPPER_case":  "upper-case",
		"---":         "", // 只有分隔符，走兜底分支，单独断言
	}
	for in, want := range cases {
		got := Slugify(in)
		if want == "" {
			if got == "" {
				t.Errorf("Slugify(%q) 返回空，应兜底随机串", in)
			}
			continue
		}
		if got != want {
			t.Errorf("Slugify(%q) = %q，期望 %q", in, got, want)
		}
	}

	// 空输入必须兜底，不能返回空串
	if got := Slugify(""); got == "" {
		t.Error("Slugify(\"\") 不能返回空")
	}

	// 超长输入截断到 100 字符以内
	long := strings.Repeat("很长的标题", 40)
	if n := len([]rune(Slugify(long))); n > 100 {
		t.Errorf("超长标题截断后 %d 字符，应 <= 100", n)
	}
}

func TestRandomTokenAndCompare(t *testing.T) {
	a := RandomToken(32)
	b := RandomToken(32)
	if len(a) != 64 {
		t.Errorf("RandomToken(32) 长度 = %d，期望 64（hex）", len(a))
	}
	if a == b {
		t.Fatal("两次生成的 token 相同")
	}
	if !ConstantTimeEqual(a, a) {
		t.Error("相同字符串比较应为 true")
	}
	if ConstantTimeEqual(a, b) {
		t.Error("不同字符串比较应为 false")
	}
	if ConstantTimeEqual(a, a+"x") {
		t.Error("长度不同应为 false")
	}
}
