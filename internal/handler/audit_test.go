package handler

import "testing"

// 导出 CSV 时，以 = + - @ 开头的单元格会被 Excel / WPS 当公式执行。
// 审计字段里有用户可控内容（用户名、文章标题），必须转义。
func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"正常内容":      "正常内容",
		"":          "",
		"article":   "article",
		"=1+1":      "'=1+1",
		"+86":       "'+86",
		"-2":        "'-2",
		"@SUM(A1)":  "'@SUM(A1)",
		"=cmd|'/c'": "'=cmd|'/c'",
		"中文 = 号":    "中文 = 号", // 首字符不是危险符号，不需处理
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q，期望 %q", in, got, want)
		}
	}
}
