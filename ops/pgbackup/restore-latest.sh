#!/bin/sh
set -eu
latest=$(ls -1t /backups/school-*.sql.gz 2>/dev/null | head -1 || true)
if [ -z "$latest" ]; then
  echo "no backups"
  exit 1
fi
gunzip -c "$latest" \
  | pg_restore -h "${DB_HOST:-postgres}" -U "${DB_USER:-school}" -d "${DB_NAME:-school}" \
      --no-owner --no-privileges --clean --if-exists
echo "$latest"
