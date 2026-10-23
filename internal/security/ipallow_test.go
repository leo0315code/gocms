package security

import "testing"

// IP 白名单是「配错就把自己锁死」的功能，解析必须严格：
// 宁可报错让用户改，也不能把看不懂的行默默当成放行。
func TestParseIPAllowList(t *testing.T) {
	text := `
# 办公室固定 IP
203.0.113.10
192.168.1.0/24

::1
  # 缩进注释也允许

`
	list, invalid := ParseIPAllowList(text)
	if len(invalid) != 0 {
		t.Fatalf("不应有非法行，实际 %v", invalid)
	}
	if got := len(list.Rules()); got != 3 {
		t.Fatalf("规则数 = %d，期望 3（注释与空行不计）", got)
	}
	if list.Empty() {
		t.Error("Empty() 应为 false")
	}

	cases := map[string]bool{
		"203.0.113.10": true,
		"192.168.1.77": true,
		"192.168.2.1":  false, // 相邻网段
		"10.1.2.3":     false,
		"::1":          true,
		"203.0.113.11": false,
		"not-an-ip":    false,
		"":             false,
	}
	for ip, want := range cases {
		if got := list.Contains(ip); got != want {
			t.Errorf("Contains(%q) = %v，期望 %v", ip, got, want)
		}
	}
}

// 无法识别的行必须被报出来。否则用户以为「加了 office 网段」，
// 实际那一行被静默忽略，等出事才发现没生效。
func TestParseIPAllowListInvalid(t *testing.T) {
	list, invalid := ParseIPAllowList("10.0.0.0/8\nbad-ip\n300.1.1.1\n10.0.0.0/33\n")
	if len(invalid) != 3 {
		t.Fatalf("非法行数 = %d，期望 3，实际 %v", len(invalid), invalid)
	}
	// 合法的部分要保留，不能因为一行写错就整组失效
	if got := len(list.Rules()); got != 1 {
		t.Errorf("合法规则数 = %d，期望 1", got)
	}
}

// 空列表的语义是「不限制」——Contains 必须返回 true。
//
// 这不是随手写的：后台保存白名单时会做「新规则是否包含当前 IP」的自锁检查，
// 如果空列表返回 false，那「清空白名单以取消限制」这个操作会被自己拒绝掉。
// 同时调用方要用 Empty() 区分「没配置」和「配了但不含你」，两者行为完全不同。
func TestEmptyAllowList(t *testing.T) {
	var nilList *IPAllowList
	if !nilList.Empty() {
		t.Error("nil 列表应视为空")
	}
	if !nilList.Contains("127.0.0.1") {
		t.Error("nil 列表 = 不限制，Contains 应返回 true")
	}

	list := EmptyIPAllowList()
	if !list.Empty() {
		t.Error("空列表应 Empty() == true")
	}
	if !list.Contains("127.0.0.1") {
		t.Error("空列表 = 不限制，Contains 应返回 true")
	}

	// 只有注释同样视为空（用户可能写了注释但把规则全删了）
	onlyComments, invalid := ParseIPAllowList("# 只有注释\n\n")
	if len(invalid) != 0 {
		t.Errorf("注释不应算非法：%v", invalid)
	}
	if !onlyComments.Empty() {
		t.Error("只有注释的列表应视为空（不限制）")
	}
	if !onlyComments.Contains("8.8.8.8") {
		t.Error("只有注释 = 不限制，Contains 应返回 true")
	}
}

// Rules() 用于设置页回显与变更检测，必须去注释、去空行、稳定顺序。
func TestRulesNormalized(t *testing.T) {
	a, _ := ParseIPAllowList("10.0.0.0/8\n# 注释\n192.168.0.0/16")
	b, _ := ParseIPAllowList("192.168.0.0/16\n\n10.0.0.0/8\n")
	ra, rb := a.Rules(), b.Rules()
	if len(ra) != 2 || len(rb) != 2 {
		t.Fatalf("规则数不对：%v / %v", ra, rb)
	}
	// 顺序可以不同，但集合必须一致 —— 二次验证的「实质变更」判断依赖这一点
	set := map[string]bool{}
	for _, r := range ra {
		set[r] = true
	}
	for _, r := range rb {
		if !set[r] {
			t.Errorf("规范化后集合不一致：%q 不在 %v 中", r, ra)
		}
	}
}
