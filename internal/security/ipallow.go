package security

import (
	"net"
	"net/netip"
	"strings"
)

// IPAllowList 后台访问 IP 白名单。
//
// 支持三种写法（每行一条，# 开头为注释）：
//
//	192.168.1.10            单个 IPv4
//	10.0.0.0/8              网段（CIDR）
//	::1                     单个 IPv6
//
// 空列表表示不限制。
type IPAllowList struct {
	rules []netip.Prefix
	raw   []string
}

// EmptyIPAllowList 返回一个不限制任何 IP 的白名单
func EmptyIPAllowList() *IPAllowList { return &IPAllowList{} }

// ParseIPAllowList 解析白名单文本。
//
// 返回的列表只包含解析成功的条目；invalid 返回无法识别的原始行，
// 由调用方决定是告警还是拒绝启动——静默忽略会让管理员误以为限制已生效。
func ParseIPAllowList(text string) (list *IPAllowList, invalid []string) {
	list = &IPAllowList{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, ok := parseRule(line)
		if !ok {
			invalid = append(invalid, line)
			continue
		}
		list.rules = append(list.rules, p)
		list.raw = append(list.raw, line)
	}
	return list, invalid
}

// parseRule 把单条规则转成 netip.Prefix。
// 单个 IP 会自动补全成 /32（IPv4）或 /128（IPv6）。
func parseRule(s string) (netip.Prefix, bool) {
	// 去掉可能带上的方括号，如 [::1]
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")

	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, false
		}
		// 归一化：去掉主机位，避免 10.0.0.5/8 这种写法行为不确定
		return p.Masked(), true
	}

	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap() // ::ffff:1.2.3.4 → 1.2.3.4
	return netip.PrefixFrom(addr, addr.BitLen()), true
}

// Empty 是否为空（= 不限制）
func (l *IPAllowList) Empty() bool { return l == nil || len(l.rules) == 0 }

// Contains 判断 IP 是否在白名单内。列表为空时永远返回 true。
func (l *IPAllowList) Contains(ip string) bool {
	if l.Empty() {
		return true
	}
	addr, ok := parseAddr(ip)
	if !ok {
		return false
	}
	for _, p := range l.rules {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Rules 返回原始规则文本，用于后台回显
func (l *IPAllowList) Rules() []string {
	if l == nil {
		return nil
	}
	return l.raw
}

// parseAddr 解析字符串 IP，兼容 "1.2.3.4:5678" 与 IPv4-mapped IPv6
func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	// 带端口时先剥端口
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
