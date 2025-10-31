#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

: "${PGHOST:=postgres}"
: "${PGPORT:=5432}"
: "${PGUSER:=postgres}"
: "${PGDATABASE:=school}"
: "${BACKUP_DIR:=/backups}"

mkdir -p "$BACKUP_DIR"

# ждём готовность Postgres (до 30с)
for i in {1..30}; do
  if pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

ts="$(date -u +'%Y%m%dT%H%M%SZ')"
file="$BACKUP_DIR/school-$ts.sql.gz"

# plain SQL + gzip. Если у тебя логины через PGPASSWORD — убедись, что он проброшен в контейнер.
pg_dump -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" --no-owner --no-privileges \
  | gzip -9 > "$file"

# symlink latest → текущий
ln -sfn "$(basename "$file")" "$BACKUP_DIR/latest.sql.gz"

# оставить только 10 последних
ls -1t "$BACKUP_DIR"/school-*.sql.gz 2>/dev/null | tail -n +11 | xargs -r rm -f
