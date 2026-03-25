#!/bin/sh
set -eu

echo "[pgbackup] installing python3…"
apk add --no-cache python3 curl >/dev/null

# каталоги
mkdir -p /www/cgi-bin /backups /var/log
# скопировать CGI в рабочий (rw) каталог
cp -f /seed/cgi-bin/* /www/cgi-bin/ 2>/dev/null || true
chmod +x /www/cgi-bin/* 2>/dev/null || true

# healthz
echo "ok" >/www/healthz

# старт CGI-сервера
python3 -m http.server --cgi 8081 --bind 0.0.0.0 -d /www >/var/log/pgbackup.http.log 2>&1 &
HTTP_PID=$!

# cron: бэкап в 05:00 MSK и сразу подготовим файл лога
echo '0 5 * * * /usr/local/bin/backup.sh >>/var/log/pgbackup.cron.log 2>&1' | crontab -
: >/var/log/pgbackup.cron.log
crond -b -l 8

# подождём, пока HTTP начнёт отвечать (до 30 сек)
for i in $(seq 1 30); do
  if curl -fsS http://localhost:8081/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# держим контейнер живым, пока жив HTTP
trap 'kill $HTTP_PID 2>/dev/null || true; exit 0' TERM INT
wait $HTTP_PID
