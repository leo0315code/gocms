package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// CategoryRepo 分类仓储
type CategoryRepo struct {
	g *gorm.DB
}

// List 全部分类（按 sort, id 排序）
func (r *CategoryRepo) List() ([]*model.Category, error) {
	var out []*model.Category
	if err := r.g.Order("sort ASC, id ASC").Find(&out).Error; err != nil {
		return nil, wrap("查询分类列表", err)
	}
	return out, nil
}

// ListByType 按类型列出
func (r *CategoryRepo) ListByType(typ string) ([]*model.Category, error) {
	var out []*model.Category
	if err := r.g.Where("type = ?", typ).Order("sort ASC, id ASC").Find(&out).Error; err != nil {
		return nil, wrap("按类型查询分类", err)
	}
	return out, nil
}

// Tree 构建树形结构
func (r *CategoryRepo) Tree() ([]*model.Category, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}
	return BuildTree(all), nil
}

// BuildTree 把平铺列表组装成树
func BuildTree(list []*model.Category) []*model.Category {
	byID := make(map[uint64]*model.Category, len(list))
	for _, c := range list {
		c.Children = nil
		byID[c.ID] = c
	}
	var roots []*model.Category
	for _, c := range list {
		if c.ParentID == 0 {
			roots = append(roots, c)
			continue
		}
		if p, ok := byID[c.ParentID]; ok {
			p.Children = append(p.Children, c)
			c.Parent = p
		} else {
			roots = append(roots, c)
		}
	}
	return roots
}

// ByID 按 ID 查询
func (r *CategoryRepo) ByID(id uint64) (*model.Category, error) {
	var c model.Category
	if err := r.g.First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询分类", err)
	}
	return &c, nil
}

// BySlug 按 slug 查询
func (r *CategoryRepo) BySlug(slug string) (*model.Category, error) {
	var c model.Category
	if err := r.g.Where("slug = ?", slug).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按别名查询分类", err)
	}
	return &c, nil
}

// Create 新建分类
func (r *CategoryRepo) Create(c *model.Category) (uint64, error) {
	if err := r.g.Create(c).Error; err != nil {
		return 0, wrap("创建分类", err)
	}
	return c.ID, nil
}

// Update 更新分类
func (r *CategoryRepo) Update(c *model.Category) error {
	err := r.g.Model(&model.Category{}).Where("id = ?", c.ID).
		Updates(map[string]any{
			"parent_id":       c.ParentID,
			"name":            c.Name,
			"slug":            c.Slug,
			"description":     c.Description,
			"cover":           c.Cover,
			"icon":            c.Icon,
			"type":            c.Type,
			"template":        c.Template,
			"sort":            c.Sort,
			"is_nav":          c.IsNav,
			"seo_title":       c.SEOTitle,
			"seo_keywords":    c.SEOKeywords,
			"seo_description": c.SEODescription,
		}).Error
	return wrap("更新分类", err)
}

// Delete 删除分类（子分类上提、文章置未分类、清自定义字段）
func (r *CategoryRepo) Delete(id uint64) error {
	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Category{}).Where("parent_id = ?", id).
			Update("parent_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Post{}).Where("category_id = ?", id).
			Update("category_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Where("category_id = ?", id).Delete(&model.Field{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Category{}).Error
	})
	return wrap("删除分类", err)
}

// ExistsSlug slug 是否重复
func (r *CategoryRepo) ExistsSlug(slug string, excludeID uint64) bool {
	var n int64
	q := r.g.Model(&model.Category{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// Count 分类总数
func (r *CategoryRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.Category{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// WithPostCount 给分类附加文章数（一次 GROUP BY，避免 N+1）
func (r *CategoryRepo) WithPostCount(list []*model.Category) []*model.Category {
	type row struct {
		CategoryID uint64 `gorm:"column:category_id"`
		N          int    `gorm:"column:n"`
	}
	var rows []row
	err := r.g.Model(&model.Post{}).
		Select("category_id, COUNT(*) AS n").
		Where("status = ?", "published").
		Group("category_id").
		Scan(&rows).Error
	if err != nil {
		return list
	}
	m := make(map[uint64]int, len(rows))
	for _, x := range rows {
		m[x.CategoryID] = x.N
	}
	for _, c := range list {
		c.PostCount = m[c.ID]
	}
	return list
}
