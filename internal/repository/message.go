package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/leo0315code/gocms/internal/model"
)

// MessageRepo 留言仓储
type MessageRepo struct {
	g *gorm.DB
}

// List 分页列出，status 为空则全部
func (r *MessageRepo) List(status string, page, perPage int) ([]*model.Message, int, error) {
	base := r.g.Model(&model.Message{})
	if status != "" {
		base = base.Where("status = ?", status)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrap("统计留言", err)
	}

	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}

	var out []*model.Message
	err := base.Order("id DESC").Limit(perPage).Offset((page - 1) * perPage).Find(&out).Error
	if err != nil {
		return nil, 0, wrap("查询留言列表", err)
	}
	return out, int(total), nil
}

// ByID 按 ID
func (r *MessageRepo) ByID(id uint64) (*model.Message, error) {
	var m model.Message
	if err := r.g.First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询留言", err)
	}
	return &m, nil
}

// Create 新建（默认未读）
func (r *MessageRepo) Create(m *model.Message) (uint64, error) {
	m.Status = "unread"
	if err := r.g.Create(m).Error; err != nil {
		return 0, wrap("保存留言", err)
	}
	return m.ID, nil
}

// MarkStatus 改状态
func (r *MessageRepo) MarkStatus(id uint64, status string) error {
	err := r.g.Model(&model.Message{}).Where("id = ?", id).Update("status", status).Error
	return wrap("更新留言状态", err)
}

// Reply 回复
func (r *MessageRepo) Reply(id uint64, reply string) error {
	err := r.g.Model(&model.Message{}).Where("id = ?", id).
		Updates(map[string]any{
			"reply":      reply,
			"replied_at": time.Now(),
			"status":     "replied",
		}).Error
	return wrap("回复留言", err)
}

// Delete 删除
func (r *MessageRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.Message{}).Error
	return wrap("删除留言", err)
}

// CountUnread 未读数
func (r *MessageRepo) CountUnread() int {
	var n int64
	if err := r.g.Model(&model.Message{}).Where("status = ?", "unread").Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// Count 总数
func (r *MessageRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.Message{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// CountRecentIP 统计同 IP 近期提交数（频率限制）
func (r *MessageRepo) CountRecentIP(ip string, d time.Duration) int {
	var n int64
	err := r.g.Model(&model.Message{}).
		Where("ip = ? AND created_at > ?", ip, time.Now().Add(-d)).
		Count(&n).Error
	if err != nil {
		return 0
	}
	return int(n)
}
