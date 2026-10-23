package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// PageRepo 单页仓储
type PageRepo struct {
	g *gorm.DB
}

// List 全部单页
func (r *PageRepo) List() ([]*model.Page, error) {
	var out []*model.Page
	if err := r.g.Order("sort ASC, id ASC").Find(&out).Error; err != nil {
		return nil, wrap("查询单页列表", err)
	}
	return out, nil
}

// ListNav 导航中显示的单页
func (r *PageRepo) ListNav() ([]*model.Page, error) {
	var out []*model.Page
	err := r.g.Where("is_nav = 1 AND status = ?", "published").
		Order("sort ASC, id ASC").Find(&out).Error
	if err != nil {
		return nil, wrap("查询导航单页", err)
	}
	return out, nil
}

// ByID 按 ID
func (r *PageRepo) ByID(id uint64) (*model.Page, error) {
	var p model.Page
	if err := r.g.First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询单页", err)
	}
	return &p, nil
}

// BySlug 按 slug
func (r *PageRepo) BySlug(slug string) (*model.Page, error) {
	var p model.Page
	if err := r.g.Where("slug = ?", slug).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按别名查询单页", err)
	}
	return &p, nil
}

// Create 新建
func (r *PageRepo) Create(p *model.Page) (uint64, error) {
	if err := r.g.Create(p).Error; err != nil {
		return 0, wrap("创建单页", err)
	}
	return p.ID, nil
}

// Update 更新
func (r *PageRepo) Update(p *model.Page) error {
	err := r.g.Model(&model.Page{}).Where("id = ?", p.ID).
		Updates(map[string]any{
			"title":           p.Title,
			"slug":            p.Slug,
			"content":         p.Content,
			"content_format":  p.ContentFormat,
			"cover":           p.Cover,
			"template":        p.Template,
			"status":          p.Status,
			"sort":            p.Sort,
			"is_nav":          p.IsNav,
			"seo_title":       p.SEOTitle,
			"seo_keywords":    p.SEOKeywords,
			"seo_description": p.SEODescription,
		}).Error
	return wrap("更新单页", err)
}

// Delete 删除
func (r *PageRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.Page{}).Error
	return wrap("删除单页", err)
}

// ExistsSlug slug 是否重复
func (r *PageRepo) ExistsSlug(slug string, excludeID uint64) bool {
	var n int64
	q := r.g.Model(&model.Page{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// Count 总数
func (r *PageRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.Page{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}
