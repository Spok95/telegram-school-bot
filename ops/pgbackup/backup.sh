#!/bin/sh
set -eu
ts=$(date +%F_%H%M%S)
out="/backups/school-${ts}.sql.gz"
pg_dump -h "${DB_HOST:-postgres}" -U "${DB_USER:-school}" -d "${DB_NAME:-school}" -Fc \
  | gzip > "$out"
# Храним 10 свежих
ls -1t /backups/school-*.sql.gz 2>/dev/null | tail -n +11 | xargs -r rm -f
echo "$out"
