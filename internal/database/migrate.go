package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate 执行 migrations 目录下未执行过的 SQL 文件（按文件名排序）。
//
// 全程持有 MySQL 命名锁并在单条连接上执行：多实例同时启动（或测试里
// 多个包同时迁移）时，后到者会先等锁，拿到锁后重新读 schema_migrations，
// 已应用的版本自然跳过。没有这把锁，两边都会看到「未应用」然后重复执行
// ALTER，后执行的会撞重复索引直接失败，还会留下半迁移状态。
func Migrate(db *sql.DB) error {
	ctx := context.Background()
	// 迁移与锁必须跑在同一条连接上：GET_LOCK 是会话级的，
	// 连接池随手换连接会让锁形同虚设、解锁也解不到点上。
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer conn.Close()

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		"SELECT GET_LOCK('gocms_migrations', 30)").Scan(&locked); err != nil {
		return fmt.Errorf("获取迁移锁失败: %w", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return fmt.Errorf("获取迁移锁超时，可能有其他实例正在执行迁移")
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK('gocms_migrations')")
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(100) NOT NULL PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return fmt.Errorf("创建迁移记录表失败: %w", err)
	}

	applied := make(map[string]bool)
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(content)) {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("执行迁移 %s 失败: %w\nSQL: %s", name, err, stmt)
			}
		}
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return err
		}
		slog.Info("迁移已执行", "version", version)
	}
	return nil
}

// splitStatements 按分号切分 SQL（忽略注释行与空语句）
func splitStatements(s string) []string {
	var out []string
	var buf strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(buf.String())
			stmt = strings.TrimSuffix(stmt, ";")
			if stmt != "" {
				out = append(out, stmt)
			}
			buf.Reset()
		}
	}
	if rest := strings.TrimSpace(buf.String()); rest != "" {
		out = append(out, strings.TrimSuffix(rest, ";"))
	}
	return out
}
