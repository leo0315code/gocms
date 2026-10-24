#!/usr/bin/env bash
# 从 backup.sh 生成的单文件归档恢复数据库与 uploads。
# 会覆盖目标库全部表与 uploads/ 目录，必须显式输入库名确认。
# 数据库凭据：已导出的环境变量优先，其次 .env。
#
# 用法：bash scripts/restore.sh backups/gocms-<时间戳>.tar.gz   （或 make restore F=...）
set -euo pipefail
cd "$(dirname "$0")/.."

FILE="${1:-}"
if [ -z "$FILE" ] || [ ! -f "$FILE" ]; then
    echo "用法: $0 backups/gocms-<时间戳>.tar.gz" >&2
    exit 1
fi
[ -f .env ] || { echo "✗ 缺少 .env" >&2; exit 1; }

get_env() {
    local cur=""
    eval "cur=\"\${$1:-}\""
    if [ -n "$cur" ]; then
        printf '%s' "$cur"
        return
    fi
    grep -E "^$1=" .env | head -1 | cut -d= -f2- | sed 's/^"//;s/"$//'
}

DB_HOST="$(get_env DB_HOST)";  DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="$(get_env DB_PORT)";  DB_PORT="${DB_PORT:-3306}"
DB_USER="$(get_env DB_USER)";  DB_USER="${DB_USER:-root}"
DB_PASS="$(get_env DB_PASS)"
DB_NAME="$(get_env DB_NAME)";  DB_NAME="${DB_NAME:-gocms}"

echo "即将用 $FILE 覆盖数据库 $DB_NAME 与 uploads/ 目录。"
read -r -p "确认请输入库名 [$DB_NAME]：" ANSWER
if [ "$ANSWER" != "$DB_NAME" ]; then
    echo "已取消。"
    exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
tar -xzf "$FILE" -C "$STAGE"

if [ ! -f "$STAGE/database.sql" ]; then
    echo "✗ 归档内没有 database.sql，不是本项目的备份包？" >&2
    exit 1
fi

echo "→ 导入数据库 $DB_NAME ..."
mysql -h"$DB_HOST" -P"$DB_PORT" -u"$DB_USER" ${DB_PASS:+-p"$DB_PASS"} "$DB_NAME" < "$STAGE/database.sql"

if [ -d "$STAGE/uploads" ]; then
    echo "→ 恢复 uploads/ ..."
    rm -rf uploads
    mkdir -p uploads
    cp -R "$STAGE/uploads/." uploads/
fi

echo "✓ 恢复完成。记得重启进程（连接池里可能缓存着旧表结构信息）。"
