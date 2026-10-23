package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Connect 打开连接池并 ping 验证
func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(10 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return db, nil
}

// Open 用已有的 *sql.DB 构建 GORM 实例。
//
// 复用同一个连接池，不重复建池：database/sql 的连接池参数（最大连接、生命周期）
// 已经在 Connect 里调好，GORM 只是在其上加一层查询构造能力。
// 注意 GORM 的日志级别在开发环境设为 Warn，避免每条 SQL 都刷屏。
func Open(sqlDB *sql.DB, isDev bool) (*gorm.DB, error) {
	cfg := &gorm.Config{
		// 表名复数化交给各自的 TableName() 显式声明，关掉默认推断
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logLevel(isDev)),
		// 关闭默认事务包装：CMS 里大量单条读写，包事务反而拖慢
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Now().Local() },
	}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), cfg)
	if err != nil {
		return nil, fmt.Errorf("初始化 GORM 失败: %w", err)
	}
	return db, nil
}

func logLevel(isDev bool) logger.LogLevel {
	if isDev {
		return logger.Warn
	}
	return logger.Error
}

// EnsureDatabase 若库不存在则创建
func EnsureDatabase(dsnNoDB, dbName string) error {
	db, err := sql.Open("mysql", dsnNoDB)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		dbName,
	))
	return err
}
