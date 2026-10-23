package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/example/gocms/internal/repository"
	"github.com/example/gocms/internal/utils"
)

// AuditList 操作日志列表
//
// 只有 admin 可见（路由层用 authAdmin 保护）。
// 支持按操作人、动作类型、关键字（对象/说明）筛选。
func (h *Handler) AuditList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "操作日志", "audits")

	page := utils.QueryInt(r, "page", 1)
	perPage := 30
	f := &repository.AuditFilter{
		Username: utils.QueryStr(r, "user"),
		Action:   utils.QueryStr(r, "action"),
		Keyword:  utils.QueryStr(r, "q"),
		Page:     page,
		PerPage:  perPage,
	}

	list, total, err := h.repo.Audit.List(f)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载日志失败")
		return
	}

	// 分页链接要带上筛选条件，否则翻页就丢失筛选
	base := h.adminPath("audits") + "?" + filterQuery(r)
	d.Items = list
	d.Total = total
	d.Query = f.Keyword
	d.Status = f.Action
	d.Pagination = utils.NewPagination(page, perPage, total, base)
	d.Options = map[string]any{
		"user":    f.Username,
		"action":  f.Action,
		"actions": h.repo.Audit.DistinctActions(),
		"users":   h.repo.Audit.DistinctUsers(),
		"total":   total,
	}
	h.renderAdmin(w, r, "audit_list", d)
}

// filterQuery 把当前筛选条件拼成查询串（不含 page）
func filterQuery(r *http.Request) string {
	v := url.Values{}
	for _, k := range []string{"user", "action", "q"} {
		if s := r.URL.Query().Get(k); s != "" {
			v.Set(k, s)
		}
	}
	return v.Encode()
}

// AuditPurge 清理 180 天前的日志
func (h *Handler) AuditPurge(w http.ResponseWriter, r *http.Request) {
	n := h.PurgeOldAuditLogs()
	h.audit(r, "audit.purge", "180 天前", "清理 "+strconv.FormatInt(n, 10)+" 条")
	h.redirect(w, r, "/admin/audits", "success", "已清理 "+strconv.FormatInt(n, 10)+" 条历史日志")
}
