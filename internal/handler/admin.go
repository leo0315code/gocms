package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/leo0315code/gocms/internal/middleware"
	"github.com/leo0315code/gocms/internal/model"
	"github.com/leo0315code/gocms/internal/repository"
	"github.com/leo0315code/gocms/internal/security"
	"github.com/leo0315code/gocms/internal/utils"
)

// AdminData 后台页面公共数据
type AdminData struct {
	Title      string
	Menu       string
	SubMenu    string
	User       *model.User
	CSRF       string
	SiteName   string
	Unread     int
	Flash      *Flash
	Version    string
	CurrentURL string

	// AdminPath 当前后台根路径（如 /manage），模板里所有链接都以它为前缀
	AdminPath string

	// 列表
	Items      any
	Total      int
	Pagination *utils.Pagination
	Query      string
	Status     string

	// 表单
	Item         any
	Categories   []*model.Category
	CatTree      []*model.Category
	Tags         []*model.Tag
	Fields       []*model.Field
	Values       map[uint64]string
	Items2       any
	Error        string
	Form         map[string]string
	Options      map[string]any
	Stats        map[string]int
	Settings     []*model.Setting
	Groups       []string
	Themes       []ThemeInfo
	CurrentTheme string
	JSON         string

	// 上传限制（MB），供模板展示
	MaxMB int64
}

// ThemeInfo 主题信息
type ThemeInfo struct {
	Dir         string
	Name        string
	Version     string
	Author      string
	Description string
	Screenshot  string
	Active      bool
}

func (h *Handler) adminView(r *http.Request, title, menu string) *AdminData {
	sess := middleware.SessionFrom(r)
	csrf := ""
	if sess != nil {
		csrf = sess.CSRFToken
	}
	site := h.siteMap()
	d := &AdminData{
		Title:      title,
		Menu:       menu,
		User:       middleware.UserFrom(r),
		CSRF:       csrf,
		SiteName:   site["site_name"],
		Unread:     h.repo.Msg.CountUnread(),
		Version:    "1.0.0",
		CurrentURL: r.URL.RequestURI(),
		MaxMB:      h.cfg.UploadMaxMB,
		AdminPath:  h.adminBase(),
	}
	if d.SiteName == "" {
		d.SiteName = h.cfg.Name
	}
	if f := readFlash(r); f != nil {
		d.Flash = f
	}
	return d
}

func (h *Handler) renderAdmin(w http.ResponseWriter, r *http.Request, tpl string, d *AdminData) {
	body, err := h.rnd.RenderAdmin(tpl, d)
	if err != nil {
		slog.Error("后台模板渲染失败", "tpl", tpl, "err", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<h1>模板渲染失败</h1><pre>" + templateEscapeStr(err.Error()) + "</pre>"))
		return
	}
	if d.Flash != nil {
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

func templateEscapeStr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// ---------- 登录 ----------

// LoginPage 登录页
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if middleware.UserFrom(r) != nil {
		http.Redirect(w, r, h.adminBase(), http.StatusSeeOther)
		return
	}
	d := h.adminView(r, "登录", "")
	d.Options = map[string]any{"next": r.URL.Query().Get("next")}
	h.renderAdmin(w, r, "login", d)
}

// LoginSubmit 登录提交
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/login", "error", "表单解析失败")
		return
	}
	ip := utils.ClientIP(r)
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := security.SafeRedirect(r.FormValue("next"), h.adminBase())

	// 三个维度分别限流，覆盖不同攻击形态：
	//   pair  同 IP + 同账号 —— 常规暴力破解
	//   acct  同账号（跨 IP）—— 分布式撞库 / 代理池
	//   ip    同 IP（跨账号）—— 拿一个字典轮着试不同用户名
	keyPair := "login:pair:" + ip + ":" + username
	keyAcct := "login:acct:" + username
	keyIP := "login:ip:" + ip
	if !h.loginLimit.Allow(keyPair) || !h.loginLimit.Allow(keyAcct) || !h.loginLimit.Allow(keyIP) {
		slog.Warn("登录被限流", "username", username, "ip", ip)
		h.auditAs(r, 0, username, AuditLoginFail, username, "触发登录限流")
		h.redirect(w, r, "/admin/login", "error", "登录失败次数过多，请 15 分钟后再试")
		return
	}

	u, err := h.repo.User.ByUsername(username)
	if err != nil || !security.CheckPassword(u.Password, password) {
		// 统一提示，避免用户名枚举
		slog.Warn("登录失败", "username", username, "ip", ip)
		h.auditAs(r, 0, username, AuditLoginFail, username, "用户名或密码错误")
		h.redirect(w, r, "/admin/login", "error", "用户名或密码错误")
		return
	}
	if u.Status != 1 {
		h.auditAs(r, 0, username, AuditLoginFail, username, "账号已禁用")
		h.redirect(w, r, "/admin/login", "error", "该账号已被禁用")
		return
	}

	sess := middleware.SessionFrom(r)
	if sess == nil {
		h.redirect(w, r, "/admin/login", "error", "会话创建失败，请重试")
		return
	}

	// 重置 token 防会话固定
	newToken := security.RandomToken(32)
	newCSRF := security.RandomToken(32)
	exp := time.Now().Add(h.cfg.SessionLifetime)
	if err := h.repo.Sess.BindUser(sess.ID, u.ID, newToken, newCSRF, exp); err != nil {
		h.redirect(w, r, "/admin/login", "error", "登录失败，请重试")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.SessionName,
		Value:    newToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cfg.SessionLifetime.Seconds()),
	})
	_ = h.repo.User.TouchLogin(u.ID, ip)
	// 登录成功清空该账号与 IP 的失败计数，避免正常用户被自己之前的成功登录锁死
	h.loginLimit.Reset(keyPair)
	h.loginLimit.Reset(keyAcct)
	h.loginLimit.Reset(keyIP)
	slog.Info("登录成功", "username", u.Username, "ip", ip)
	h.auditAs(r, u.ID, u.Username, AuditLoginOK, u.Username, "登录成功")

	h.redirect(w, r, next, "success", "欢迎回来，"+u.DisplayName())
}

// Logout 退出
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if sess := middleware.SessionFrom(r); sess != nil {
		_ = h.repo.Sess.Delete(sess.Token)
	}
	http.SetCookie(w, &http.Cookie{Name: h.cfg.SessionName, Value: "", Path: "/", MaxAge: -1})
	h.redirect(w, r, "/admin/login", "success", "已安全退出")
}

// ---------- 概览 ----------

// Dashboard 后台首页
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "数据概览", "dashboard")
	d.Stats = map[string]int{
		"posts":      h.repo.Post.CountByStatus("published"),
		"drafts":     h.repo.Post.CountByStatus("draft"),
		"pages":      h.repo.Page.Count(),
		"categories": h.repo.Cat.Count(),
		"tags":       h.repo.Tag.Count(),
		"media":      h.repo.Media.Count(),
		"messages":   h.repo.Msg.Count(),
		"unread":     h.repo.Msg.CountUnread(),
		"views":      h.repo.Post.TotalViews(),
	}
	recent, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 8, AdminView: true})
	d.Items = recent
	msgs, _, _ := h.repo.Msg.List("", 1, 6)
	d.Items2 = msgs
	h.renderAdmin(w, r, "dashboard", d)
}

// ---------- 文章 ----------

// PostList 文章列表
func (h *Handler) PostList(w http.ResponseWriter, r *http.Request) {
	page := max(1, utils.QueryInt(r, "page", 1))
	status := utils.QueryStr(r, "status")
	kw := utils.QueryStr(r, "q")
	catID := utils.FormUint(r, "cat", uint64(utils.QueryInt(r, "cat", 0)))

	f := &repository.PostFilter{
		Status:     status,
		Keyword:    kw,
		CategoryID: catID,
		Page:       page,
		PerPage:    15,
		AdminView:  true,
	}
	list, total, err := h.repo.Post.List(f)
	if err != nil {
		h.redirect(w, r, "/admin/posts", "error", "加载失败："+err.Error())
		return
	}
	d := h.adminView(r, "文章管理", "posts")
	d.Items = list
	d.Total = total
	d.Status = status
	d.Query = kw
	base := "/admin/posts?status=" + status
	if kw != "" {
		base += "&q=" + kw
	}
	if catID > 0 {
		base += "&cat=" + strconv.FormatUint(catID, 10)
	}
	d.Pagination = utils.NewPagination(page, 15, total, base)
	tree, _ := h.repo.Cat.Tree()
	d.CatTree = tree
	h.renderAdmin(w, r, "post_list", d)
}

// PostNew 新建文章页
func (h *Handler) PostNew(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "写文章", "posts")
	d.Item = &model.Post{Status: "published", ContentFormat: "html", CategoryID: 0}
	tree, _ := h.repo.Cat.Tree()
	d.CatTree = tree
	tags, _ := h.repo.Tag.List(0)
	d.Tags = tags
	d.Values = map[uint64]string{}
	h.renderAdmin(w, r, "post_form", d)
}

// PostEdit 编辑文章页
func (h *Handler) PostEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/posts", "error", "参数错误")
		return
	}
	p, err := h.repo.Post.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/posts", "error", "文章不存在")
		return
	}
	d := h.adminView(r, "编辑文章", "posts")
	d.Item = p
	tree, _ := h.repo.Cat.Tree()
	d.CatTree = tree
	tags, _ := h.repo.Tag.List(0)
	d.Tags = tags
	if p.CategoryID > 0 {
		if fields, err := h.repo.Field.ListByCategory(p.CategoryID); err == nil {
			d.Fields = fields
		}
	}
	values := make(map[uint64]string)
	if fv, err := h.repo.Field.ValuesOf(p.ID); err == nil {
		for _, v := range fv {
			values[v.FieldID] = v.Value
		}
	}
	d.Values = values
	h.renderAdmin(w, r, "post_form", d)
}

// PostSave 保存文章（新建/更新）
func (h *Handler) PostSave(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, 64); err != nil {
		h.redirect(w, r, "/admin/posts", "error", "表单过大或解析失败")
		return
	}
	u := userOf(r)
	id, _ := utils.ParseUint(r.FormValue("id"))

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		h.redirect(w, r, h.postFormURL(id), "error", "标题不能为空")
		return
	}

	p := &model.Post{}
	if id > 0 {
		existing, err := h.repo.Post.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/posts", "error", "文章不存在")
			return
		}
		p = existing
	} else {
		p.UserID = u.ID
		p.ContentFormat = "html"
	}

	p.Title = title
	p.Summary = strings.TrimSpace(r.FormValue("summary"))
	p.Content = r.FormValue("content")
	p.ContentFormat = firstNonEmpty(r.FormValue("content_format"), "html")
	p.Cover = strings.TrimSpace(r.FormValue("cover"))
	p.Status = firstNonEmpty(r.FormValue("status"), "draft")
	p.Template = strings.TrimSpace(r.FormValue("template"))
	p.SEOTitle = strings.TrimSpace(r.FormValue("seo_title"))
	p.SEOKeywords = strings.TrimSpace(r.FormValue("seo_keywords"))
	p.SEODescription = strings.TrimSpace(r.FormValue("seo_description"))
	p.CategoryID = utils.FormUint(r, "category_id", 0)
	p.IsTop = boolToInt(r.FormValue("is_top") != "")
	p.IsRecommend = boolToInt(r.FormValue("is_recommend") != "")
	p.Sort = utils.FormInt(r, "sort", 0)

	// slug 唯一
	slugInput := strings.TrimSpace(r.FormValue("slug"))
	if slugInput == "" {
		slugInput = title
	}
	base := security.Slugify(slugInput)
	p.Slug = h.uniqueSlug(base, func(s string) bool {
		return h.repo.Post.ExistsSlug(s, p.ID)
	})

	// 发布时间
	if p.Status == "published" && p.PublishedAt == nil {
		now := time.Now()
		p.PublishedAt = &now
	}
	if v := strings.TrimSpace(r.FormValue("published_at")); v != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", v, h.cfg.Location); err == nil {
			p.PublishedAt = &t
		} else if t, err := time.ParseInLocation("2006-01-02 15:04", v, h.cfg.Location); err == nil {
			p.PublishedAt = &t
		}
	}

	var postID uint64
	if p.ID > 0 {
		if err := h.repo.Post.Update(p); err != nil {
			h.redirect(w, r, h.postFormURL(p.ID), "error", "保存失败："+err.Error())
			return
		}
		postID = p.ID
		h.audit(r, AuditPostUpdate, p.Title, "状态 "+p.Status+" / 栏目 "+strconv.FormatUint(p.CategoryID, 10))
	} else {
		newID, err := h.repo.Post.Create(p)
		if err != nil {
			h.redirect(w, r, "/admin/posts/new", "error", "保存失败："+err.Error())
			return
		}
		postID = newID
		h.audit(r, AuditPostCreate, p.Title, "状态 "+p.Status+" / 栏目 "+strconv.FormatUint(p.CategoryID, 10))
	}

	// 标签
	if tagStr := strings.TrimSpace(r.FormValue("tags")); tagStr != "" {
		names := splitTags(tagStr)
		_ = h.repo.Post.SyncTags(postID, names)
	} else {
		_ = h.repo.Post.SyncTags(postID, nil)
	}

	// 自定义字段
	if err := h.saveCustomFields(r, postID, p.CategoryID); err != nil {
		slog.Warn("保存自定义字段失败", "err", err)
	}

	if r.FormValue("continue") != "" {
		h.redirect(w, r, h.postFormURL(postID), "success", "已保存，可继续编辑")
		return
	}
	h.redirect(w, r, "/admin/posts", "success", "文章已保存")
}

func (h *Handler) saveCustomFields(r *http.Request, postID, categoryID uint64) error {
	if categoryID == 0 {
		return nil
	}
	fields, err := h.repo.Field.ListByCategory(categoryID)
	if err != nil {
		return err
	}
	values := make(map[uint64]string)
	for _, f := range fields {
		values[f.ID] = strings.TrimSpace(r.FormValue("field_" + strconv.FormatUint(f.ID, 10)))
	}
	return h.repo.Field.SaveValues(postID, values)
}

// postFormURL 文章编辑页地址。
//
// 注意：这里必须用 h.adminPath 而不是拼 "/admin/..."。
// 虽然 redirect 内部有 rebase 兜底，但一旦有人把返回值直接塞进模板，
// 就会变成写死的 /admin —— 后台路径改过之后链接就断了。
func (h *Handler) postFormURL(id uint64) string {
	if id == 0 {
		return h.adminPath("posts", "new")
	}
	return h.adminPath("posts", strconv.FormatUint(id, 10), "edit")
}

func splitTags(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if r == ',' || r == '，' || r == '\n' || r == '|' {
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, t)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PostDelete 删除文章
func (h *Handler) PostDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/posts", "error", "参数错误")
		return
	}
	// 先取标题再删，否则日志里只剩一个 ID，追溯时不知道删的是哪篇
	title := ""
	if p, err := h.repo.Post.ByID(id); err == nil {
		title = p.Title
	}
	if err := h.repo.Post.Delete(id); err != nil {
		h.redirect(w, r, "/admin/posts", "error", "删除失败："+err.Error())
		return
	}
	h.audit(r, AuditPostDelete, title, "文章 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/posts", "success", "文章已删除")
}

// PostBulk 批量操作
func (h *Handler) PostBulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/posts", "error", "表单解析失败")
		return
	}
	action := r.FormValue("action")
	ids := r.Form["ids"]
	n := 0
	for _, s := range ids {
		id, ok := utils.ParseUint(s)
		if !ok {
			continue
		}
		switch action {
		case "delete":
			if err := h.repo.Post.Delete(id); err == nil {
				n++
			}
		case "publish":
			if p, err := h.repo.Post.ByID(id); err == nil {
				p.Status = "published"
				if p.PublishedAt == nil {
					now := time.Now()
					p.PublishedAt = &now
				}
				if err := h.repo.Post.Update(p); err == nil {
					n++
				}
			}
		case "draft":
			if p, err := h.repo.Post.ByID(id); err == nil {
				p.Status = "draft"
				if err := h.repo.Post.Update(p); err == nil {
					n++
				}
			}
		}
	}
	h.audit(r, AuditPostBulk, action, "处理 "+strconv.Itoa(n)+" 条，选中 "+strconv.Itoa(len(ids))+" 条")
	h.redirect(w, r, "/admin/posts", "success", "已处理 "+strconv.Itoa(n)+" 条")
}
