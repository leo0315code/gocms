package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// BannerRepo 轮播仓储
type BannerRepo struct {
	g *gorm.DB
}

// List 列出轮播，position 为空则全部
func (r *BannerRepo) List(position string, onlyShow bool) ([]*model.Banner, error) {
	q := r.g.Order("sort ASC, id ASC")
	if position != "" {
		q = q.Where("position = ?", position)
	}
	if onlyShow {
		q = q.Where("is_show = 1")
	}
	var out []*model.Banner
	if err := q.Find(&out).Error; err != nil {
		return nil, wrap("查询轮播列表", err)
	}
	return out, nil
}

// ByID 按 ID
func (r *BannerRepo) ByID(id uint64) (*model.Banner, error) {
	var b model.Banner
	if err := r.g.First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询轮播", err)
	}
	return &b, nil
}

// Create 新建
func (r *BannerRepo) Create(b *model.Banner) (uint64, error) {
	if err := r.g.Create(b).Error; err != nil {
		return 0, wrap("创建轮播", err)
	}
	return b.ID, nil
}

// Update 更新
func (r *BannerRepo) Update(b *model.Banner) error {
	err := r.g.Model(&model.Banner{}).Where("id = ?", b.ID).
		Updates(map[string]any{
			"title":       b.Title,
			"subtitle":    b.Subtitle,
			"image":       b.Image,
			"url":         b.URL,
			"button_text": b.ButtonText,
			"position":    b.Position,
			"sort":        b.Sort,
			"is_show":     b.IsShow,
		}).Error
	return wrap("更新轮播", err)
}

// Delete 删除
func (r *BannerRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.Banner{}).Error
	return wrap("删除轮播", err)
}
