#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

: "${PGHOST:=postgres}"
: "${PGPORT:=5432}"
: "${PGUSER:=school}"
: "${PGDATABASE:=school}"
: "${BACKUP_DIR:=/backups}"

mkdir -p "$BACKUP_DIR"

for i in {1..30}; do
  if pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

ts="$(date -u +'%Y%m%dT%H%M%SZ')"
file="$BACKUP_DIR/school-$ts.sql.gz"

pg_dump -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" --no-owner --no-privileges \
  | gzip -9 > "$file"

ln -sfn "$(basename "$file")" "$BACKUP_DIR/latest.sql.gz"

# вот это бот и покажет
echo "$file"
