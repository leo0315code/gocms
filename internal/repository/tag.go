package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
	"github.com/example/gocms/internal/security"
)

// slugOrRandom 生成 slug，空则随机
func slugOrRandom(s string) string {
	s = security.Slugify(s)
	if s == "" {
		return security.RandomToken(4)
	}
	return s
}

// TagRepo 标签仓储
type TagRepo struct {
	g *gorm.DB
}

// List 全部标签（按计数倒序）
func (r *TagRepo) List(limit int) ([]*model.Tag, error) {
	q := r.g.Order("`count` DESC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []*model.Tag
	if err := q.Find(&out).Error; err != nil {
		return nil, wrap("查询标签列表", err)
	}
	return out, nil
}

// ByID 按 ID
func (r *TagRepo) ByID(id uint64) (*model.Tag, error) {
	var t model.Tag
	if err := r.g.First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询标签", err)
	}
	return &t, nil
}

// BySlug 按 slug
func (r *TagRepo) BySlug(slug string) (*model.Tag, error) {
	var t model.Tag
	if err := r.g.Where("slug = ?", slug).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按别名查询标签", err)
	}
	return &t, nil
}

// Create 新建
func (r *TagRepo) Create(t *model.Tag) (uint64, error) {
	if err := r.g.Create(t).Error; err != nil {
		return 0, wrap("创建标签", err)
	}
	return t.ID, nil
}

// Update 更新
func (r *TagRepo) Update(t *model.Tag) error {
	err := r.g.Model(&model.Tag{}).Where("id = ?", t.ID).
		Updates(map[string]any{"name": t.Name, "slug": t.Slug}).Error
	return wrap("更新标签", err)
}

// Delete 删除（先清关联表，再删标签本体）
func (r *TagRepo) Delete(id uint64) error {
	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM post_tags WHERE tag_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Tag{}).Error
	})
	return wrap("删除标签", err)
}

// Count 总数
func (r *TagRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.Tag{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}
