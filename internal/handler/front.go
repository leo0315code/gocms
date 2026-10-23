package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/repository"
	"github.com/example/gocms/internal/utils"
)

// Home 首页
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.notFound(w, r)
		return
	}
	v := h.newView(r)
	v.SEO = SEO{
		Title:       v.Site["seo_title"],
		Keywords:    v.Site["seo_keywords"],
		Description: v.Site["seo_description"],
		Canonical:   v.SiteURL + "/",
		Image:       v.Logo,
		Type:        "website",
	}
	if v.SEO.Title == "" {
		v.SEO.Title = v.SiteName + " - " + v.Site["site_slogan"]
	}

	banners, _ := h.repo.Banner.List("home", true)
	v.Banner = banners

	// 推荐内容
	reco, _, _ := h.repo.Post.List(&repository.PostFilter{IsRecommend: true, Page: 1, PerPage: 6})
	v.Posts = reco

	// 最新
	latest, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 8})
	v.Latest = latest

	// 热门
	hot, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 5, OrderBy: "views"})
	v.HotPosts = hot

	tree, _ := h.repo.Cat.Tree()
	v.CatTree = h.repo.Cat.WithPostCount(tree)

	tags, _ := h.repo.Tag.List(20)
	v.Tags = tags

	links, _ := h.repo.Link.List(true)
	v.Links = links

	h.renderFront(w, r, "home", v)
}

// Category 栏目列表
func (h *Handler) Category(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	cat, err := h.repo.Cat.BySlug(slug)
	if err != nil {
		h.notFound(w, r)
		return
	}
	// 收集子分类 ID，实现父栏目聚合
	ids := []uint64{cat.ID}
	if all, err := h.repo.Cat.List(); err == nil {
		ids = append(ids, descendantIDs(all, cat.ID)...)
	}

	page := max(1, utils.QueryInt(r, "page", 1))
	list, total, err := h.repo.Post.List(&repository.PostFilter{
		CategoryIDs: ids,
		Page:        page,
		PerPage:     h.cfg.PageSize,
	})
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "加载内容失败", err)
		return
	}

	v := h.newView(r)
	v.Category = cat
	v.Posts = list
	v.Total = total
	v.Pagination = utils.NewPagination(page, h.cfg.PageSize, total, "/c/"+cat.Slug)
	v.SEO = SEO{
		Title:       firstNonEmpty(cat.SEOTitle, cat.Name+" - "+v.SiteName),
		Keywords:    firstNonEmpty(cat.SEOKeywords, v.Site["seo_keywords"]),
		Description: firstNonEmpty(cat.SEODescription, cat.Description),
		Canonical:   v.SiteURL + "/c/" + cat.Slug,
		Image:       absoluteURL(v.SiteURL, cat.Cover),
		Type:        "website",
	}
	v.Breadcrumb = []Crumb{{Name: "首页", URL: "/"}, {Name: cat.Name, URL: "/c/" + cat.Slug}}

	// 同级/子分类
	if all, err := h.repo.Cat.List(); err == nil {
		v.Categories = childrenOf(all, cat.ID)
	}
	tags, _ := h.repo.Tag.List(15)
	v.Tags = tags
	hot, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 5, OrderBy: "views"})
	v.HotPosts = hot

	// 支持自定义模板
	tpl := "category"
	if cat.Template != "" && h.rnd.HasTheme(cat.Template) {
		tpl = cat.Template
	}
	h.renderFront(w, r, tpl, v)
}

// Post 文章详情
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := h.repo.Post.BySlug(slug)
	if err != nil || p.Status != "published" {
		h.notFound(w, r)
		return
	}
	if p.PublishedAt != nil && p.PublishedAt.After(time.Now()) {
		h.notFound(w, r)
		return
	}

	h.repo.Post.UpdateViews(p.ID)

	v := h.newView(r)
	v.Post = p
	if p.Category != nil {
		v.Category = p.Category
		v.Breadcrumb = []Crumb{
			{Name: "首页", URL: "/"},
			{Name: p.Category.Name, URL: "/c/" + p.Category.Slug},
			{Name: p.Title, URL: "/a/" + p.Slug},
		}
	} else {
		v.Breadcrumb = []Crumb{{Name: "首页", URL: "/"}, {Name: p.Title, URL: "/a/" + p.Slug}}
	}

	v.Prev = h.repo.Post.Prev(p)
	v.Next = h.repo.Post.Next(p)
	v.Related = h.repo.Post.Related(p, 5)
	hot, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 5, OrderBy: "views"})
	v.HotPosts = hot
	tags, _ := h.repo.Tag.List(15)
	v.Tags = tags

	// 自定义字段
	if fv, err := h.repo.Field.ValuesOf(p.ID); err == nil {
		v.Fields = fv
	}

	v.SEO = SEO{
		Title:       firstNonEmpty(p.SEOTitle, p.Title+" - "+v.SiteName),
		Keywords:    firstNonEmpty(p.SEOKeywords, v.Site["seo_keywords"]),
		Description: firstNonEmpty(p.SEODescription, p.Summary, utils.Excerpt(p.Content, 120)),
		Canonical:   v.SiteURL + "/a/" + p.Slug,
		Image:       absoluteURL(v.SiteURL, p.Cover),
		Type:        "article",
	}
	if p.PublishedAt != nil {
		v.SEO.PublishedAt = *p.PublishedAt
	}

	tpl := "post"
	if p.Template != "" && h.rnd.HasTheme(p.Template) {
		tpl = p.Template
	}
	h.renderFront(w, r, tpl, v)
}

// Page 单页
func (h *Handler) Page(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	pg, err := h.repo.Page.BySlug(slug)
	if err != nil || pg.Status != "published" {
		h.notFound(w, r)
		return
	}
	v := h.newView(r)
	v.Page = pg
	v.Breadcrumb = []Crumb{{Name: "首页", URL: "/"}, {Name: pg.Title, URL: "/p/" + pg.Slug}}
	v.SEO = SEO{
		Title:       firstNonEmpty(pg.SEOTitle, pg.Title+" - "+v.SiteName),
		Keywords:    firstNonEmpty(pg.SEOKeywords, v.Site["seo_keywords"]),
		Description: firstNonEmpty(pg.SEODescription, utils.Excerpt(pg.Content, 120)),
		Canonical:   v.SiteURL + "/p/" + pg.Slug,
		Image:       absoluteURL(v.SiteURL, pg.Cover),
		Type:        "website",
	}

	tpl := "page"
	if pg.Template != "" && h.rnd.HasTheme(pg.Template) {
		tpl = pg.Template
	}
	h.renderFront(w, r, tpl, v)
}

// Tag 标签列表
func (h *Handler) Tag(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	tag, err := h.repo.Tag.BySlug(slug)
	if err != nil {
		h.notFound(w, r)
		return
	}
	page := max(1, utils.QueryInt(r, "page", 1))
	list, total, err := h.repo.Post.List(&repository.PostFilter{
		TagID:   tag.ID,
		Page:    page,
		PerPage: h.cfg.PageSize,
	})
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "加载内容失败", err)
		return
	}
	v := h.newView(r)
	v.Tag = tag
	v.Posts = list
	v.Total = total
	v.Pagination = utils.NewPagination(page, h.cfg.PageSize, total, "/t/"+tag.Slug)
	v.Breadcrumb = []Crumb{{Name: "首页", URL: "/"}, {Name: "#" + tag.Name, URL: "/t/" + tag.Slug}}
	v.SEO = SEO{
		Title:       tag.Name + " - " + v.SiteName,
		Description: "标签「" + tag.Name + "」下的全部内容",
		Canonical:   v.SiteURL + "/t/" + tag.Slug,
		Type:        "website",
	}
	h.renderFront(w, r, "tag", v)
}

// Search 搜索
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := utils.QueryStr(r, "q")
	page := max(1, utils.QueryInt(r, "page", 1))

	v := h.newView(r)
	v.Query = q
	v.SEO = SEO{
		Title:       "搜索：" + q + " - " + v.SiteName,
		Description: "站内搜索 " + q,
		NoIndex:     true,
	}
	if q != "" {
		list, total, err := h.repo.Post.Search(q, page, h.cfg.PageSize)
		if err != nil {
			h.renderError(w, r, http.StatusInternalServerError, "搜索失败", err)
			return
		}
		v.Posts = list
		v.Total = total
		v.Pagination = utils.NewPagination(page, h.cfg.PageSize, total, "/search?q="+urlQueryEscape(q))
	}
	tags, _ := h.repo.Tag.List(15)
	v.Tags = tags
	h.renderFront(w, r, "search", v)
}

// MessageSubmit 留言提交
func (h *Handler) MessageSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/", "error", "表单解析失败")
		return
	}
	ip := utils.ClientIP(r)
	if !h.limiter.Allow("msg:" + ip) {
		h.redirect(w, r, safeBack(r, "/"), "error", "提交过于频繁，请稍后再试")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	email := strings.TrimSpace(r.FormValue("email"))
	company := strings.TrimSpace(r.FormValue("company"))
	content := strings.TrimSpace(r.FormValue("content"))
	back := safeBack(r, "/")

	// 蜜罐字段：机器人会填
	if r.FormValue("website") != "" {
		h.redirect(w, r, back, "success", "提交成功，我们会尽快与您联系")
		return
	}

	var errs []string
	if name == "" || len([]rune(name)) > 50 {
		errs = append(errs, "请填写姓名（不超过 50 字）")
	}
	if phone == "" && email == "" {
		errs = append(errs, "请至少填写电话或邮箱")
	}
	if phone != "" && len(phone) > 40 {
		errs = append(errs, "电话格式不正确")
	}
	if email != "" && !isEmailLoose(email) {
		errs = append(errs, "邮箱格式不正确")
	}
	if content == "" {
		errs = append(errs, "请填写留言内容")
	}
	if len([]rune(content)) > 2000 {
		errs = append(errs, "留言内容请控制在 2000 字以内")
	}
	if len(errs) > 0 {
		h.redirect(w, r, back, "error", strings.Join(errs, "；"))
		return
	}

	msg := &model.Message{
		Name:      name,
		Phone:     phone,
		Email:     email,
		Company:   company,
		Content:   content,
		SourceURL: truncateStr(r.Referer(), 490),
		IP:        ip,
		UserAgent: truncateStr(r.UserAgent(), 250),
	}
	if _, err := h.repo.Msg.Create(msg); err != nil {
		slog.Error("保存留言失败", "err", err)
		h.redirect(w, r, back, "error", "提交失败，请稍后重试")
		return
	}
	h.redirect(w, r, back, "success", "提交成功，我们会尽快与您联系")
}

// Sitemap 站点地图
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	siteURL := strings.TrimRight(h.siteMap()["site_url"], "/")
	if siteURL == "" {
		siteURL = h.cfg.URL
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")

	writeURL := func(loc, lastmod, freq string, prio string) {
		b.WriteString("  <url>\n")
		b.WriteString("    <loc>" + xmlEscape(loc) + "</loc>\n")
		if lastmod != "" {
			b.WriteString("    <lastmod>" + lastmod + "</lastmod>\n")
		}
		if freq != "" {
			b.WriteString("    <changefreq>" + freq + "</changefreq>\n")
		}
		if prio != "" {
			b.WriteString("    <priority>" + prio + "</priority>\n")
		}
		b.WriteString("  </url>\n")
	}

	now := time.Now().Format("2006-01-02")
	writeURL(siteURL+"/", now, "daily", "1.0")

	if cats, err := h.repo.Cat.List(); err == nil {
		for _, c := range cats {
			writeURL(siteURL+"/c/"+c.Slug, c.UpdatedAt.Format("2006-01-02"), "weekly", "0.8")
		}
	}
	if pages, err := h.repo.Page.List(); err == nil {
		for _, p := range pages {
			if p.Status != "published" {
				continue
			}
			writeURL(siteURL+"/p/"+p.Slug, p.UpdatedAt.Format("2006-01-02"), "monthly", "0.6")
		}
	}
	posts, _, err := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 2000})
	if err == nil {
		for _, p := range posts {
			lm := p.UpdatedAt.Format("2006-01-02")
			writeURL(siteURL+"/a/"+p.Slug, lm, "monthly", "0.7")
		}
	}
	b.WriteString("</urlset>\n")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(b.String()))
}

// Robots robots.txt
//
// 这里有个反直觉的点：robots.txt 是公开文件，把后台路径写进 Disallow
// 等于向所有扫描器广播「后台在这里」。所以策略是：
//   - 静默模式（目标是隐藏后台）：完全不写后台路径，靠「不链接 + 未登录 404/跳登录」防收录
//   - 非静默模式：只屏蔽当前路径，帮搜索引擎跳过；不列历史路径（它们只会 302 过来，多此一举）
func (h *Handler) Robots(w http.ResponseWriter, r *http.Request) {
	siteURL := strings.TrimRight(h.siteMap()["site_url"], "/")
	if siteURL == "" {
		siteURL = h.cfg.URL
	}
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	if !h.ap.Silent() {
		b.WriteString("Disallow: " + h.ap.URL() + "/\n")
	}
	b.WriteString("Disallow: /search\n")
	b.WriteString("\nSitemap: " + siteURL + "/sitemap.xml\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// RSS RSS 2.0
func (h *Handler) RSS(w http.ResponseWriter, r *http.Request) {
	site := h.siteMap()
	siteURL := strings.TrimRight(site["site_url"], "/")
	if siteURL == "" {
		siteURL = h.cfg.URL
	}
	name := site["site_name"]
	if name == "" {
		name = h.cfg.Name
	}

	posts, _, _ := h.repo.Post.List(&repository.PostFilter{Page: 1, PerPage: 30})

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">` + "\n<channel>\n")
	b.WriteString("  <title>" + xmlEscape(name) + "</title>\n")
	b.WriteString("  <link>" + xmlEscape(siteURL) + "</link>\n")
	b.WriteString("  <description>" + xmlEscape(firstNonEmpty(site["seo_description"], site["site_slogan"], name)) + "</description>\n")
	b.WriteString("  <language>zh-CN</language>\n")
	b.WriteString("  <lastBuildDate>" + time.Now().Format(time.RFC1123Z) + "</lastBuildDate>\n")
	b.WriteString(`  <atom:link href="` + xmlEscape(siteURL+"/rss.xml") + `" rel="self" type="application/rss+xml"/>` + "\n")

	for _, p := range posts {
		link := siteURL + "/a/" + p.Slug
		pub := p.CreatedAt
		if p.PublishedAt != nil {
			pub = *p.PublishedAt
		}
		b.WriteString("  <item>\n")
		b.WriteString("    <title>" + xmlEscape(p.Title) + "</title>\n")
		b.WriteString("    <link>" + xmlEscape(link) + "</link>\n")
		b.WriteString(`    <guid isPermaLink="true">` + xmlEscape(link) + "</guid>\n")
		b.WriteString("    <pubDate>" + pub.Format(time.RFC1123Z) + "</pubDate>\n")
		b.WriteString("    <description>" + xmlEscape(firstNonEmpty(p.Summary, utils.Excerpt(p.Content, 200))) + "</description>\n")
		b.WriteString("  </item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// ---------- 辅助 ----------

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func absoluteURL(base, p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(p, "/")
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "+", "%2B"), "&", "%26")
}

func isEmailLoose(s string) bool {
	at := strings.Index(s, "@")
	return at > 0 && strings.Contains(s[at:], ".")
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// safeBack 安全返回地址：只允许站内路径
func safeBack(r *http.Request, fallback string) string {
	ref := r.FormValue("back")
	if ref != "" && strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "//") {
		return ref
	}
	// 从 Referer 提取路径
	ref2 := r.Referer()
	if i := strings.Index(ref2, "://"); i > 0 {
		rest := ref2[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			p := rest[j:]
			if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
				return p
			}
		}
	}
	return fallback
}

// descendantIDs 递归取子孙分类 ID
func descendantIDs(all []*model.Category, parentID uint64) []uint64 {
	var out []uint64
	for _, c := range all {
		if c.ParentID == parentID {
			out = append(out, c.ID)
			out = append(out, descendantIDs(all, c.ID)...)
		}
	}
	return out
}

func childrenOf(all []*model.Category, parentID uint64) []*model.Category {
	var out []*model.Category
	for _, c := range all {
		if c.ParentID == parentID {
			out = append(out, c)
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = fmt.Sprint
