package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/leo0315code/gocms/internal/model"
)

// MediaRepo 媒体仓储
type MediaRepo struct {
	g *gorm.DB
}

// List 分页列出，typ 可为 image/file/"" 全部
func (r *MediaRepo) List(typ string, page, perPage int) ([]*model.Media, int, error) {
	base := r.g.Model(&model.Media{})
	switch typ {
	case "image":
		base = base.Where("mime LIKE ?", "image/%")
	case "file":
		base = base.Where("mime NOT LIKE ?", "image/%")
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrap("统计媒体", err)
	}

	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 24
	}

	var out []*model.Media
	err := base.Order("id DESC").Limit(perPage).Offset((page - 1) * perPage).Find(&out).Error
	if err != nil {
		return nil, 0, wrap("查询媒体列表", err)
	}
	return out, int(total), nil
}

// ByID 按 ID
func (r *MediaRepo) ByID(id uint64) (*model.Media, error) {
	var m model.Media
	if err := r.g.First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询媒体", err)
	}
	return &m, nil
}

// Create 入库
func (r *MediaRepo) Create(m *model.Media) (uint64, error) {
	if err := r.g.Create(m).Error; err != nil {
		return 0, wrap("保存媒体", err)
	}
	return m.ID, nil
}

// Delete 删除记录
func (r *MediaRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.Media{}).Error
	return wrap("删除媒体", err)
}

// Count 总数
func (r *MediaRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.Media{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}
