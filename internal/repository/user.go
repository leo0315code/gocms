package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/example/gocms/internal/model"
)

// UserRepo 用户仓储
type UserRepo struct {
	g *gorm.DB
}

// ByUsername 按用户名查询
func (r *UserRepo) ByUsername(username string) (*model.User, error) {
	var u model.User
	if err := r.g.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按用户名查询用户", err)
	}
	return &u, nil
}

// ByID 按 ID 查询
func (r *UserRepo) ByID(id uint64) (*model.User, error) {
	var u model.User
	if err := r.g.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询用户", err)
	}
	return &u, nil
}

// List 列出全部用户
func (r *UserRepo) List() ([]*model.User, error) {
	var out []*model.User
	if err := r.g.Order("id ASC").Find(&out).Error; err != nil {
		return nil, wrap("查询用户列表", err)
	}
	return out, nil
}

// Count 用户总数
func (r *UserRepo) Count() int {
	var n int64
	if err := r.g.Model(&model.User{}).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// Create 新建用户
func (r *UserRepo) Create(u *model.User) (uint64, error) {
	if err := r.g.Create(u).Error; err != nil {
		return 0, wrap("创建用户", err)
	}
	return u.ID, nil
}

// Update 更新用户基础信息
func (r *UserRepo) Update(u *model.User) error {
	err := r.g.Model(&model.User{}).Where("id = ?", u.ID).
		Updates(map[string]any{
			"email":    u.Email,
			"nickname": u.Nickname,
			"avatar":   u.Avatar,
			"role":     u.Role,
			"status":   u.Status,
		}).Error
	return wrap("更新用户", err)
}

// UpdatePassword 改密
func (r *UserRepo) UpdatePassword(id uint64, hash string) error {
	err := r.g.Model(&model.User{}).Where("id = ?", id).
		Update("password", hash).Error
	return wrap("更新密码", err)
}

// TouchLogin 记录登录时间与 IP
func (r *UserRepo) TouchLogin(id uint64, ip string) error {
	err := r.g.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"last_login_at": time.Now(), "last_login_ip": ip}).Error
	return wrap("记录登录信息", err)
}

// Delete 删除用户
func (r *UserRepo) Delete(id uint64) error {
	err := r.g.Where("id = ?", id).Delete(&model.User{}).Error
	return wrap("删除用户", err)
}

// ExistsUsername 用户名是否已存在
func (r *UserRepo) ExistsUsername(username string, excludeID uint64) bool {
	var n int64
	q := r.g.Model(&model.User{}).Where("username = ?", username)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}
