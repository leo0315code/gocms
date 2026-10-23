package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// LinkRepo 友情链接仓储
type LinkRepo struct {
	g *gorm.DB
}

// List 列出友链
func (r *LinkRepo) List(onlyShow bool) ([]*model.Link, error) {
	q := r.g.Order("sort ASC, id ASC")
	if onlyShow {
		q = q.Where("is_show = 1")
	}
	var out []*model.Link
	if err := q.Find(&out).Error; err != nil {
		return nil, wrap("查询友链列表", err)
	}
	return out, nil
}

// ByID 按 ID
func (r *LinkRepo) ByID(id uint64) (*model.Link, error) {
	var l model.Link
	if err := r.g.First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询友链", err)
	}
	return &l, nil
}

// Create 新建
func (r *LinkRepo) Create(l *model.Link) (uint64, error) {
	if err := r.g.Create(l).Error; err != nil {
		return 0, wrap("创建友链", err)
	}
	return l.ID, nil
}

// Update 更新
func (r *LinkRepo) Update(l *model.Link) error {
	err := r.g.Model(&model.Link{}).Where("id = ?", l.ID).
		Updates(map[string]any{
			"name":        l.Name,
			"url":         l.URL,
			"logo":        l.Logo,
			"description": l.Description,
			"sort":        l.Sort,
			"is_show":     l.IsShow,
		}).Error
	return wrap("更新友链", err)
}

// Delete 删除
func (r *LinkRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.Link{}).Error
	return wrap("删除友链", err)
}
