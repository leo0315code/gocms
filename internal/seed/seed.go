package seed

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/leo0315code/gocms/internal/config"
	"github.com/leo0315code/gocms/internal/model"
	"github.com/leo0315code/gocms/internal/repository"
	"github.com/leo0315code/gocms/internal/security"
)

// Run 初始化管理员、默认设置、演示内容
func Run(cfg *config.Config, repo *repository.Repo) error {
	if err := seedSettings(repo); err != nil {
		return err
	}
	adminID, err := seedAdmin(cfg, repo)
	if err != nil {
		return err
	}
	if err := seedMenus(repo); err != nil {
		return err
	}
	// 已有内容则跳过演示数据
	if repo.Post.CountByStatus("published") > 0 {
		return nil
	}
	return seedContent(repo, adminID)
}

// seedAdmin 建管理员（仅当没有任何用户）
func seedAdmin(cfg *config.Config, repo *repository.Repo) (uint64, error) {
	if repo.User.Count() > 0 {
		u, err := repo.User.ByUsername(cfg.AdminUser)
		if err == nil {
			return u.ID, nil
		}
		users, _ := repo.User.List()
		if len(users) > 0 {
			return users[0].ID, nil
		}
	}
	hash, err := security.HashPassword(cfg.AdminPass)
	if err != nil {
		return 0, err
	}
	u := &model.User{
		Username: cfg.AdminUser,
		Password: hash,
		Email:    cfg.AdminEmail,
		Nickname: "站长",
		Role:     "admin",
		Status:   1,
	}
	id, err := repo.User.Create(u)
	if err != nil {
		return 0, fmt.Errorf("创建管理员失败: %w", err)
	}
	slog.Info("已创建管理员账号", "username", cfg.AdminUser)
	return id, nil
}

type settingDef struct {
	Key, Value, Group, Label, Type string
	Sort                           int
}

// seedSettings 默认设置项
func seedSettings(repo *repository.Repo) error {
	defs := []settingDef{
		{"site_name", "云启科技", "general", "站点名称", "text", 1},
		{"site_slogan", "让技术驱动业务增长", "general", "站点副标题", "text", 2},
		{"site_url", "http://localhost:8090", "general", "站点地址", "text", 3},
		{"site_logo", "", "general", "站点 Logo", "image", 4},
		{"site_favicon", "", "general", "站点图标", "image", 5},
		{"site_icp", "京ICP备00000000号", "general", "备案号", "text", 6},
		{"site_copyright", "© 2026 云启科技 版权所有", "general", "版权信息", "text", 7},

		{"seo_title", "云启科技 - 企业数字化解决方案提供商", "seo", "默认 SEO 标题", "text", 1},
		{"seo_keywords", "企业官网,数字化,软件开发,云服务", "seo", "默认 SEO 关键词", "text", 2},
		{"seo_description", "云启科技提供企业数字化咨询、软件开发与云服务，助力企业高效增长。", "seo", "默认 SEO 描述", "textarea", 3},

		{"contact_phone", "400-888-8888", "contact", "联系电话", "text", 1},
		{"contact_email", "contact@example.com", "contact", "联系邮箱", "text", 2},
		{"contact_address", "北京市朝阳区科技园区 8 号楼", "contact", "公司地址", "text", 3},
		{"contact_hours", "周一至周五 09:00 - 18:00", "contact", "工作时间", "text", 4},

		{"theme", "default", "display", "当前主题", "text", 1},
		{"page_size", "10", "display", "每页条数", "number", 2},
		{"footer_about", "云启科技专注企业数字化服务十余年，服务客户超过 500 家。", "display", "页脚简介", "textarea", 3},
		{"comment_enabled", "0", "display", "开启评论", "bool", 4},

		{"social_wechat", "", "social", "微信公众号", "text", 1},
		{"social_weibo", "", "social", "微博链接", "text", 2},

		// 安全分组：后台入口路径。
		// 值随 .env ADMIN_PATH 初始化，之后由后台页面修改，存库即时生效。
		{"admin_path", "", "security", "后台入口路径", "text", 1},
		{"admin_path_silent", "0", "security", "旧路径静默（返回 404）", "bool", 2},
		{"admin_ip_allow", "", "security", "后台 IP 白名单", "textarea", 3},
	}

	for _, d := range defs {
		s := &model.Setting{
			Key: d.Key, Value: d.Value, Group: d.Group,
			Label: d.Label, Type: d.Type, Sort: d.Sort,
		}
		if err := repo.Set.AddDef(s); err != nil {
			return err
		}
	}
	return nil
}

// seedMenus 默认菜单与导航项
func seedMenus(repo *repository.Repo) error {
	menus, err := repo.Menu.List()
	if err != nil {
		return err
	}
	if len(menus) > 0 {
		return nil
	}
	if _, err := repo.Menu.CreateMenu(&model.Menu{Name: "主导航", Key: "main"}); err != nil {
		return err
	}
	if _, err := repo.Menu.CreateMenu(&model.Menu{Name: "页脚导航", Key: "footer"}); err != nil {
		return err
	}
	return nil
}

// seedContent 演示分类、文章、单页、轮播、友链、留言
func seedContent(repo *repository.Repo, adminID uint64) error {
	type catDef struct {
		Name, Slug, Desc string
		Sort             int
		IsNav            int
	}
	cats := []catDef{
		{"关于我们", "about", "了解云启科技的发展历程、团队与文化", 1, 1},
		{"产品中心", "products", "覆盖云平台、数据中台与行业解决方案", 2, 1},
		{"解决方案", "solutions", "面向制造、零售、医疗的数字化方案", 3, 1},
		{"新闻动态", "news", "公司新闻、行业洞察与活动资讯", 4, 1},
		{"客户案例", "cases", "客户成功故事与实施经验分享", 5, 1},
	}
	catIDs := map[string]uint64{}
	for _, c := range cats {
		id, err := repo.Cat.Create(&model.Category{
			Name:        c.Name,
			Slug:        c.Slug,
			Description: c.Desc,
			Type:        "post",
			Sort:        c.Sort,
			IsNav:       c.IsNav,
		})
		if err != nil {
			return err
		}
		catIDs[c.Slug] = id
	}

	// 单页（先建单页，菜单项才能回填真实 ID）
	pages := []struct {
		Title, Slug, Content string
		Sort, IsNav          int
	}{
		{"联系我们", "contact", `<h2>联系方式</h2>
<p>电话：400-888-8888<br>邮箱：contact@example.com<br>地址：北京市朝阳区科技园区 8 号楼</p>
<h2>商务合作</h2>
<p>如需方案咨询或项目合作，请通过上方表单留言，我们会在 1 个工作日内回复。</p>`, 1, 0},
		{"隐私政策", "privacy", `<h2>隐私政策</h2>
<p>我们重视您的隐私。本页面说明我们如何收集、使用与保护您的个人信息。</p>
<h3>信息收集</h3>
<p>当您通过站内表单提交咨询时，我们会收集您填写的姓名、联系方式与需求描述，仅用于业务沟通。</p>
<h3>信息保护</h3>
<p>我们采用行业通行的安全措施保护您的数据，不会向第三方出售或共享您的个人信息。</p>`, 2, 0},
	}
	pageIDs := map[string]uint64{}
	for _, p := range pages {
		id, err := repo.Page.Create(&model.Page{
			Title:         p.Title,
			Slug:          p.Slug,
			Content:       p.Content,
			ContentFormat: "html",
			Status:        "published",
			Sort:          p.Sort,
			IsNav:         p.IsNav,
		})
		if err != nil {
			return err
		}
		pageIDs[p.Slug] = id
	}

	// 主导航菜单项
	menus, _ := repo.Menu.List()
	var mainID uint64
	for _, m := range menus {
		if m.Key == "main" {
			mainID = m.ID
		}
	}
	if mainID > 0 {
		items := []struct {
			Title, Slug, Page string
			Sort              int
		}{
			{"首页", "", "", 0},
			{"关于我们", "about", "", 1},
			{"产品中心", "products", "", 2},
			{"解决方案", "solutions", "", 3},
			{"新闻动态", "news", "", 4},
			{"客户案例", "cases", "", 5},
			{"联系我们", "", "contact", 6},
		}
		for _, it := range items {
			mi := &model.MenuItem{
				MenuID: mainID,
				Title:  it.Title,
				Sort:   it.Sort,
				IsShow: 1,
				Target: "_self",
			}
			switch {
			case it.Page != "":
				mi.LinkType = "page"
				mi.LinkID = pageIDs[it.Page]
				mi.URL = "/p/" + it.Page
			case it.Slug != "":
				mi.LinkType = "category"
				mi.LinkID = catIDs[it.Slug]
				mi.URL = "/c/" + it.Slug
			default:
				mi.LinkType = "custom"
				mi.URL = "/"
			}
			if _, err := repo.Menu.CreateItem(mi); err != nil {
				return err
			}
		}
	}

	// 文章
	type postDef struct {
		Title, Slug, Cat, Summary, Content string
		Days                               int
		Views                              int
		Recommend, Top                     int
	}
	posts := []postDef{
		{
			"云启科技发布新一代数据中台 3.0", "data-platform-3", "news",
			"全新架构支持实时数据接入与湖仓一体，查询性能提升 3 倍。",
			`<p>云启科技今日正式发布数据中台 3.0，围绕"实时、统一、智能"三个关键词完成全面升级。</p>
<h2>实时数据接入</h2>
<p>新版本将数据接入延迟从分钟级降至秒级，支持 Kafka、CDC、API 等多种数据源。</p>
<h2>湖仓一体架构</h2>
<p>通过统一元数据层打通数据湖与数据仓库，避免数据重复存储与口径不一致的问题。</p>
<h2>性能提升</h2>
<p>在同等硬件条件下，复杂多表关联查询性能提升约 3 倍，资源占用下降 40%。</p>`,
			2, 328, 1, 1,
		},
		{
			"制造业数字化转型的三个关键阶段", "manufacturing-digital", "solutions",
			"从单点信息化到全链路协同，制造企业如何分阶段推进数字化。",
			`<p>制造业数字化转型不是一次性工程，而是分阶段的持续演进。基于 200+ 制造企业服务经验，我们将其归纳为三个阶段。</p>
<h2>第一阶段：单点信息化</h2>
<p>解决某个具体环节的效率问题，例如 MES 系统替代纸质工单、WMS 优化库存周转。</p>
<h2>第二阶段：数据打通</h2>
<p>把 ERP、MES、WMS、PLM 之间的数据打通，实现订单到交付的端到端可视化。</p>
<h2>第三阶段：智能决策</h2>
<p>在数据打通的基础上，引入排产优化、质量预测、能耗优化等智能应用。</p>
<p>大多数企业处于第一到第二阶段之间，建议先夯实数据基础，再谈智能应用。</p>`,
			5, 512, 1, 0,
		},
		{
			"如何选择适合企业的云服务架构", "cloud-architecture", "solutions",
			"公有云、私有云还是混合云？从成本、合规与业务弹性三个维度做选择。",
			`<p>云架构选型没有标准答案，只有适配度。建议从三个维度评估。</p>
<h2>成本维度</h2>
<p>业务波动大的场景适合公有云的弹性付费；负载稳定的核心系统，私有云或自建长期成本更低。</p>
<h2>合规维度</h2>
<p>涉及敏感数据（如患者信息、金融数据）时，需考虑数据驻留与等保要求，混合云往往是折中方案。</p>
<h2>弹性维度</h2>
<p>大促、开学季等有明显峰谷的业务，需要云平台具备分钟级扩容能力。</p>`,
			8, 289, 1, 0,
		},
		{
			"某连锁零售企业会员系统重构实录", "retail-member-system", "cases",
			"将分散在 12 个系统的会员数据统一到一套中台，复购率提升 18%。",
			`<p>客户是拥有 300 家门店的连锁零售企业，会员数据分散在 POS、小程序、电商和线下储值系统中。</p>
<h2>问题</h2>
<p>会员在各渠道身份不统一，积分与权益无法互通，营销活动重复触达。</p>
<h2>方案</h2>
<p>我们搭建统一会员中台，通过手机号、UnionID 与实体会员卡做多维度身份合并，建立 OneID。</p>
<h2>效果</h2>
<p>上线 6 个月后，会员识别率从 61% 提升至 94%，跨渠道复购率提升 18%。</p>`,
			12, 431, 1, 0,
		},
		{
			"云启科技完成 B 轮融资", "funding-b-round", "news",
			"本轮融资将主要用于产品研发与区域服务网络建设。",
			`<p>云启科技宣布完成 B 轮融资，本轮资金将重点投入三个方面。</p>
<ul>
<li>数据中台与 AI 能力的研发投入</li>
<li>华东、华南区域服务中心建设</li>
<li>行业解决方案的标准化沉淀</li>
</ul>
<p>感谢客户与投资伙伴的信任，我们将持续做好产品与服务。</p>`,
			15, 198, 0, 0,
		},
		{
			"企业官网建设的 10 个常见误区", "website-pitfalls", "news",
			"从信息架构到移动端体验，避开这些坑能让官网真正带来线索。",
			`<p>我们复盘了近三年 100+ 企业官网项目，总结了最常见的误区。</p>
<h2>1. 首页信息过载</h2>
<p>把所有业务都堆在首屏，访客反而抓不住重点。</p>
<h2>2. 缺少明确的行动指引</h2>
<p>每个页面都应该有且仅有一个主要转化目标。</p>
<h2>3. 忽略移动端</h2>
<p>超过 60% 的访问来自手机，但很多官网在手机上体验很差。</p>
<h2>4. 内容长期不更新</h2>
<p>新闻页停留在两年前，会让访客质疑企业是否还在正常运营。</p>
<h2>5. 没有做 SEO 基础工作</h2>
<p>标题、描述、结构化数据、sitemap 这些基础项缺失，搜索引擎很难收录。</p>`,
			20, 623, 0, 0,
		},
		{
			"关于我们", "about-us", "about",
			"云启科技成立于 2014 年，专注企业数字化服务。",
			`<h2>公司简介</h2>
<p>云启科技成立于 2014 年，是一家专注企业数字化服务的技术公司。我们为制造、零售、医疗等行业客户提供咨询、软件开发与云服务。</p>
<h2>我们的团队</h2>
<p>团队现有成员 120 余人，其中研发与交付人员占比超过 70%。</p>
<h2>我们的价值观</h2>
<ul>
<li><strong>客户成功</strong>：客户的业务结果是我们唯一的衡量标准</li>
<li><strong>长期主义</strong>：不做一次性交付，做可持续演进</li>
<li><strong>技术务实</strong>：选合适的技术，不追新概念</li>
</ul>`,
			30, 756, 0, 0,
		},
		{
			"云原生架构落地的现实路径", "cloud-native-path", "products",
			"不要一上来就谈 Service Mesh，先把容器化与可观测性做好。",
			`<p>云原生是目标，不是起点。多数企业的可行路径如下。</p>
<h2>第一步：容器化</h2>
<p>先把应用打包成容器，统一构建与发布流程。这一步的收益最直接。</p>
<h2>第二步：可观测性</h2>
<p>日志、指标、链路三件套到位，否则出了问题无从查起。</p>
<h2>第三步：编排与治理</h2>
<p>在有多个服务且确实需要弹性时，再引入 Kubernetes 与服务网格。</p>`,
			25, 344, 0, 0,
		},
	}

	now := time.Now()
	for i, p := range posts {
		pub := now.AddDate(0, 0, -p.Days).Add(time.Duration(i) * time.Hour)
		post := &model.Post{
			CategoryID:    catIDs[p.Cat],
			UserID:        adminID,
			Title:         p.Title,
			Slug:          p.Slug,
			Summary:       p.Summary,
			Content:       p.Content,
			ContentFormat: "html",
			Status:        "published",
			IsRecommend:   p.Recommend,
			IsTop:         p.Top,
			Views:         p.Views,
			Sort:          0,
			PublishedAt:   &pub,
		}
		id, err := repo.Post.Create(post)
		if err != nil {
			return err
		}
		// 标签
		tagSets := map[string][]string{
			"news":      {"公司新闻", "产品发布"},
			"solutions": {"解决方案", "数字化转型"},
			"cases":     {"客户案例", "零售"},
			"about":     {"公司介绍"},
			"products":  {"产品", "云原生"},
		}
		_ = repo.Post.SyncTags(id, tagSets[p.Cat])
	}

	// 轮播
	banners := []model.Banner{
		{Title: "让技术驱动业务增长", Subtitle: "企业数字化咨询 · 软件开发 · 云服务", URL: "/c/solutions", ButtonText: "了解方案", Position: "home", Sort: 1, IsShow: 1},
		{Title: "数据中台 3.0 正式发布", Subtitle: "实时接入 · 湖仓一体 · 性能提升 3 倍", URL: "/a/data-platform-3", ButtonText: "查看详情", Position: "home", Sort: 2, IsShow: 1},
		{Title: "500+ 企业的共同选择", Subtitle: "制造 · 零售 · 医疗 行业数字化实践", URL: "/c/cases", ButtonText: "客户案例", Position: "home", Sort: 3, IsShow: 1},
	}
	for i := range banners {
		_, _ = repo.Banner.Create(&banners[i])
	}

	// 友链
	links := []model.Link{
		{Name: "开源中国", URL: "https://www.oschina.net", Description: "开源技术社区", Sort: 1, IsShow: 1},
		{Name: "Go 语言中文网", URL: "https://studygolang.com", Description: "Go 语言学习社区", Sort: 2, IsShow: 1},
		{Name: "MySQL 官方文档", URL: "https://dev.mysql.com/doc/", Description: "MySQL 参考手册", Sort: 3, IsShow: 1},
	}
	for i := range links {
		_, _ = repo.Link.Create(&links[i])
	}

	// 留言
	msgs := []model.Message{
		{Name: "张先生", Phone: "13800138000", Email: "zhang@example.com", Company: "某制造企业", Content: "我们有一套 2015 年的 ERP 系统，想了解数据打通方案的可行性，方便约个时间沟通吗？", Status: "unread", IP: "127.0.0.1"},
		{Name: "李女士", Phone: "13900139000", Email: "li@example.com", Company: "连锁零售", Content: "看到你们的会员中台案例，我们的情况比较类似，想了解实施周期和大致预算。", Status: "read", IP: "127.0.0.1"},
	}
	for i := range msgs {
		_, _ = repo.Msg.Create(&msgs[i])
	}

	// 产品类目的自定义字段演示
	productCatID := catIDs["products"]
	if productCatID > 0 {
		demoFields := []model.Field{
			{CategoryID: productCatID, Name: "version", Label: "产品版本", Type: "text", Placeholder: "如 v3.0", Sort: 1},
			{CategoryID: productCatID, Name: "price", Label: "参考价格", Type: "text", Placeholder: "如 ￥98,000 起", Sort: 2},
			{CategoryID: productCatID, Name: "deploy", Label: "部署方式", Type: "select", Options: "公有云\n私有云\n混合云", Sort: 3},
			{CategoryID: productCatID, Name: "features", Label: "核心功能", Type: "textarea", Placeholder: "每行一条", Sort: 4},
		}
		for i := range demoFields {
			_, _ = repo.Field.Create(&demoFields[i])
		}
	}

	slog.Info("演示数据已生成", "posts", len(posts), "categories", len(cats))
	_ = json.Marshal
	return nil
}
