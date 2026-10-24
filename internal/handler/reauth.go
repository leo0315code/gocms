package handler

import (
	"net/http"
	"strings"

	"github.com/leo0315code/gocms/internal/adminpath"
	"github.com/leo0315code/gocms/internal/middleware"
	"github.com/leo0315code/gocms/internal/security"
	"github.com/leo0315code/gocms/internal/utils"
)

// 二次验证：修改高危安全项时必须重新输入当前密码。
//
// 覆盖项：
//   - admin_path      后台入口路径（改错就进不去后台）
//   - admin_ip_allow  IP 白名单（改错就把自己挡在门外）
//
// 判定原则：只有「新值是合法的、且与当前值确实不同」才要求验证。
// 值非法时后面本来就会拒绝并提示，此时再要求输密码只会让用户困惑。

// needReauth 判断本次提交是否触碰了高危项
func (h *Handler) needReauth(r *http.Request, hasPath bool, rawPath string, hasIP bool, rawIP string) bool {
	if hasPath {
		// 非法值不要求密码：后面会直接报错，不必让用户先猜密码
		if clean, ok := adminpath.Sanitize(rawPath); ok && clean != h.ap.Path() {
			return true
		}
	}
	if hasIP {
		// 比较规范化后的规则，避免仅空白差异就要求输密码
		cur := h.repo.Set.Get("admin_ip_allow")
		if normalizeIPText(rawIP) != normalizeIPText(cur) {
			return true
		}
	}
	return false
}

// normalizeIPText 把白名单文本归一化：去空行、去注释、去空白、排序
func normalizeIPText(s string) string {
	list, _ := security.ParseIPAllowList(s)
	rules := list.Rules()
	if len(rules) == 0 {
		return ""
	}
	sorted := make([]string, len(rules))
	copy(sorted, rules)
	// 简单插入排序，规则数量很少
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return strings.Join(sorted, "\n")
}

// checkReauth 校验密码。返回空字符串表示通过。
func (h *Handler) checkReauth(r *http.Request) string {
	u := middleware.UserFrom(r)
	if u == nil {
		return "会话已失效，请重新登录"
	}

	// 复用登录限流器，防止有人拿已登录会话当跳板暴力猜密码
	ip := utils.ClientIP(r)
	key := "reauth:" + ip
	if !h.loginLimit.Allow(key) {
		h.audit(r, AuditReauthFail, "security", "密码验证尝试过于频繁")
		return "密码验证失败次数过多，请稍后再试"
	}

	pwd := r.FormValue("confirm_password")
	if pwd == "" {
		h.audit(r, AuditReauthFail, "security", "未提供密码")
		return "修改后台路径或 IP 白名单需要输入当前密码"
	}
	if !security.CheckPassword(u.Password, pwd) {
		h.audit(r, AuditReauthFail, "security", "密码错误")
		return "当前密码不正确，修改未生效"
	}

	// 验证通过，清掉该 IP 的失败计数
	h.loginLimit.Reset(key)
	return ""
}
