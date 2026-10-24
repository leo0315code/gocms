package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/leo0315code/gocms/internal/model"
)

// SessionRepo 会话仓储
type SessionRepo struct {
	g *gorm.DB
}

// Create 新建会话
func (r *SessionRepo) Create(s *model.Session) error {
	return wrap("创建会话", r.g.Create(s).Error)
}

// ByToken 按 token 查询未过期会话
func (r *SessionRepo) ByToken(token string) (*model.Session, error) {
	var s model.Session
	err := r.g.Where("token = ? AND expires_at > ?", token, time.Now()).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// BindUser 登录成功后绑定用户并重置 token（防会话固定）
func (r *SessionRepo) BindUser(sessionID, userID uint64, newToken, newCSRF string, expiresAt time.Time) error {
	err := r.g.Model(&model.Session{}).Where("id = ?", sessionID).
		Updates(map[string]any{
			"user_id":    userID,
			"token":      newToken,
			"csrf_token": newCSRF,
			"expires_at": expiresAt,
		}).Error
	return wrap("绑定会话用户", err)
}

// Delete 删除会话
func (r *SessionRepo) Delete(token string) error {
	return r.g.Where("token = ?", token).Delete(&model.Session{}).Error
}

// SessionView 会话列表视图（不含 token / csrf_token，列表页不暴露凭据）
type SessionView struct {
	ID        uint64
	UserID    uint64
	Username  string
	Nickname  string
	Role      string
	IP        string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ListActive 未过期的登录会话，按登录时间倒序，附带用户信息。
// 匿名访客会话（user_id=0）不在此列——管理页面只关心「谁在线」。
func (r *SessionRepo) ListActive() ([]*SessionView, error) {
	var out []*SessionView
	err := r.g.Table("sessions s").
		Select("s.id, s.user_id, s.ip, s.user_agent, s.created_at, s.expires_at, u.username, u.nickname, u.role").
		Joins("LEFT JOIN users u ON u.id = s.user_id").
		Where("s.expires_at > ? AND s.user_id > 0", time.Now()).
		Order("s.created_at DESC").
		Limit(200).
		Scan(&out).Error
	return out, wrap("查询在线会话", err)
}

// ByID 按主键查会话（踢下线前先取信息做审计）
func (r *SessionRepo) ByID(id uint64) (*model.Session, error) {
	var s model.Session
	err := r.g.First(&s, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询会话", err)
	}
	return &s, nil
}

// DeleteByID 按主键删除会话（踢下线）
func (r *SessionRepo) DeleteByID(id uint64) error {
	return wrap("删除会话", r.g.Where("id = ?", id).Delete(&model.Session{}).Error)
}

// DeleteByUser 删除某用户全部会话
func (r *SessionRepo) DeleteByUser(userID uint64) error {
	return r.g.Where("user_id = ?", userID).Delete(&model.Session{}).Error
}

// CleanExpired 清理过期会话
func (r *SessionRepo) CleanExpired() (int64, error) {
	res := r.g.Where("expires_at <= ?", time.Now()).Delete(&model.Session{})
	return res.RowsAffected, res.Error
}

// Touch 续期
func (r *SessionRepo) Touch(id uint64, expiresAt time.Time) error {
	return r.g.Model(&model.Session{}).Where("id = ?", id).
		Update("expires_at", expiresAt).Error
}
