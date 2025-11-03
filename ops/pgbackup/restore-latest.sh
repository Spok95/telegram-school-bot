#!/bin/sh
set -e

BACKUP_DIR=/backups
FILE="$BACKUP_DIR/latest.sql.gz"

PGHOST="${PGHOST:-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-school}"
PGDATABASE="${PGDATABASE:-school}"

if [ ! -f "$FILE" ]; then
  echo "no latest.sql.gz in $BACKUP_DIR"
  exit 1
fi

# всё шумное — в /dev/null
psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null 2>&1

gzip -dc "$FILE" | psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" >/dev/null 2>&1

# а это уже попадёт в телеграм
echo "$FILE"
