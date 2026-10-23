package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("记录不存在")

// Repo 仓储集合
//
// 全部仓储已迁移到 GORM。DB 字段保留给未来需要手写 SQL 的复杂统计场景
// （如多表聚合报表），与 GORM 共用同一个连接池，不额外占用连接。
type Repo struct {
	DB     *sql.DB
	Gorm   *gorm.DB
	User   *UserRepo
	Post   *PostRepo
	Page   *PageRepo
	Cat    *CategoryRepo
	Tag    *TagRepo
	Media  *MediaRepo
	Menu   *MenuRepo
	Banner *BannerRepo
	Link   *LinkRepo
	Msg    *MessageRepo
	Set    *SettingRepo
	Field  *FieldRepo
	Sess   *SessionRepo
	Audit  *AuditRepo
}

// New 初始化全部仓储
func New(db *sql.DB, gdb *gorm.DB) *Repo {
	return &Repo{
		DB:     db,
		Gorm:   gdb,
		User:   &UserRepo{g: gdb},
		Post:   &PostRepo{g: gdb},
		Page:   &PageRepo{g: gdb},
		Cat:    &CategoryRepo{g: gdb},
		Tag:    &TagRepo{g: gdb},
		Media:  &MediaRepo{g: gdb},
		Menu:   &MenuRepo{g: gdb},
		Banner: &BannerRepo{g: gdb},
		Link:   &LinkRepo{g: gdb},
		Msg:    &MessageRepo{g: gdb},
		Set:    &SettingRepo{g: gdb},
		Field:  &FieldRepo{g: gdb},
		Sess:   &SessionRepo{g: gdb},
		Audit:  &AuditRepo{g: gdb},
	}
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
