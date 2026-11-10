#!/usr/bin/env bash
# Backup diario de la base SQLite (consistente, soporta WAL).
# Cron sugerido (3:15 AM):
#   15 3 * * * /opt/grupo-sur/deploy/backup.sh >> /var/log/grupo-sur-backup.log 2>&1
set -euo pipefail

DB="${DB_PATH:-/opt/grupo-sur/data/gruposur.db}"
DEST="${BACKUP_DIR:-/opt/grupo-sur/backups}"
KEEP_DAYS="${KEEP_DAYS:-30}"
STAMP="$(date +%Y%m%d-%H%M%S)"

mkdir -p "$DEST"
OUT="$DEST/gruposur-$STAMP.db"

if command -v sqlite3 >/dev/null 2>&1; then
	# .backup hace una copia consistente incluyendo el WAL.
	sqlite3 "$DB" ".backup '$OUT'"
else
	# Fallback: copiar db + wal/shm.
	cp "$DB" "$OUT"
	[ -f "$DB-wal" ] && cp "$DB-wal" "$OUT-wal" || true
	[ -f "$DB-shm" ] && cp "$DB-shm" "$OUT-shm" || true
fi

gzip -f "$OUT"
echo "$(date -Is) backup OK: $OUT.gz"

# Retención.
find "$DEST" -name 'gruposur-*.db.gz' -mtime "+$KEEP_DAYS" -delete
