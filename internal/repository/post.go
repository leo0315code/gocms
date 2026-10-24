package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/leo0315code/gocms/internal/model"
)

// PostRepo 文章仓储
type PostRepo struct {
	g *gorm.DB
}

// PostFilter 列表筛选条件
type PostFilter struct {
	CategoryID  uint64
	CategoryIDs []uint64
	TagID       uint64
	Status      string
	Keyword     string
	IsRecommend bool
	OnlyTop     bool
	ExcludeID   uint64
	OrderBy     string // latest | views | sort
	Page        int
	PerPage     int
	// 后台用：包含草稿
	AdminView bool
}

// build 依据筛选条件构建查询（不含分页）
func (f *PostFilter) build(g *gorm.DB) *gorm.DB {
	q := g.Model(&model.Post{})

	if !f.AdminView {
		if f.Status != "" {
			q = q.Where("status = ?", f.Status)
		} else {
			q = q.Where("status = ?", "published")
		}
		q = q.Where("(published_at IS NULL OR published_at <= ?)", time.Now())
	} else if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	if f.CategoryID > 0 {
		q = q.Where("category_id = ?", f.CategoryID)
	}
	if len(f.CategoryIDs) > 0 {
		q = q.Where("category_id IN ?", f.CategoryIDs)
	}
	if f.TagID > 0 {
		q = q.Where("id IN (?)",
			g.Model(&model.PostTag{}).Select("post_id").Where("tag_id = ?", f.TagID))
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("(title LIKE ? OR summary LIKE ?)", kw, kw)
	}
	if f.IsRecommend {
		q = q.Where("is_recommend = 1")
	}
	if f.OnlyTop {
		q = q.Where("is_top = 1")
	}
	if f.ExcludeID > 0 {
		q = q.Where("id <> ?", f.ExcludeID)
	}
	return q
}

// order 排序子句
func (f *PostFilter) order(g *gorm.DB) *gorm.DB {
	switch f.OrderBy {
	case "views":
		return g.Order("is_top DESC, views DESC, id DESC")
	case "sort":
		return g.Order("is_top DESC, sort ASC, id DESC")
	default:
		return g.Order("is_top DESC, COALESCE(published_at, created_at) DESC, id DESC")
	}
}

// Search 全文/模糊搜索（标题+摘要+正文）。
//
// 关键字 ≥ 2 字符时走 ngram FULLTEXT 布尔短语检索（倒排索引，不扫全表）；
// 单字符检索词低于 ngram 最小 token 长度（服务端默认 2），索引里根本没有
// 对应 token，退回 LIKE 兜底，行为与旧实现一致。
//
// 已知行为：InnoDB FULLTEXT 对刚提交的行有极短的可见延迟，等价于
// 「刚发布的文章可能晚几秒才被搜到」，对站内搜索无实际影响。
func (r *PostRepo) Search(keyword string, page, perPage int) ([]*model.Post, int, error) {
	base := r.g.Model(&model.Post{}).
		Where("status = ?", "published").
		Where("(published_at IS NULL OR published_at <= ?)", time.Now())

	if ft := fulltextKeyword(keyword); ft != "" {
		base = base.Where("MATCH(title, summary, content) AGAINST(? IN BOOLEAN MODE)", ft)
	} else {
		kw := "%" + keyword + "%"
		base = base.Where("(title LIKE ? OR summary LIKE ? OR content LIKE ?)", kw, kw, kw)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrap("搜索文章", err)
	}

	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 10
	}

	var out []*model.Post
	err := base.Order("COALESCE(published_at, created_at) DESC, id DESC").
		Limit(perPage).Offset((page - 1) * perPage).Find(&out).Error
	if err != nil {
		return nil, 0, wrap("搜索文章", err)
	}
	return out, int(total), nil
}

// fulltextKeyword 把检索词转成布尔模式的短语检索串；返回空串表示不适合走
// FULLTEXT，调用方应退回 LIKE。
//
// 用双引号包成短语而不是裸词：布尔模式下裸的 ngram 分词结果之间是 OR 关系，
// 「花园」会被拆成 花/园 两个 token 各自命中，出现「花盆、园丁」这种误召回；
// 短语检索要求分词结果连续出现，等效于子串匹配。
func fulltextKeyword(kw string) string {
	kw = strings.TrimSpace(kw)
	if len([]rune(kw)) < 2 {
		return ""
	}
	// 短语串内的双引号与反斜杠会破坏布尔模式语法，直接剥掉
	var b strings.Builder
	for _, r := range kw {
		if r != '"' && r != '\\' {
			b.WriteRune(r)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return ""
	}
	return `"` + b.String() + `"`
}

// List 条件列表
func (r *PostRepo) List(f *PostFilter) ([]*model.Post, int, error) {
	var total int64
	if err := f.build(r.g).Count(&total).Error; err != nil {
		return nil, 0, wrap("统计文章", err)
	}

	if f.PerPage <= 0 {
		f.PerPage = 10
	}
	if f.Page < 1 {
		f.Page = 1
	}

	var out []*model.Post
	q := f.order(f.build(r.g)).Limit(f.PerPage).Offset((f.Page - 1) * f.PerPage)
	if err := q.Find(&out).Error; err != nil {
		return nil, 0, wrap("查询文章列表", err)
	}
	if err := r.attachCategories(out); err != nil {
		return nil, 0, err
	}
	return out, int(total), nil
}

// ByID 按 ID 查询（含标签与分类）
func (r *PostRepo) ByID(id uint64) (*model.Post, error) {
	var p model.Post
	if err := r.g.First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询文章", err)
	}
	if err := r.attachCategories([]*model.Post{&p}); err != nil {
		return nil, err
	}
	if tags, err := r.TagsOf(p.ID); err == nil {
		p.Tags = tags
	}
	return &p, nil
}

// BySlug 按 slug 查询
func (r *PostRepo) BySlug(slug string) (*model.Post, error) {
	var p model.Post
	if err := r.g.Where("slug = ?", slug).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按别名查询文章", err)
	}
	if err := r.attachCategories([]*model.Post{&p}); err != nil {
		return nil, err
	}
	if tags, err := r.TagsOf(p.ID); err == nil {
		p.Tags = tags
	}
	return &p, nil
}

// attachCategories 批量填充分类（一次 IN 查询，避免 N+1）
func (r *PostRepo) attachCategories(list []*model.Post) error {
	var ids []uint64
	seen := make(map[uint64]bool)
	for _, p := range list {
		if p.CategoryID > 0 && !seen[p.CategoryID] {
			seen[p.CategoryID] = true
			ids = append(ids, p.CategoryID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var cats []*model.Category
	if err := r.g.Where("id IN ?", ids).Find(&cats).Error; err != nil {
		return wrap("查询文章分类", err)
	}
	m := make(map[uint64]*model.Category, len(cats))
	for _, c := range cats {
		m[c.ID] = c
	}
	for _, p := range list {
		if c, ok := m[p.CategoryID]; ok {
			p.Category = c
		}
	}
	return nil
}

// Prev 上一篇（发布时间更早）
func (r *PostRepo) Prev(p *model.Post) *model.Post {
	t := p.CreatedAt
	if p.PublishedAt != nil {
		t = *p.PublishedAt
	}
	var out model.Post
	err := r.g.Where("status = ?", "published").
		Where("(COALESCE(published_at, created_at) < ? OR (COALESCE(published_at, created_at) = ? AND id < ?))", t, t, p.ID).
		Order("COALESCE(published_at, created_at) DESC, id DESC").
		First(&out).Error
	if err != nil {
		return nil
	}
	return &out
}

// Next 下一篇（发布时间更晚）
func (r *PostRepo) Next(p *model.Post) *model.Post {
	t := p.CreatedAt
	if p.PublishedAt != nil {
		t = *p.PublishedAt
	}
	var out model.Post
	err := r.g.Where("status = ?", "published").
		Where("(COALESCE(published_at, created_at) > ? OR (COALESCE(published_at, created_at) = ? AND id > ?))", t, t, p.ID).
		Order("COALESCE(published_at, created_at) ASC, id ASC").
		First(&out).Error
	if err != nil {
		return nil
	}
	return &out
}

// Related 同分类相关文章
func (r *PostRepo) Related(p *model.Post, limit int) []*model.Post {
	if p.CategoryID == 0 || limit <= 0 {
		return nil
	}
	list, _, err := r.List(&PostFilter{CategoryID: p.CategoryID, ExcludeID: p.ID, PerPage: limit, OrderBy: "latest"})
	if err != nil {
		return nil
	}
	return list
}

// Create 新建文章
func (r *PostRepo) Create(p *model.Post) (uint64, error) {
	if err := r.g.Create(p).Error; err != nil {
		return 0, wrap("创建文章", err)
	}
	return p.ID, nil
}

// Update 更新文章
func (r *PostRepo) Update(p *model.Post) error {
	err := r.g.Model(&model.Post{}).Where("id = ?", p.ID).
		Updates(map[string]any{
			"category_id":     p.CategoryID,
			"title":           p.Title,
			"slug":            p.Slug,
			"summary":         p.Summary,
			"content":         p.Content,
			"content_format":  p.ContentFormat,
			"cover":           p.Cover,
			"status":          p.Status,
			"is_top":          p.IsTop,
			"is_recommend":    p.IsRecommend,
			"sort":            p.Sort,
			"template":        p.Template,
			"seo_title":       p.SEOTitle,
			"seo_keywords":    p.SEOKeywords,
			"seo_description": p.SEODescription,
			"published_at":    p.PublishedAt,
		}).Error
	return wrap("更新文章", err)
}

// Delete 物理删除（连带标签关联与自定义字段值）
func (r *PostRepo) Delete(id uint64) error {
	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", id).Delete(&model.PostTag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("post_id = ?", id).Delete(&model.PostFieldValue{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Post{}).Error
	})
	return wrap("删除文章", err)
}

// UpdateViews 浏览量 +1
//
// 只保留给单条即时场景（如后台手动调整）。
// 前台访问请走 viewcount.Counter：每次访问写一次库会把热文的行锁打满，
// 而且「刷新一次 +1」本身也不符合浏览量的语义。
func (r *PostRepo) UpdateViews(id uint64) {
	_ = r.g.Model(&model.Post{}).Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

// AddViews 批量累加浏览量。
//
// 一条 UPDATE 处理整批，避免 N 次往返：
//
//	UPDATE posts SET views = views + CASE id WHEN 1 THEN 5 WHEN 2 THEN 3 END WHERE id IN (1,2)
//
// id 与增量都是整型，直接内联是安全的；不用参数占位是因为 CASE 里的
// WHEN 值无法用 ? 批量绑定（占位符数量随 map 长度变化）。
func (r *PostRepo) AddViews(deltas map[uint64]int64) error {
	if len(deltas) == 0 {
		return nil
	}
	var sb strings.Builder
	ids := make([]uint64, 0, len(deltas))
	sb.WriteString("views + CASE id")
	for id, d := range deltas {
		if d == 0 {
			continue
		}
		fmt.Fprintf(&sb, " WHEN %d THEN %d", id, d)
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	sb.WriteString(" END")

	return wrap("累加浏览量", r.g.Model(&model.Post{}).
		Where("id IN ?", ids).
		UpdateColumn("views", gorm.Expr(sb.String())).Error)
}

// CountByStatus 按状态统计
func (r *PostRepo) CountByStatus(status string) int {
	var n int64
	if err := r.g.Model(&model.Post{}).Where("status = ?", status).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// TotalViews 总浏览量
func (r *PostRepo) TotalViews() int {
	var sum sql.NullInt64
	err := r.g.Model(&model.Post{}).Select("COALESCE(SUM(views), 0)").Scan(&sum).Error
	if err != nil || !sum.Valid {
		return 0
	}
	return int(sum.Int64)
}

// ExistsSlug slug 是否重复
func (r *PostRepo) ExistsSlug(slug string, excludeID uint64) bool {
	var n int64
	q := r.g.Model(&model.Post{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// ---------- 标签关联 ----------

// TagsOf 查询文章标签
func (r *PostRepo) TagsOf(postID uint64) ([]*model.Tag, error) {
	var out []*model.Tag
	err := r.g.Model(&model.Tag{}).
		Joins("INNER JOIN post_tags pt ON pt.tag_id = tags.id").
		Where("pt.post_id = ?", postID).
		Order("tags.id ASC").
		Find(&out).Error
	if err != nil {
		return nil, wrap("查询文章标签", err)
	}
	return out, nil
}

// SyncTags 同步文章标签（按名称列表），完成后重算受影响标签的计数
func (r *PostRepo) SyncTags(postID uint64, names []string) error {
	var oldTagIDs []uint64
	_ = r.g.Model(&model.PostTag{}).Where("post_id = ?", postID).
		Pluck("tag_id", &oldTagIDs).Error

	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", postID).Delete(&model.PostTag{}).Error; err != nil {
			return err
		}

		seen := make(map[string]bool)
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true

			var tagID uint64
			var tag model.Tag
			err := tx.Where("name = ?", name).First(&tag).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				slug := slugOrRandom(name)
				// slug 冲突时加后缀
				for i := 0; i < 5; i++ {
					var n int64
					tx.Model(&model.Tag{}).Where("slug = ?", slug).Count(&n)
					if n == 0 {
						break
					}
					slug = slugOrRandom(name) + "-" + fmt.Sprint(i)
				}
				nt := &model.Tag{Name: name, Slug: slug}
				if err := tx.Create(nt).Error; err != nil {
					continue
				}
				tagID = nt.ID
			} else if err != nil {
				continue
			} else {
				tagID = tag.ID
			}

			link := &model.PostTag{PostID: postID, TagID: tagID}
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(link).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return wrap("同步文章标签", err)
	}

	// 重算受影响标签的计数（事务外，失败不影响主流程）
	var newTagIDs []uint64
	_ = r.g.Model(&model.PostTag{}).Where("post_id = ?", postID).
		Pluck("tag_id", &newTagIDs).Error
	touched := append(oldTagIDs, newTagIDs...)
	for _, tid := range touched {
		_ = r.g.Model(&model.Tag{}).Where("id = ?", tid).
			UpdateColumn("count", gorm.Expr("(SELECT COUNT(*) FROM post_tags WHERE tag_id = ?)", tid)).Error
	}
	return nil
}
