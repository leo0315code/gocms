package repository

import "testing"

// Search 关键字转布尔模式短语串的规则：
//   - < 2 字符低于 ngram 最小 token 长度，返回空让调用方退回 LIKE
//   - 包成双引号做短语检索，避免裸词被 ngram 分词后各 token 之间 OR 误召回
//   - 双引号与反斜杠会破坏布尔模式语法，必须剥掉
func TestFulltextKeyword(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{" a ", ""}, // 单字符（ASCII）
		{" 花 ", ""}, // 单字符（中文）
		{"  ", ""},  // 纯空白
		{`"`, ""},   // 剥掉引号后为空
		{"花园", `"花园"`},
		{" 企业数字化 ", `"企业数字化"`},
		{`bad"quote`, `"badquote"`},
		{`back\slash`, `"backslash"`},
	}
	for _, c := range cases {
		if got := fulltextKeyword(c.in); got != c.want {
			t.Errorf("fulltextKeyword(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
