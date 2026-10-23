package utils

import (
	"net/http"
	"testing"
)

func testRequest(remoteAddr, realIP, xff string) *http.Request {
	r, _ := http.NewRequest("POST", "/", nil)
	r.RemoteAddr = remoteAddr
	if realIP != "" {
		r.Header.Set("X-Real-IP", realIP)
	}
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

// ClientIP 是限流、IP 白名单、审计日志共用的取 IP 入口，
// 取错一侧等于让攻击者自报家门绕过所有基于 IP 的防护。
func TestClientIP(t *testing.T) {
	// 信任代理时：XFF 是追加式头，客户端可自带任意前缀，
	// 只有最右侧一跳（信任代理写入的）可信。
	SetTrustProxy(true)
	defer SetTrustProxy(false)

	cases := []struct {
		name       string
		remoteAddr string
		realIP     string
		xff        string
		want       string
	}{
		{
			name:       "XFF 带伪造前缀时取最右侧",
			remoteAddr: "203.0.113.9:5555",
			xff:        "1.2.3.4, 10.0.0.9, 203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "XFF 末项带空白时仍取得到地址",
			remoteAddr: "203.0.113.9:5555",
			xff:        "1.2.3.4, 203.0.113.9 ",
			want:       "203.0.113.9",
		},
		{
			name:       "X-Real-IP 优先（代理覆盖写入，无追加问题）",
			remoteAddr: "203.0.113.9:5555",
			realIP:     "198.51.100.7",
			xff:        "1.2.3.4, 203.0.113.9",
			want:       "198.51.100.7",
		},
		{
			name:       "两个头都没有时回退 TCP 对端地址",
			remoteAddr: "203.0.113.9:5555",
			want:       "203.0.113.9",
		},
		{
			name:       "IPv6 对端地址",
			remoteAddr: "[2001:db8::1]:443",
			want:       "[2001:db8::1]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClientIP(testRequest(c.remoteAddr, c.realIP, c.xff)); got != c.want {
				t.Errorf("ClientIP() = %q，期望 %q", got, c.want)
			}
		})
	}

	// 不信任代理时：任何请求头都不可信，一律取 TCP 对端地址。
	SetTrustProxy(false)
	r := testRequest("203.0.113.9:5555", "198.51.100.7", "1.2.3.4")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Errorf("TRUST_PROXY=0 时 ClientIP() = %q，期望 TCP 对端地址 203.0.113.9", got)
	}
}
