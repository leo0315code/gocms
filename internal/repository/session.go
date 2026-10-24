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
