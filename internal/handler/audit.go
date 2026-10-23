package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/gocms/internal/middleware"
	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/utils"
)

// 审计动作标识。
// 命名规则：<对象>.<动词>，便于按前缀筛选（如 post. 开头的都是文章相关）。
const (
	AuditLoginOK   = "auth.login"
	AuditLoginFail = "auth.login_fail"
	AuditLogout    = "auth.logout"

	AuditSettingsSave = "settings.save"
	AuditAdminPath    = "settings.admin_path"
	AuditIPAllow      = "settings.ip_allow"
	AuditThemeSwitch  = "theme.switch"

	// 二次验证（修改高危安全项时的密码确认）
	AuditReauthOK   = "settings.reauth_ok"
	AuditReauthFail = "settings.reauth_fail"

	// 日志自身的管理动作
	AuditAuditExport = "audit.export"
	AuditAuditPurge  = "audit.purge"

	AuditPostCreate = "post.create"
	AuditPostUpdate = "post.update"
	AuditPostDelete = "post.delete"
	AuditPostBulk   = "post.bulk"

	AuditPageSave   = "page.save"
	AuditPageDelete = "page.delete"

	AuditCategorySave   = "category.save"
	AuditCategoryDelete = "category.delete"

	AuditTagSave   = "tag.save"
	AuditTagDelete = "tag.delete"

	AuditMediaUpload = "media.upload"
	AuditMediaDelete = "media.delete"

	AuditBannerSave    = "banner.save"
	AuditBannerDelete  = "banner.delete"
	AuditLinkSave      = "link.save"
	AuditLinkDelete    = "link.delete"
	AuditMenuSave      = "menu.save"
	AuditMenuDelete    = "menu.delete"
	AuditMessageReply  = "message.reply"
	AuditMessageDelete = "message.delete"

	AuditUserCreate = "user.create"
	AuditUserUpdate = "user.update"
	AuditUserDelete = "user.delete"

	AuditFieldSave   = "field.save"
	AuditFieldDelete = "field.delete"
)

// audit 记录一条操作日志。
//
// 设计取舍：
//   - 写库失败只记 slog，不返回给调用方 —— 审计永远不该阻断业务
//   - detail 会被截断到 500 字符，避免超长内容写不进去
//   - 绝不记录密码、token 等敏感值，由调用方保证
func (h *Handler) audit(r *http.Request, action, target, detail string) {
	if h.repo == nil || h.repo.Audit == nil {
		return
	}
	l := &model.AuditLog{
		Action: action,
		Target: truncateRunes(strings.TrimSpace(target), 120),
		Detail: truncateRunes(strings.TrimSpace(detail), 500),
		IP:     utils.ClientIP(r),
	}
	if u := middleware.UserFrom(r); u != nil {
		l.UserID = u.ID
		l.Username = u.Username
	} else {
		// 登录失败等未登录场景，用户名由调用方放进 target
		l.Username = ""
	}
	if err := h.repo.Audit.Create(l); err != nil {
		slog.Warn("写入审计日志失败", "action", action, "err", err)
	}
}

// auditAs 指定操作人与用户 ID。
//
// 用于登录成功/失败这类「Session 里还没有用户」的场景：
// 登录成功时 context 尚未刷新，必须显式传入 u.ID，否则日志里 user_id 会缺失。
func (h *Handler) auditAs(r *http.Request, userID uint64, username, action, target, detail string) {
	if h.repo == nil || h.repo.Audit == nil {
		return
	}
	l := &model.AuditLog{
		UserID:   userID,
		Username: truncateRunes(username, 50),
		Action:   action,
		Target:   truncateRunes(strings.TrimSpace(target), 120),
		Detail:   truncateRunes(strings.TrimSpace(detail), 500),
		IP:       utils.ClientIP(r),
	}
	if l.UserID == 0 {
		if u := middleware.UserFrom(r); u != nil {
			l.UserID = u.ID
			if l.Username == "" {
				l.Username = u.Username
			}
		}
	}
	if err := h.repo.Audit.Create(l); err != nil {
		slog.Warn("写入审计日志失败", "action", action, "err", err)
	}
}

// truncateRunes 按字符（而非字节）截断，避免把中文切成乱码
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// purgeOldAuditLogs 清理过期审计日志，保留 180 天。
// 由 main 的定时任务调用。
func (h *Handler) PurgeOldAuditLogs() int64 {
	if h.repo == nil || h.repo.Audit == nil {
		return 0
	}
	return h.repo.Audit.PurgeBefore(time.Now().AddDate(0, 0, -180))
}
