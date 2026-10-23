package handler

import (
	"encoding/csv"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

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
	// 导出链接在这里拼好。
	// 不能把查询串直接塞进模板的 href：html/template 会对 URL 上下文做转义，
	// "&" 变成 %26、"=" 变成 %3D，服务端收到的就不是多个参数了，筛选会失效。
	// 用 template.URL 声明「这已经是安全的完整 URL」，模板才不会二次转义。
	exportURL := h.adminPath("audits", "export.csv")
	if qs := filterQuery(r); qs != "" {
		exportURL += "?" + qs
	}

	d.Options = map[string]any{
		"user":       f.Username,
		"action":     f.Action,
		"actions":    h.repo.Audit.DistinctActions(),
		"users":      h.repo.Audit.DistinctUsers(),
		"total":      total,
		"export_url": template.URL(exportURL),
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
	h.audit(r, AuditAuditPurge, "180 天前", "清理 "+strconv.FormatInt(n, 10)+" 条")
	h.redirect(w, r, "/admin/audits", "success", "已清理 "+strconv.FormatInt(n, 10)+" 条历史日志")
}

// auditExportMax 单次导出上限。
// 日志表可能有几十万行，不设上限会一次性拉爆内存；超出的请先用筛选条件缩小范围。
const auditExportMax = 10000

// AuditExportCSV 导出当前筛选结果为 CSV。
//
// 交接场景最需要：把「谁在什么时候改了什么」交给接手的人，或者供外部审计。
// 导出沿用页面上的筛选条件，导出的就是用户眼前看到的那一批。
func (h *Handler) AuditExportCSV(w http.ResponseWriter, r *http.Request) {
	f := &repository.AuditFilter{
		Username: utils.QueryStr(r, "user"),
		Action:   utils.QueryStr(r, "action"),
		Keyword:  utils.QueryStr(r, "q"),
		Page:     1,
		PerPage:  auditExportMax,
	}
	list, total, err := h.repo.Audit.List(f)
	if err != nil {
		h.redirect(w, r, "/admin/audits", "error", "导出失败："+err.Error())
		return
	}

	name := "audit-" + time.Now().Format("20060102-150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	// 导出内容是敏感操作记录，禁止任何中间层缓存
	w.Header().Set("Cache-Control", "no-store")
	// Excel 只有看到 BOM 才认 UTF-8，否则中文全是乱码
	_, _ = w.Write([]byte("\xef\xbb\xbf"))

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"时间", "操作人", "动作", "对象", "说明", "IP"})
	for _, l := range list {
		_ = cw.Write([]string{
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			csvSafe(l.Username),
			csvSafe(l.Action),
			csvSafe(l.Target),
			csvSafe(l.Detail),
			csvSafe(l.IP),
		})
	}
	cw.Flush()

	detail := "导出 " + strconv.Itoa(len(list)) + " 条"
	if total > len(list) {
		detail += "（共 " + strconv.Itoa(total) + " 条，已截断到上限）"
	}
	if q := filterQuery(r); q != "" {
		detail += "，筛选：" + q
	}
	h.audit(r, AuditAuditExport, name, detail)
}

// csvSafe 防 CSV 注入。
//
// Excel / WPS 会把以 = + - @ 开头的单元格当公式执行，而审计字段里有
// 用户可控内容（用户名、文章标题）。前缀单引号可让它按纯文本显示。
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
