package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/leo0315code/gocms/internal/model"
	"github.com/leo0315code/gocms/internal/repository"
	"github.com/leo0315code/gocms/internal/security"
	"github.com/leo0315code/gocms/internal/utils"
)

// ---------- 分类 ----------

// CategoryList 分类列表
func (h *Handler) CategoryList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "栏目管理", "categories")
	tree, err := h.repo.Cat.Tree()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载分类失败")
		return
	}
	flat := flattenCats(tree, 0)
	d.Items = flat
	d.Total = len(flat)
	d.Fields, _ = h.repo.Field.ListAll()
	h.renderAdmin(w, r, "category_list", d)
}

// flatCat 带层级信息
type flatCat struct {
	*model.Category
	Depth int
	Path  string
}

func flattenCats(list []*model.Category, depth int) []*flatCat {
	var out []*flatCat
	for _, c := range list {
		out = append(out, &flatCat{Category: c, Depth: depth})
		out = append(out, flattenCats(c.Children, depth+1)...)
	}
	return out
}

// CategoryNew 新建分类页
func (h *Handler) CategoryNew(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "新建栏目", "categories")
	d.Item = &model.Category{Type: "post", IsNav: 1}
	tree, _ := h.repo.Cat.Tree()
	d.CatTree = tree
	h.renderAdmin(w, r, "category_form", d)
}

// CategoryEdit 编辑分类页
func (h *Handler) CategoryEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/categories", "error", "参数错误")
		return
	}
	c, err := h.repo.Cat.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/categories", "error", "栏目不存在")
		return
	}
	d := h.adminView(r, "编辑栏目", "categories")
	d.Item = c
	tree, _ := h.repo.Cat.Tree()
	d.CatTree = tree
	if fields, err := h.repo.Field.ListByCategory(id); err == nil {
		d.Fields = fields
	}
	h.renderAdmin(w, r, "category_form", d)
}

// CategorySave 保存分类
func (h *Handler) CategorySave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/categories", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.redirect(w, r, "/admin/categories", "error", "栏目名称不能为空")
		return
	}

	c := &model.Category{}
	if id > 0 {
		existing, err := h.repo.Cat.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/categories", "error", "栏目不存在")
			return
		}
		c = existing
	}
	c.Name = name
	c.ParentID = utils.FormUint(r, "parent_id", 0)
	if c.ParentID == c.ID {
		c.ParentID = 0
	}
	c.Description = strings.TrimSpace(r.FormValue("description"))
	c.Cover = strings.TrimSpace(r.FormValue("cover"))
	c.Icon = strings.TrimSpace(r.FormValue("icon"))
	c.Type = firstNonEmpty(r.FormValue("type"), "post")
	c.Template = strings.TrimSpace(r.FormValue("template"))
	c.Sort = utils.FormInt(r, "sort", 0)
	c.IsNav = boolToInt(r.FormValue("is_nav") != "")
	c.SEOTitle = strings.TrimSpace(r.FormValue("seo_title"))
	c.SEOKeywords = strings.TrimSpace(r.FormValue("seo_keywords"))
	c.SEODescription = strings.TrimSpace(r.FormValue("seo_description"))

	slugInput := strings.TrimSpace(r.FormValue("slug"))
	if slugInput == "" {
		slugInput = name
	}
	c.Slug = h.uniqueSlug(slugInput, func(s string) bool {
		return h.repo.Cat.ExistsSlug(s, c.ID)
	})

	if c.ID > 0 {
		if err := h.repo.Cat.Update(c); err != nil {
			h.redirect(w, r, "/admin/categories", "error", "保存失败："+err.Error())
			return
		}
	} else {
		if _, err := h.repo.Cat.Create(c); err != nil {
			h.redirect(w, r, "/admin/categories", "error", "保存失败："+err.Error())
			return
		}
	}
	h.audit(r, AuditCategorySave, c.Name, "栏目 ID "+strconv.FormatUint(c.ID, 10))
	h.redirect(w, r, "/admin/categories", "success", "栏目已保存")
}

// CategoryDelete 删除分类
func (h *Handler) CategoryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/categories", "error", "参数错误")
		return
	}
	catName := ""
	if old, err := h.repo.Cat.ByID(id); err == nil {
		catName = old.Name
	}
	if err := h.repo.Cat.Delete(id); err != nil {
		h.redirect(w, r, "/admin/categories", "error", "删除失败："+err.Error())
		return
	}
	h.audit(r, AuditCategoryDelete, catName, "栏目 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/categories", "success", "栏目已删除，其下文章已移至未分类")
}

// ---------- 自定义字段 ----------

// FieldSave 新增/更新字段
func (h *Handler) FieldSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/categories", "error", "表单解析失败")
		return
	}
	catID := utils.FormUint(r, "category_id", 0)
	if catID == 0 {
		h.redirect(w, r, "/admin/categories", "error", "缺少分类 ID")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("field_id"))
	f := &model.Field{}
	if id > 0 {
		existing, err := h.repo.Field.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/categories/"+strconv.FormatUint(catID, 10)+"/edit", "error", "字段不存在")
			return
		}
		f = existing
	}
	f.CategoryID = catID
	f.Name = strings.TrimSpace(r.FormValue("field_name"))
	f.Label = strings.TrimSpace(r.FormValue("field_label"))
	f.Type = firstNonEmpty(r.FormValue("field_type"), "text")
	f.Options = strings.TrimSpace(r.FormValue("field_options"))
	f.Placeholder = strings.TrimSpace(r.FormValue("field_placeholder"))
	f.Required = boolToInt(r.FormValue("field_required") != "")
	f.Sort = utils.FormInt(r, "field_sort", 0)

	if f.Name == "" || f.Label == "" {
		h.redirect(w, r, "/admin/categories/"+strconv.FormatUint(catID, 10)+"/edit", "error", "字段名与标签不能为空")
		return
	}

	if f.ID > 0 {
		_ = h.repo.Field.Update(f)
	} else {
		_, _ = h.repo.Field.Create(f)
	}
	h.audit(r, AuditFieldSave, f.Label, "字段 "+f.Name+" / 栏目 ID "+strconv.FormatUint(catID, 10))
	h.redirect(w, r, "/admin/categories/"+strconv.FormatUint(catID, 10)+"/edit", "success", "字段已保存")
}

// FieldDelete 删除字段
func (h *Handler) FieldDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/categories", "error", "参数错误")
		return
	}
	catID := uint64(utils.QueryInt(r, "cat", 0))
	_ = h.repo.Field.Delete(id)
	h.audit(r, AuditFieldDelete, "字段 ID "+strconv.FormatUint(id, 10), "栏目 ID "+strconv.FormatUint(catID, 10))
	h.redirect(w, r, "/admin/categories/"+strconv.FormatUint(catID, 10)+"/edit", "success", "字段已删除")
}

// ---------- 单页 ----------

// PageList 单页列表
func (h *Handler) PageList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "单页管理", "pages")
	list, err := h.repo.Page.List()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d.Items = list
	d.Total = len(list)
	h.renderAdmin(w, r, "page_list", d)
}

// PageNew 新建单页
func (h *Handler) PageNew(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "新建单页", "pages")
	d.Item = &model.Page{Status: "published", ContentFormat: "html", IsNav: 1}
	h.renderAdmin(w, r, "page_form", d)
}

// PageEdit 编辑单页
func (h *Handler) PageEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/pages", "error", "参数错误")
		return
	}
	p, err := h.repo.Page.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/pages", "error", "页面不存在")
		return
	}
	d := h.adminView(r, "编辑单页", "pages")
	d.Item = p
	h.renderAdmin(w, r, "page_form", d)
}

// PageSave 保存单页
func (h *Handler) PageSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/pages", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		h.redirect(w, r, "/admin/pages", "error", "标题不能为空")
		return
	}
	p := &model.Page{}
	if id > 0 {
		existing, err := h.repo.Page.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/pages", "error", "页面不存在")
			return
		}
		p = existing
	}
	p.Title = title
	p.Content = r.FormValue("content")
	p.ContentFormat = firstNonEmpty(r.FormValue("content_format"), "html")
	p.Cover = strings.TrimSpace(r.FormValue("cover"))
	p.Template = strings.TrimSpace(r.FormValue("template"))
	p.Status = firstNonEmpty(r.FormValue("status"), "published")
	p.Sort = utils.FormInt(r, "sort", 0)
	p.IsNav = boolToInt(r.FormValue("is_nav") != "")
	p.SEOTitle = strings.TrimSpace(r.FormValue("seo_title"))
	p.SEOKeywords = strings.TrimSpace(r.FormValue("seo_keywords"))
	p.SEODescription = strings.TrimSpace(r.FormValue("seo_description"))

	slugInput := strings.TrimSpace(r.FormValue("slug"))
	if slugInput == "" {
		slugInput = title
	}
	p.Slug = h.uniqueSlug(slugInput, func(s string) bool {
		return h.repo.Page.ExistsSlug(s, p.ID)
	})

	if p.ID > 0 {
		if err := h.repo.Page.Update(p); err != nil {
			h.redirect(w, r, "/admin/pages", "error", "保存失败："+err.Error())
			return
		}
	} else {
		if _, err := h.repo.Page.Create(p); err != nil {
			h.redirect(w, r, "/admin/pages", "error", "保存失败："+err.Error())
			return
		}
	}
	h.audit(r, AuditPageSave, p.Title, "单页 ID "+strconv.FormatUint(p.ID, 10))
	h.redirect(w, r, "/admin/pages", "success", "单页已保存")
}

// PageDelete 删除单页
func (h *Handler) PageDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/pages", "error", "参数错误")
		return
	}
	pageTitle := ""
	if old, err := h.repo.Page.ByID(id); err == nil {
		pageTitle = old.Title
	}
	if err := h.repo.Page.Delete(id); err != nil {
		h.redirect(w, r, "/admin/pages", "error", "删除失败："+err.Error())
		return
	}
	h.audit(r, AuditPageDelete, pageTitle, "单页 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/pages", "success", "单页已删除")
}

// ---------- 标签 ----------

// TagList 标签管理
func (h *Handler) TagList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "标签管理", "tags")
	list, err := h.repo.Tag.List(0)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d.Items = list
	d.Total = len(list)
	h.renderAdmin(w, r, "tag_list", d)
}

// TagSave 保存标签
func (h *Handler) TagSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/tags", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.redirect(w, r, "/admin/tags", "error", "标签名不能为空")
		return
	}
	t := &model.Tag{Name: name}
	if id > 0 {
		existing, err := h.repo.Tag.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/tags", "error", "标签不存在")
			return
		}
		t = existing
		t.Name = name
	}
	slugInput := strings.TrimSpace(r.FormValue("slug"))
	if slugInput == "" {
		slugInput = name
	}
	t.Slug = security.Slugify(slugInput)

	if t.ID > 0 {
		_ = h.repo.Tag.Update(t)
	} else {
		_, _ = h.repo.Tag.Create(t)
	}
	h.audit(r, AuditTagSave, t.Name, "标签 ID "+strconv.FormatUint(t.ID, 10))
	h.redirect(w, r, "/admin/tags", "success", "标签已保存")
}

// TagDelete 删除标签
func (h *Handler) TagDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/tags", "error", "参数错误")
		return
	}
	tagName := ""
	if old, err := h.repo.Tag.ByID(id); err == nil {
		tagName = old.Name
	}
	_ = h.repo.Tag.Delete(id)
	h.audit(r, AuditTagDelete, tagName, "标签 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/tags", "success", "标签已删除")
}

// ---------- 媒体 ----------

// MediaList 媒体库
func (h *Handler) MediaList(w http.ResponseWriter, r *http.Request) {
	page := max(1, utils.QueryInt(r, "page", 1))
	typ := utils.QueryStr(r, "type")
	list, total, err := h.repo.Media.List(typ, page, 24)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d := h.adminView(r, "媒体库", "media")
	d.Items = list
	d.Total = total
	d.Status = typ
	d.Pagination = utils.NewPagination(page, 24, total, "/admin/media?type="+typ)
	h.renderAdmin(w, r, "media_list", d)
}

// MediaUpload 上传
func (h *Handler) MediaUpload(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, h.cfg.UploadMaxMB+2); err != nil {
		h.redirect(w, r, "/admin/media", "error", "上传失败：表单解析错误")
		return
	}
	u := userOf(r)
	// 只有 multipart 请求 ParseMultipartForm 才会填充 MultipartForm；
	// urlencoded / 空 body 的请求走不到那个分支，直接取 .File 会 nil panic。
	if r.MultipartForm == nil {
		h.redirect(w, r, "/admin/media", "error", "请选择文件")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		h.redirect(w, r, "/admin/media", "error", "请选择文件")
		return
	}

	okCount := 0
	var lastErr string
	for _, fh := range files {
		res, err := h.saveUpload(fh, "")
		if err != nil {
			lastErr = err.Error()
			continue
		}
		m := &model.Media{
			UserID: u.ID,
			Name:   res.Name,
			Path:   res.Path,
			URL:    res.URL,
			Mime:   res.Mime,
			Ext:    res.Ext,
			Size:   res.Size,
			Width:  res.Width,
			Height: res.Height,
		}
		if _, err := h.repo.Media.Create(m); err != nil {
			lastErr = err.Error()
			continue
		}
		okCount++
	}
	if okCount == 0 {
		h.redirect(w, r, "/admin/media", "error", "上传失败："+lastErr)
		return
	}
	h.audit(r, AuditMediaUpload, strconv.Itoa(okCount)+" 个文件", "")
	h.redirect(w, r, "/admin/media", "success", "已上传 "+strconv.Itoa(okCount)+" 个文件")
}

// MediaDelete 删除媒体
func (h *Handler) MediaDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/media", "error", "参数错误")
		return
	}
	m, err := h.repo.Media.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/media", "error", "文件不存在")
		return
	}
	h.deleteUploaded(m.URL)
	_ = h.repo.Media.Delete(id)
	h.audit(r, AuditMediaDelete, m.Name, m.URL)
	h.redirect(w, r, "/admin/media", "success", "文件已删除")
}

// ---------- 菜单 ----------

// MenuList 菜单管理
func (h *Handler) MenuList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "导航菜单", "menus")
	menus, err := h.repo.Menu.List()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	if len(menus) == 0 {
		// 自动创建默认菜单
		_, _ = h.repo.Menu.CreateMenu(&model.Menu{Name: "主导航", Key: "main"})
		_, _ = h.repo.Menu.CreateMenu(&model.Menu{Name: "页脚导航", Key: "footer"})
		menus, _ = h.repo.Menu.List()
	}
	d.Items = menus
	d.Options = map[string]any{"selected": utils.QueryInt(r, "menu", 0)}
	h.renderAdmin(w, r, "menu_list", d)
}

// MenuDetail 某菜单的菜单项管理
func (h *Handler) MenuDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/menus", "error", "参数错误")
		return
	}
	menu, err := h.repo.Menu.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/menus", "error", "菜单不存在")
		return
	}
	d := h.adminView(r, "菜单项："+menu.Name, "menus")
	d.Item = menu
	flat, _ := h.repo.Menu.FlatItems(id)
	d.Items = flat
	d.Items2 = flattenMenu(menu.Items, 0)
	cats, _ := h.repo.Cat.List()
	d.Categories = cats
	pages, _ := h.repo.Page.List()
	d.Options = map[string]any{"pages": pages}
	h.renderAdmin(w, r, "menu_detail", d)
}

type flatMenu struct {
	*model.MenuItem
	Depth int
}

func flattenMenu(list []*model.MenuItem, depth int) []*flatMenu {
	var out []*flatMenu
	for _, it := range list {
		out = append(out, &flatMenu{MenuItem: it, Depth: depth})
		out = append(out, flattenMenu(it.Children, depth+1)...)
	}
	return out
}

// MenuSave 新建菜单
func (h *Handler) MenuSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/menus", "error", "表单解析失败")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	key := security.SlugifyASCII(r.FormValue("key"))
	if name == "" || key == "" {
		h.redirect(w, r, "/admin/menus", "error", "菜单名与标识不能为空")
		return
	}
	if _, err := h.repo.Menu.CreateMenu(&model.Menu{Name: name, Key: key}); err != nil {
		h.redirect(w, r, "/admin/menus", "error", "创建失败："+err.Error())
		return
	}
	h.audit(r, AuditMenuSave, name, "菜单标识 "+key)
	h.redirect(w, r, "/admin/menus", "success", "菜单已创建")
}

// MenuDelete 删除菜单
func (h *Handler) MenuDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/menus", "error", "参数错误")
		return
	}
	menuName := ""
	if old, err := h.repo.Menu.ByID(id); err == nil {
		menuName = old.Name
	}
	_ = h.repo.Menu.DeleteMenu(id)
	h.audit(r, AuditMenuDelete, menuName, "菜单 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/menus", "success", "菜单已删除")
}

// MenuItemSave 保存菜单项
func (h *Handler) MenuItemSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/menus", "error", "表单解析失败")
		return
	}
	menuID := utils.FormUint(r, "menu_id", 0)
	if menuID == 0 {
		h.redirect(w, r, "/admin/menus", "error", "缺少菜单 ID")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("item_id"))
	it := &model.MenuItem{MenuID: menuID}
	if id > 0 {
		existing, err := h.repo.Menu.ItemByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/menus/"+utils.UintStr(menuID), "error", "菜单项不存在")
			return
		}
		it = existing
	}
	it.Title = strings.TrimSpace(r.FormValue("title"))
	it.ParentID = utils.FormUint(r, "parent_id", 0)
	if it.ParentID == it.ID {
		it.ParentID = 0
	}
	it.LinkType = firstNonEmpty(r.FormValue("link_type"), "custom")
	it.LinkID = utils.FormUint(r, "link_id", 0)
	it.URL = strings.TrimSpace(r.FormValue("url"))
	it.Target = firstNonEmpty(r.FormValue("target"), "_self")
	it.Icon = strings.TrimSpace(r.FormValue("icon"))
	it.Sort = utils.FormInt(r, "sort", 0)
	it.IsShow = boolToInt(r.FormValue("is_show") != "")

	if it.Title == "" {
		h.redirect(w, r, "/admin/menus/"+utils.UintStr(menuID), "error", "菜单标题不能为空")
		return
	}

	if it.ID > 0 {
		_ = h.repo.Menu.UpdateItem(it)
	} else {
		_, _ = h.repo.Menu.CreateItem(it)
	}
	h.audit(r, AuditMenuSave, it.Title, "菜单项 ID "+strconv.FormatUint(it.ID, 10))
	h.redirect(w, r, "/admin/menus/"+utils.UintStr(menuID), "success", "菜单项已保存")
}

// MenuItemDelete 删除菜单项
func (h *Handler) MenuItemDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/menus", "error", "参数错误")
		return
	}
	menuID := uint64(utils.QueryInt(r, "menu", 0))
	itemTitle := ""
	if old, err := h.repo.Menu.ItemByID(id); err == nil {
		itemTitle = old.Title
	}
	_ = h.repo.Menu.DeleteItem(id)
	h.audit(r, AuditMenuDelete, itemTitle, "菜单项 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/menus/"+utils.UintStr(menuID), "success", "菜单项已删除")
}

// ---------- 轮播 ----------

// BannerList 轮播管理
func (h *Handler) BannerList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "轮播图", "banners")
	list, err := h.repo.Banner.List("", false)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d.Items = list
	if id := utils.QueryInt(r, "edit", 0); id > 0 {
		if b, err := h.repo.Banner.ByID(uint64(id)); err == nil {
			d.Item = b
		}
	}
	h.renderAdmin(w, r, "banner_list", d)
}

// BannerSave 保存轮播
func (h *Handler) BannerSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/banners", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	b := &model.Banner{Position: "home", IsShow: 1}
	if id > 0 {
		existing, err := h.repo.Banner.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/banners", "error", "记录不存在")
			return
		}
		b = existing
	}
	b.Title = strings.TrimSpace(r.FormValue("title"))
	b.Subtitle = strings.TrimSpace(r.FormValue("subtitle"))
	b.Image = strings.TrimSpace(r.FormValue("image"))
	b.URL = strings.TrimSpace(r.FormValue("url"))
	b.ButtonText = strings.TrimSpace(r.FormValue("button_text"))
	b.Position = firstNonEmpty(r.FormValue("position"), "home")
	b.Sort = utils.FormInt(r, "sort", 0)
	b.IsShow = boolToInt(r.FormValue("is_show") != "")

	if b.ID > 0 {
		_ = h.repo.Banner.Update(b)
	} else {
		_, _ = h.repo.Banner.Create(b)
	}
	h.audit(r, AuditBannerSave, b.Title, "轮播 ID "+strconv.FormatUint(b.ID, 10))
	h.redirect(w, r, "/admin/banners", "success", "轮播已保存")
}

// BannerDelete 删除轮播
func (h *Handler) BannerDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/banners", "error", "参数错误")
		return
	}
	bannerTitle := ""
	if old, err := h.repo.Banner.ByID(id); err == nil {
		bannerTitle = old.Title
	}
	_ = h.repo.Banner.Delete(id)
	h.audit(r, AuditBannerDelete, bannerTitle, "轮播 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/banners", "success", "轮播已删除")
}

// ---------- 友链 ----------

// LinkList 友链管理
func (h *Handler) LinkList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "友情链接", "links")
	list, err := h.repo.Link.List(false)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d.Items = list
	if id := utils.QueryInt(r, "edit", 0); id > 0 {
		if l, err := h.repo.Link.ByID(uint64(id)); err == nil {
			d.Item = l
		}
	}
	h.renderAdmin(w, r, "link_list", d)
}

// LinkSave 保存友链
func (h *Handler) LinkSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/links", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	l := &model.Link{IsShow: 1}
	if id > 0 {
		existing, err := h.repo.Link.ByID(id)
		if err != nil {
			h.redirect(w, r, "/admin/links", "error", "记录不存在")
			return
		}
		l = existing
	}
	l.Name = strings.TrimSpace(r.FormValue("name"))
	l.URL = strings.TrimSpace(r.FormValue("url"))
	l.Logo = strings.TrimSpace(r.FormValue("logo"))
	l.Description = strings.TrimSpace(r.FormValue("description"))
	l.Sort = utils.FormInt(r, "sort", 0)
	l.IsShow = boolToInt(r.FormValue("is_show") != "")

	if l.Name == "" || l.URL == "" {
		h.redirect(w, r, "/admin/links", "error", "名称与链接不能为空")
		return
	}
	if !security.IsHTTPURL(l.URL) {
		h.redirect(w, r, "/admin/links", "error", "链接需以 http:// 或 https:// 开头")
		return
	}

	if l.ID > 0 {
		_ = h.repo.Link.Update(l)
	} else {
		_, _ = h.repo.Link.Create(l)
	}
	h.audit(r, AuditLinkSave, l.Name, "友链 ID "+strconv.FormatUint(l.ID, 10))
	h.redirect(w, r, "/admin/links", "success", "友链已保存")
}

// LinkDelete 删除友链
func (h *Handler) LinkDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/links", "error", "参数错误")
		return
	}
	linkName := ""
	if old, err := h.repo.Link.ByID(id); err == nil {
		linkName = old.Name
	}
	_ = h.repo.Link.Delete(id)
	h.audit(r, AuditLinkDelete, linkName, "友链 ID "+strconv.FormatUint(id, 10))
	h.redirect(w, r, "/admin/links", "success", "友链已删除")
}

var _ = repository.ErrNotFound
