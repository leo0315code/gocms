package handler

import (
	"net/http"

	"github.com/leo0315code/gocms/internal/utils"
)

// 在线会话管理：查看当前登录会话、单个踢下线、按用户全部踢下线。
//
// 场景：怀疑账号泄露、员工离职回收、某设备异常时，管理员需要能立刻
// 让对应会话失效。服务端 Session 表使这件事成为可能——JWT 做不到。
//
// 权限：仅 admin（RequireRole("admin") 在路由层）。会话涉及全部账号的
// 登录态，editor / viewer 不应看到也不应能操作。

// SessionsPage 在线会话列表
func (h *Handler) SessionsPage(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "在线会话", "sessions")
	list, err := h.repo.Sess.ListActive()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载会话失败："+err.Error())
		return
	}
	d.Items = list
	d.Total = len(list)

	// 同一用户可能有多条会话（多设备），提供「全部踢下线」的用户清单
	seen := map[uint64]bool{}
	users := make([]map[string]any, 0, len(list))
	for _, s := range list {
		if seen[s.UserID] {
			continue
		}
		seen[s.UserID] = true
		users = append(users, map[string]any{"id": s.UserID, "label": h.sessionOwnerName(s.UserID)})
	}
	d.Options = map[string]any{"users": users}
	h.renderAdmin(w, r, "session_list", d)
}

// SessionKick 踢掉单个会话
func (h *Handler) SessionKick(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/sessions", "error", "参数错误")
		return
	}
	s, err := h.repo.Sess.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/sessions", "error", "会话不存在或已过期")
		return
	}
	name := h.sessionOwnerName(s.UserID)
	if err := h.repo.Sess.DeleteByID(id); err != nil {
		h.redirect(w, r, "/admin/sessions", "error", "踢下线失败："+err.Error())
		return
	}
	h.audit(r, AuditSessionKick, name, "单会话 · IP "+s.IP)
	h.redirect(w, r, "/admin/sessions", "success", name+" 的会话已踢下线")
}

// SessionKickUser 踢掉某用户全部会话
func (h *Handler) SessionKickUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/sessions", "error", "表单解析失败")
		return
	}
	uid := utils.FormUint(r, "user_id", 0)
	if uid == 0 {
		h.redirect(w, r, "/admin/sessions", "error", "请选择用户")
		return
	}
	name := h.sessionOwnerName(uid)
	if err := h.repo.Sess.DeleteByUser(uid); err != nil {
		h.redirect(w, r, "/admin/sessions", "error", "踢下线失败："+err.Error())
		return
	}
	h.audit(r, AuditSessionKick, name, "该用户全部会话")
	h.redirect(w, r, "/admin/sessions", "success", name+" 的全部会话已踢下线")
}

// sessionOwnerName 用户 ID 转展示名（审计与提示用）；查不到返回占位
func (h *Handler) sessionOwnerName(userID uint64) string {
	if u, err := h.repo.User.ByID(userID); err == nil {
		if u.Nickname != "" {
			return u.Nickname + "（" + u.Username + "）"
		}
		return u.Username
	}
	return "用户 #" + utils.UintStr(userID)
}
