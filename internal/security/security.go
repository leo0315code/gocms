package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/crypto/bcrypt"
)

// ---------- 密码 ----------

// HashPassword 生成 bcrypt 哈希
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword 校验密码
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// ValidatePasswordStrength 密码强度：至少 8 位，含字母与数字
func ValidatePasswordStrength(p string) error {
	if len([]rune(p)) < 8 {
		return fmt.Errorf("密码至少 8 位")
	}
	var hasLetter, hasDigit bool
	for _, r := range p {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("密码需同时包含字母和数字")
	}
	return nil
}

// ---------- 随机 token ----------

// RandomToken 生成 n 字节的十六进制串（n*2 长度）
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下退化为时间戳，但正常不会发生
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

// ConstantTimeEqual 常量时间比较，防时序攻击
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---------- HTML 净化 ----------

var richPolicy = buildRichPolicy()

func buildRichPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-zA-Z0-9\s\-_]+$`)).OnElements(
		"p", "div", "span", "table", "td", "tr", "th", "thead", "tbody", "ul", "ol", "li",
		"h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "code", "img", "a",
	)
	p.AllowAttrs("style").Matching(regexp.MustCompile(`^[a-zA-Z0-9\s:#;,.\-\(\)%]+$`)).OnElements(
		"p", "div", "span", "td", "th", "table", "img", "figure", "figcaption",
	)
	p.AllowAttrs("src", "alt", "width", "height", "loading").OnElements("img")
	p.AllowAttrs("href", "title", "target", "rel").OnElements("a")
	p.AllowAttrs("colspan", "rowspan", "border", "cellpadding", "cellspacing").OnElements("td", "th", "table")
	p.AllowAttrs("data-*").Globally() // 供编辑器/前端组件使用
	p.AllowStandardURLs()
	p.AllowURLSchemes("http", "https", "mailto", "tel")
	p.RequireNoFollowOnLinks(false)
	p.RequireNoFollowOnFullyQualifiedLinks(false)
	p.AllowRelativeURLs(true)
	return p
}

// SanitizeHTML 富文本白名单净化（防存储型 XSS）
func SanitizeHTML(s string) string {
	return richPolicy.Sanitize(s)
}

// SanitizePlain 纯文本：去除所有标签
func SanitizePlain(s string) string {
	return bluemonday.StrictPolicy().Sanitize(s)
}

// ---------- Slug ----------

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)
var slugCJK = regexp.MustCompile(`[^\p{Han}a-zA-Z0-9]+`)

// Slugify 生成 URL 友好 slug；中文转拼音成本高，这里保留中文（现代浏览器 URL 支持）
func Slugify(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = slugCJK.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	s = regexp.MustCompile(`-{2,}`).ReplaceAllString(s, "-")
	if s == "" {
		s = RandomToken(4)
	}
	if len([]rune(s)) > 100 {
		s = string([]rune(s)[:100])
		s = strings.Trim(s, "-")
	}
	return s
}

// SlugifyASCII 仅保留 ASCII，用于文件命名
func SlugifyASCII(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugInvalid.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ---------- 输入校验 ----------

var (
	rePhone = regexp.MustCompile(`^1[3-9]\d{9}$`)
	reEmail = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	reURL   = regexp.MustCompile(`^https?://[^\s]+$`)
)

// IsPhone 大陆手机号
func IsPhone(s string) bool { return rePhone.MatchString(strings.TrimSpace(s)) }

// IsEmail 邮箱
func IsEmail(s string) bool { return reEmail.MatchString(strings.TrimSpace(s)) }

// IsHTTPURL 是否是 http/https 链接
func IsHTTPURL(s string) bool { return reURL.MatchString(strings.TrimSpace(s)) }

// SafeRedirect 只允许站内相对路径跳转，防开放重定向
func SafeRedirect(target, fallback string) string {
	if target == "" {
		return fallback
	}
	u, err := url.Parse(target)
	if err != nil {
		return fallback
	}
	if u.IsAbs() || u.Host != "" {
		return fallback
	}
	if !strings.HasPrefix(target, "/") {
		return fallback
	}
	return target
}
