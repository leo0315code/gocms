#!/usr/bin/env bash
# 备份数据库 + uploads 到 backups/gocms-<时间戳>.tar.gz（单文件，好传输好归档）。
# 数据库凭据：已导出的环境变量优先，其次 .env（与应用读取语义一致）。
# 恢复用 scripts/restore.sh。
#
# 用法：bash scripts/backup.sh   （或 make backup）
set -euo pipefail
cd "$(dirname "$0")/.."

[ -f .env ] || { echo "✗ 缺少 .env" >&2; exit 1; }

# get_env KEY：取已设置的环境变量，否则回落到 .env 里同名键
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

OUT_DIR="backups"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="$OUT_DIR/gocms-$STAMP.tar.gz"
mkdir -p "$OUT_DIR"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "→ 导出数据库 $DB_NAME ..."
mysqldump -h"$DB_HOST" -P"$DB_PORT" -u"$DB_USER" \
    ${DB_PASS:+-p"$DB_PASS"} \
    --single-transaction --routines --triggers \
    --default-character-set=utf8mb4 --no-tablespaces \
    "$DB_NAME" > "$STAGE/database.sql"

echo "→ 打包 uploads/ ..."
mkdir -p "$STAGE/uploads"
if [ -d uploads ]; then
    cp -R uploads/. "$STAGE/uploads/"
fi

tar -czf "$OUT" -C "$STAGE" .

SIZE=$(ls -lh "$OUT" | awk '{print $5}')
echo "✓ 备份完成：$OUT ($SIZE)"
echo "  恢复：make restore F=$OUT"
