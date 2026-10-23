package handler

import (
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/example/gocms/internal/adminpath"
	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/security"
	"github.com/example/gocms/internal/utils"
)

// ---------- 留言 ----------

// MessageList 留言管理
func (h *Handler) MessageList(w http.ResponseWriter, r *http.Request) {
	page := max(1, utils.QueryInt(r, "page", 1))
	status := utils.QueryStr(r, "status")
	list, total, err := h.repo.Msg.List(status, page, 20)
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d := h.adminView(r, "留言管理", "messages")
	d.Items = list
	d.Total = total
	d.Status = status
	d.Pagination = utils.NewPagination(page, 20, total, "/admin/messages?status="+status)
	if id := utils.QueryInt(r, "view", 0); id > 0 {
		if m, err := h.repo.Msg.ByID(uint64(id)); err == nil {
			d.Item = m
			if m.Status == "unread" {
				_ = h.repo.Msg.MarkStatus(m.ID, "read")
				m.Status = "read"
			}
		}
	}
	h.renderAdmin(w, r, "message_list", d)
}

// MessageReply 回复留言
func (h *Handler) MessageReply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/messages", "error", "表单解析失败")
		return
	}
	id := utils.FormUint(r, "id", 0)
	reply := strings.TrimSpace(r.FormValue("reply"))
	if id == 0 {
		h.redirect(w, r, "/admin/messages", "error", "参数错误")
		return
	}
	if reply == "" {
		h.redirect(w, r, "/admin/messages?view="+utils.UintStr(id), "error", "回复内容不能为空")
		return
	}
	if err := h.repo.Msg.Reply(id, reply); err != nil {
		h.redirect(w, r, "/admin/messages", "error", "回复失败："+err.Error())
		return
	}
	h.audit(r, AuditMessageReply, "留言 ID "+utils.UintStr(id), truncateRunes(reply, 60))
	h.redirect(w, r, "/admin/messages?status=replied", "success", "已回复")
}

// MessageStatus 改状态
func (h *Handler) MessageStatus(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/messages", "error", "表单解析失败")
		return
	}
	id := utils.FormUint(r, "id", 0)
	status := r.FormValue("status")
	if status != "unread" && status != "read" && status != "replied" {
		status = "read"
	}
	if id > 0 {
		_ = h.repo.Msg.MarkStatus(id, status)
	}
	h.redirect(w, r, "/admin/messages", "success", "状态已更新")
}

// MessageDelete 删除留言
func (h *Handler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/messages", "error", "参数错误")
		return
	}
	msgName := ""
	if old, err := h.repo.Msg.ByID(id); err == nil {
		msgName = old.Name
	}
	_ = h.repo.Msg.Delete(id)
	h.audit(r, AuditMessageDelete, msgName, "留言 ID "+utils.UintStr(id))
	h.redirect(w, r, "/admin/messages", "success", "留言已删除")
}

// ---------- 用户 ----------

// UserList 用户管理
func (h *Handler) UserList(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "用户管理", "users")
	list, err := h.repo.User.List()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	d.Items = list
	if id := utils.QueryInt(r, "edit", 0); id > 0 {
		if u, err := h.repo.User.ByID(uint64(id)); err == nil {
			d.Item = u
		}
	}
	h.renderAdmin(w, r, "user_list", d)
}

// UserSave 新建/更新用户
func (h *Handler) UserSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/users", "error", "表单解析失败")
		return
	}
	id, _ := utils.ParseUint(r.FormValue("id"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	email := strings.TrimSpace(r.FormValue("email"))
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	role := firstNonEmpty(r.FormValue("role"), "editor")

	if role != "admin" && role != "editor" && role != "viewer" {
		role = "editor"
	}

	if id == 0 {
		if username == "" {
			h.redirect(w, r, "/admin/users", "error", "用户名不能为空")
			return
		}
		if h.repo.User.ExistsUsername(username, 0) {
			h.redirect(w, r, "/admin/users", "error", "用户名已存在")
			return
		}
		if err := security.ValidatePasswordStrength(password); err != nil {
			h.redirect(w, r, "/admin/users", "error", err.Error())
			return
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			h.redirect(w, r, "/admin/users", "error", "密码处理失败")
			return
		}
		u := &model.User{
			Username: username,
			Password: hash,
			Email:    email,
			Nickname: nickname,
			Role:     role,
			Status:   1,
		}
		if _, err := h.repo.User.Create(u); err != nil {
			h.redirect(w, r, "/admin/users", "error", "创建失败："+err.Error())
			return
		}
		h.audit(r, AuditUserCreate, username, "角色 "+u.Role)
		h.redirect(w, r, "/admin/users", "success", "用户已创建")
		return
	}

	u, err := h.repo.User.ByID(id)
	if err != nil {
		h.redirect(w, r, "/admin/users", "error", "用户不存在")
		return
	}
	u.Email = email
	u.Nickname = nickname
	u.Status = boolToInt(r.FormValue("status") != "")
	// 不允许把自己降级，防锁死
	if me := userOf(r); me == nil || me.ID != u.ID {
		u.Role = role
	}
	if password != "" {
		if err := security.ValidatePasswordStrength(password); err != nil {
			h.redirect(w, r, "/admin/users", "error", err.Error())
			return
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			h.redirect(w, r, "/admin/users", "error", "密码处理失败")
			return
		}
		if err := h.repo.User.UpdatePassword(u.ID, hash); err != nil {
			h.redirect(w, r, "/admin/users", "error", "更新密码失败")
			return
		}
	}
	if err := h.repo.User.Update(u); err != nil {
		h.redirect(w, r, "/admin/users", "error", "保存失败："+err.Error())
		return
	}
	h.audit(r, AuditUserUpdate, username, "角色 "+u.Role)
	h.redirect(w, r, "/admin/users", "success", "用户已更新")
}

// UserDelete 删除用户
func (h *Handler) UserDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := utils.ParseUint(r.PathValue("id"))
	if !ok {
		h.redirect(w, r, "/admin/users", "error", "参数错误")
		return
	}
	if me := userOf(r); me != nil && me.ID == id {
		h.redirect(w, r, "/admin/users", "error", "不能删除当前登录账号")
		return
	}
	if h.repo.User.Count() <= 1 {
		h.redirect(w, r, "/admin/users", "error", "至少保留一个管理员账号")
		return
	}
	_ = h.repo.Sess.DeleteByUser(id)
	_ = h.repo.User.Delete(id)
	h.audit(r, AuditUserDelete, "用户 ID "+utils.UintStr(id), "")
	h.redirect(w, r, "/admin/users", "success", "用户已删除")
}

// Profile 个人资料
func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "个人资料", "profile")
	d.Item = userOf(r)
	h.renderAdmin(w, r, "profile", d)
}

// ProfileSave 保存个人资料
func (h *Handler) ProfileSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/profile", "error", "表单解析失败")
		return
	}
	u := userOf(r)
	if u == nil {
		h.redirect(w, r, "/admin/login", "error", "请先登录")
		return
	}
	oldPass := r.FormValue("old_password")
	newPass := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	u.Email = strings.TrimSpace(r.FormValue("email"))
	u.Nickname = strings.TrimSpace(r.FormValue("nickname"))

	if newPass != "" {
		if !security.CheckPassword(u.Password, oldPass) {
			h.redirect(w, r, "/admin/profile", "error", "原密码不正确")
			return
		}
		if newPass != confirm {
			h.redirect(w, r, "/admin/profile", "error", "两次输入的新密码不一致")
			return
		}
		if err := security.ValidatePasswordStrength(newPass); err != nil {
			h.redirect(w, r, "/admin/profile", "error", err.Error())
			return
		}
		hash, err := security.HashPassword(newPass)
		if err != nil {
			h.redirect(w, r, "/admin/profile", "error", "密码处理失败")
			return
		}
		if err := h.repo.User.UpdatePassword(u.ID, hash); err != nil {
			h.redirect(w, r, "/admin/profile", "error", "更新密码失败")
			return
		}
	}
	if err := h.repo.User.Update(u); err != nil {
		h.redirect(w, r, "/admin/profile", "error", "保存失败")
		return
	}
	h.redirect(w, r, "/admin/profile", "success", "资料已更新")
}

// ---------- 设置 ----------

// Settings 系统设置
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "系统设置", "settings")
	list, err := h.repo.Set.ListDefs()
	if err != nil {
		h.redirect(w, r, "/admin", "error", "加载失败")
		return
	}
	// admin_path_legacy 是「历史后台路径」的内部记录（逗号分隔），
	// 由程序自动维护，不面向用户编辑，设置页不展示。
	visible := make([]*model.Setting, 0, len(list))
	for _, s := range list {
		if s.Key == "admin_path_legacy" {
			continue
		}
		visible = append(visible, s)
	}
	list = visible
	// 分组按 settings 表内定义的顺序去重
	// 分组按业务习惯排序（基本信息最常用，放在最前），未列出的分组排在最后
	groupOrder := []string{"general", "seo", "contact", "display", "social", "mail", "sms", "security"}
	rank := func(g string) int {
		for i, v := range groupOrder {
			if v == g {
				return i
			}
		}
		return len(groupOrder)
	}
	var groups []string
	seen := map[string]bool{}
	for _, s := range list {
		if !seen[s.Group] {
			seen[s.Group] = true
			groups = append(groups, s.Group)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return rank(groups[i]) < rank(groups[j]) })
	// 当前分组：?group= 优先，缺省取第一个分组
	active := utils.QueryStr(r, "group")
	if active == "" || !seen[active] {
		if len(groups) > 0 {
			active = groups[0]
		}
	}
	// 只渲染当前分组，避免左导航点了没反应
	filtered := make([]*model.Setting, 0, len(list))
	for _, s := range list {
		if s.Group == active {
			filtered = append(filtered, s)
		}
	}
	d.Settings = filtered
	d.Groups = groups
	d.Options = map[string]any{
		"active": active,
		// 历史后台路径：给「旧路径静默」的说明文案提供真实例子，
		// 避免在文案里写死 /admin（后台路径早已不是 admin 时那句话就会误导）
		"legacy": h.ap.Legacy(),
	}
	h.renderAdmin(w, r, "settings", d)
}

// SettingsSave 保存设置
func (h *Handler) SettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/settings", "error", "表单解析失败")
		return
	}
	list, err := h.repo.Set.ListDefs()
	if err != nil {
		h.redirect(w, r, "/admin/settings", "error", "加载失败")
		return
	}
	// 设置页一次只提交一个分组，必须只处理该分组，
	// 否则未出现在表单里的 bool 项会被误写成 0。
	group := strings.TrimSpace(r.FormValue("group"))
	kv := make(map[string]string)
	for _, s := range list {
		if group != "" && s.Group != group {
			continue
		}
		vals, exists := r.Form["setting_"+s.Key]
		if !exists {
			// 复选框未勾选 = 0（仅针对 bool 类型）
			if s.Type == "bool" {
				kv[s.Key] = "0"
			}
			continue
		}
		kv[s.Key] = strings.TrimSpace(vals[0])
	}

	// 以下两项不能跟着批量 upsert 一起落库，必须先校验：
	//   admin_path       —— 非法值写进库，重启后会回退默认值并让设置页显示出错内容
	//   admin_ip_allow   —— 写错的规则会让后台直接不可访问，必须校验通过才落库
	// 统一在 SetBatch 之前摘出来，校验通过后单独 Set。
	rawAdminPath, hasAdminPath := kv["admin_path"]
	delete(kv, "admin_path")

	rawIPAllow, hasIPAllow := kv["admin_ip_allow"]
	delete(kv, "admin_ip_allow")

	// ===== 二次验证 =====
	// 后台路径与 IP 白名单属于「一改就可能进不去」的高危操作，
	// 光有已登录的会话不够——会话被劫持、或人离开电脑没锁屏，都能造成不可逆后果。
	// 只要这两项发生实质变更，就必须重新输入当前账号密码。
	if h.needReauth(r, hasAdminPath, rawAdminPath, hasIPAllow, rawIPAllow) {
		if msg := h.checkReauth(r); msg != "" {
			h.redirect(w, r, "/admin/settings?group=security", "error", msg)
			return
		}
		h.audit(r, AuditReauthOK, "security", "修改安全项已通过密码验证")
	}

	if err := h.repo.Set.SetBatch(kv); err != nil {
		h.redirect(w, r, "/admin/settings", "error", "保存失败："+err.Error())
		return
	}
	// 记录改了哪些键（不记值，避免把短信密钥/token 之类写进日志）
	if len(kv) > 0 {
		keys := make([]string, 0, len(kv))
		for k := range kv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		h.audit(r, AuditSettingsSave, group, strings.Join(keys, ", "))
	}

	// 旧路径静默开关：关=旧路径 302 到新路径（书签友好）；开=旧路径 404（不泄露新路径）。
	// legacyRedirect 每次请求都读 ap.Silent()，所以这里改完立即生效，无需重建路由。
	silentChanged := false
	if v, ok := kv["admin_path_silent"]; ok {
		if want := v == "1"; want != h.ap.Silent() {
			h.ap.SetSilent(want)
			silentChanged = true
		}
	}

	// 后台 IP 白名单：解析失败时保留原规则并提示，避免写错一条就整组失效。
	// 特别注意「把自己锁在门外」：如果新规则不包含当前访问 IP，直接拒绝保存。
	if hasIPAllow {
		if h.ipGuard == nil {
			// 兜底：守卫未注入时退化为不做校验，但至少不丢用户输入
			_ = h.repo.Set.Set("admin_ip_allow", rawIPAllow)
		} else {
			list, invalid := security.ParseIPAllowList(rawIPAllow)
			if len(invalid) > 0 {
				h.redirect(w, r, "/admin/settings?group=security", "error",
					"IP 格式无法识别："+strings.Join(invalid, "、")+"（支持单个 IP 或 CIDR，如 192.168.1.0/24）")
				return
			}
			me := utils.ClientIP(r)
			if !list.Contains(me) {
				h.redirect(w, r, "/admin/settings?group=security", "error",
					"保存已拒绝：新规则不包含你当前使用的 IP（"+me+"），保存后将无法再进入后台。"+
						"请先把自己的 IP 或所在网段加进去。")
				return
			}
			// 校验通过才落库 + 生效
			if err := h.repo.Set.Set("admin_ip_allow", rawIPAllow); err != nil {
				slog.Warn("保存 IP 白名单失败", "err", err)
			}
			h.ipGuard.Set(list)
			slog.Info("后台 IP 白名单已更新", "rules", len(list.Rules()), "by", me)
			h.audit(r, AuditIPAllow, strings.Join(list.Rules(), " "),
				"共 "+strconv.Itoa(len(list.Rules()))+" 条规则")
		}
	}

	// 后台路径变更：校验 → 写库 → 重建路由 → 跳到新地址
	// 注意必须在跳转前完成重建，否则本次重定向会 404。
	newPathApplied := false
	if hasAdminPath {
		old := h.ap.Path()
		if msg := adminpath.Validate(rawAdminPath); msg != "" {
			h.redirect(w, r, "/admin/settings?group=security", "error", msg)
			return
		}
		clean, _ := adminpath.Sanitize(rawAdminPath)
		if clean != old {
			if h.ap.Set(clean) {
				newPathApplied = true
				// 路径本身与历史路径一起持久化，重启后旧地址仍能重定向
				if err := h.repo.Set.Set("admin_path", clean); err != nil {
					slog.Warn("保存后台路径失败", "err", err)
				}
				if err := h.repo.Set.Set("admin_path_legacy", strings.Join(h.ap.Legacy(), ",")); err != nil {
					slog.Warn("保存历史后台路径失败", "err", err)
				}
				if h.reloader != nil {
					h.reloader.Reload()
				}
				slog.Info("后台路径已变更", "old", old, "new", clean)
				h.audit(r, AuditAdminPath, clean, "由 /"+old+" 改为 /"+clean)
			}
		}
	}

	h.RefreshSite()

	target := "/admin/settings"
	if group != "" {
		target += "?group=" + url.QueryEscape(group)
	}
	if newPathApplied {
		// 路径变了，旧地址已经没有对应路由，直接给新地址
		target = h.adminPath("settings")
		if group != "" {
			target += "?group=" + url.QueryEscape(group)
		}
		msg := "后台路径已更新为 " + h.ap.URL()
		if h.ap.Silent() {
			msg += "，旧地址将直接返回 404"
		} else {
			msg += "，旧地址会自动跳转到新地址。建议开启「旧路径静默」以免泄露新入口"
		}
		h.redirect(w, r, target, "success", msg)
		return
	}
	if silentChanged {
		if h.ap.Silent() {
			h.redirect(w, r, target, "success", "已开启旧路径静默：历史后台地址现在返回 404")
		} else {
			h.redirect(w, r, target, "success", "已关闭旧路径静默：历史后台地址会 302 跳转到当前地址")
		}
		return
	}
	h.redirect(w, r, target, "success", "设置已保存")
}

// ---------- 主题 ----------

// ThemePage 主题管理
func (h *Handler) ThemePage(w http.ResponseWriter, r *http.Request) {
	d := h.adminView(r, "主题管理", "theme")
	themes := h.scanThemes()
	d.Themes = themes
	d.CurrentTheme = h.siteMap()["theme"]
	if d.CurrentTheme == "" {
		d.CurrentTheme = h.cfg.Theme
	}
	// 有主题信息时默认切到第一组
	if len(themes) > 0 && d.Groups == nil {
		d.Groups = []string{themes[0].Dir}
	}
	h.renderAdmin(w, r, "theme", d)
}

// scanThemes 扫描 themes 目录
func (h *Handler) scanThemes() []ThemeInfo {
	root := filepath.Dir(h.themeRoot) // themes/
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	current := h.siteMap()["theme"]
	if current == "" {
		current = h.cfg.Theme
	}

	var out []ThemeInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		themeDir := filepath.Join(root, dir)
		info := ThemeInfo{Dir: dir, Name: dir, Active: dir == current}
		// 读 theme.json
		if b, err := os.ReadFile(filepath.Join(themeDir, "theme.json")); err == nil {
			var meta struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Author      string `json:"author"`
				Description string `json:"description"`
				Screenshot  string `json:"screenshot"`
			}
			if json.Unmarshal(b, &meta) == nil {
				if meta.Name != "" {
					info.Name = meta.Name
				}
				info.Version = meta.Version
				info.Author = meta.Author
				info.Description = meta.Description
				info.Screenshot = meta.Screenshot
			}
		}
		if !dirExists(filepath.Join(themeDir, "templates")) {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// ThemeSwitch 切换主题
func (h *Handler) ThemeSwitch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirect(w, r, "/admin/theme", "error", "表单解析失败")
		return
	}
	dir := security.SlugifyASCII(r.FormValue("theme"))
	if dir == "" || strings.Contains(dir, "/") || strings.Contains(dir, "..") {
		h.redirect(w, r, "/admin/theme", "error", "主题名称非法")
		return
	}
	themesRoot := filepath.Dir(h.themeRoot)
	target := filepath.Join(themesRoot, dir)
	if !dirExists(filepath.Join(target, "templates")) {
		h.redirect(w, r, "/admin/theme", "error", "主题不存在或不完整")
		return
	}
	if err := h.rnd.SetTheme(target); err != nil {
		h.redirect(w, r, "/admin/theme", "error", "加载主题失败："+err.Error())
		return
	}
	h.themeRoot = target
	if err := h.repo.Set.Set("theme", dir); err != nil {
		h.redirect(w, r, "/admin/theme", "error", "保存失败："+err.Error())
		return
	}
	h.RefreshSite()
	h.audit(r, AuditThemeSwitch, dir, "前台主题已切换")
	h.redirect(w, r, "/admin/theme", "success", "已切换到主题："+dir)
}

// ---------- 媒体上传弹窗 & 编辑器图片上传 ----------

// MediaPickerJSON 媒体列表 JSON（供编辑器插图）
func (h *Handler) MediaPickerJSON(w http.ResponseWriter, r *http.Request) {
	page := max(1, utils.QueryInt(r, "page", 1))
	list, total, err := h.repo.Media.List("image", page, 24)
	if err != nil {
		utils.JSON(w, http.StatusInternalServerError, `{"error":"加载失败"}`)
		return
	}
	type item struct {
		ID   uint64 `json:"id"`
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	items := make([]item, 0, len(list))
	for _, m := range list {
		items = append(items, item{ID: m.ID, URL: m.URL, Name: m.Name})
	}
	resp := map[string]any{"items": items, "total": total, "page": page}
	b, _ := json.Marshal(resp)
	utils.JSON(w, http.StatusOK, string(b))
}

// UploadJSON 编辑器直传
func (h *Handler) UploadJSON(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, h.cfg.UploadMaxMB+2); err != nil {
		utils.JSON(w, http.StatusBadRequest, `{"error":"上传失败：表单解析错误"}`)
		return
	}
	fh, err := firstFile(r)
	if err != nil {
		utils.JSON(w, http.StatusBadRequest, `{"error":"未选择文件"}`)
		return
	}
	res, err := h.saveUpload(fh, "")
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		utils.JSON(w, http.StatusBadRequest, string(b))
		return
	}
	u := userOf(r)
	m := &model.Media{UserID: u.ID, Name: res.Name, Path: res.Path, URL: res.URL, Mime: res.Mime, Ext: res.Ext, Size: res.Size, Width: res.Width, Height: res.Height}
	if id, err := h.repo.Media.Create(m); err == nil {
		m.ID = id
	}
	b, _ := json.Marshal(map[string]any{"url": res.URL, "id": m.ID, "name": res.Name})
	utils.JSON(w, http.StatusOK, string(b))
}

func firstFile(r *http.Request) (*multipart.FileHeader, error) {
	if r.MultipartForm == nil {
		return nil, http.ErrMissingFile
	}
	if fhs, ok := r.MultipartForm.File["file"]; ok && len(fhs) > 0 {
		return fhs[0], nil
	}
	if fhs, ok := r.MultipartForm.File["files"]; ok && len(fhs) > 0 {
		return fhs[0], nil
	}
	return nil, http.ErrMissingFile
}

// ---------- 数据统计（简易） ----------

// StatsJSON 概览数据
func (h *Handler) StatsJSON(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"posts":    h.repo.Post.CountByStatus("published"),
		"drafts":   h.repo.Post.CountByStatus("draft"),
		"messages": h.repo.Msg.Count(),
		"unread":   h.repo.Msg.CountUnread(),
		"views":    h.repo.Post.TotalViews(),
	}
	b, _ := json.Marshal(resp)
	utils.JSON(w, http.StatusOK, string(b))
}

var _ = strconv.Itoa
