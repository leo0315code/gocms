package repository

import (
	"time"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// AuditRepo 操作审计仓储
type AuditRepo struct {
	g *gorm.DB
}

// AuditFilter 日志筛选条件
type AuditFilter struct {
	Username string
	Action   string
	Keyword  string
	Page     int
	PerPage  int
}

// Create 写入一条审计记录。
// 失败只返回错误，由调用方决定是否忽略——审计不应阻断业务。
func (r *AuditRepo) Create(l *model.AuditLog) error {
	return wrap("写入审计日志", r.g.Create(l).Error)
}

// List 分页查询
func (r *AuditRepo) List(f *AuditFilter) ([]*model.AuditLog, int, error) {
	q := r.g.Model(&model.AuditLog{})

	if f.Username != "" {
		q = q.Where("username = ?", f.Username)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("(target LIKE ? OR detail LIKE ?)", kw, kw)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap("统计审计日志", err)
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage <= 0 {
		f.PerPage = 30
	}

	var out []*model.AuditLog
	err := q.Order("id DESC").Limit(f.PerPage).Offset((f.Page - 1) * f.PerPage).Find(&out).Error
	if err != nil {
		return nil, 0, wrap("查询审计日志", err)
	}
	return out, int(total), nil
}

// DistinctActions 出现过的动作类型，供筛选下拉使用
func (r *AuditRepo) DistinctActions() []string {
	var out []string
	_ = r.g.Model(&model.AuditLog{}).
		Distinct().Order("action ASC").Pluck("action", &out).Error
	return out
}

// DistinctUsers 出现过的操作人
func (r *AuditRepo) DistinctUsers() []string {
	var out []string
	_ = r.g.Model(&model.AuditLog{}).
		Distinct().Order("username ASC").Pluck("username", &out).Error
	return out
}

// PurgeBefore 删除指定时间之前的日志，返回删除行数
func (r *AuditRepo) PurgeBefore(t time.Time) int64 {
	res := r.g.Where("created_at < ?", t).Delete(&model.AuditLog{})
	if res.Error != nil {
		return 0
	}
	return res.RowsAffected
}

// Count 总数
func (r *AuditRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.AuditLog{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}
