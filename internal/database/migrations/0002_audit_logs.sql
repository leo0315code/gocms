-- 操作审计日志
-- 记录后台的关键写操作，用于交接追溯与安全排查。
-- 只记「谁、什么时候、从哪个 IP、做了什么」，不记录敏感内容（密码等）。

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    username VARCHAR(50) NOT NULL DEFAULT '',
    action VARCHAR(40) NOT NULL DEFAULT '',      -- 动作标识，如 post.delete / settings.save
    target VARCHAR(120) NOT NULL DEFAULT '',     -- 操作对象，如文章标题、设置分组
    detail VARCHAR(500) NOT NULL DEFAULT '',     -- 补充说明（已脱敏）
    ip VARCHAR(45) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_audit_created (created_at),
    KEY idx_audit_user (user_id),
    KEY idx_audit_action (action)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
