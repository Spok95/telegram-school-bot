#!/usr/bin/env bash
set -Eeuo pipefail

: "${PGHOST:=postgres}"
: "${PGPORT:=5432}"
: "${PGUSER:=postgres}"
: "${PGDATABASE:=school}"
: "${BACKUP_DIR:=/backups}"

latest="$BACKUP_DIR/latest.sql.gz"
if [[ ! -f "$latest" ]]; then
  echo "no latest.sql.gz in $BACKUP_DIR" >&2
  exit 1
fi

# обнулим public, чтобы не было конфликтов
psql "host=$PGHOST port=$PGPORT user=$PGUSER dbname=$PGDATABASE" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  PERFORM 1;
  EXECUTE 'DROP SCHEMA IF EXISTS public CASCADE';
  EXECUTE 'CREATE SCHEMA public';
  EXECUTE 'GRANT ALL ON SCHEMA public TO public';
EXCEPTION WHEN OTHERS THEN
  RAISE;
END $$;
SQL

# рестор
zcat "$latest" \
 | psql "host=$PGHOST port=$PGPORT user=$PGUSER dbname=$PGDATABASE" -v ON_ERROR_STOP=1
