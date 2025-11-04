# Telegram School Bot

Промышленный Telegram-бот для школ: учёт «баллов»/достижений, роли (ученик/родитель/учитель/администрация/админ), подтверждения, периоды учёта и экспорт. Реализован на Go.

## Возможности

- Роли: **student**, **parent**, **teacher**, **administration**, **admin**.
- Регистрация через /start с выбором роли (инлайн‑кнопки).
- Привязка родителя к ребёнку (FSM-процедура).
- Начисление/списание баллов по категориям (уровни 100/200/300): «Работа на уроке», «Курсы по выбору», «Внеурочная активность», «Социальные поступки», «Дежурство», «Аукцион».
- Подтверждения и статус начислений (черновик/на утверждении/подтверждено и т. п.).
- Периоды учёта (четверть/триместр/год) и подсчёт индивидуального/коллективного рейтинга класса.
- Экспорт и резервное копирование данных (команды /export, /backup, /restore).
- Встроенные миграции БД (goose, embed). На старте бота миграции применяются автоматически.
- Нотификатор учебного года (например, поздравления/напоминания).

## Команды бота

- `/add_score`
- `/approvals`
- `/auction`
- `/backup`
- `/cancel`
- `/export`
- `/my_score`
- `/periods`
- `/remove_score`
- `/restore`
- `/start`

## Технологический стек

- Go 1.24.4
- Telegram Bot API: [`go-telegram-bot-api/v5`](https://github.com/go-telegram-bot-api/telegram-bot-api)
- PostgreSQL 17 (в docker-compose), драйвер `pgx/v5`
- Миграции: `pressly/goose` (встроены через `embed.FS`)
- Тесты: `testcontainers-go`
- Сборка Docker (многостадийная, финальный образ на **distroless**)

## Структура проекта

```
cmd/bot/               # точка входа (main.go) — загрузка .env, запуск бота, автоприменение миграций
internal/app/          # маршрутизация апдейтов, фоновые задачи (notifier)
internal/bot/          # обработчики команд/кнопок/сообщений, FSM, клавиатуры, меню
  ├─ auth/             # FSM регистрации и привязок (родитель ↔ ребёнок и т.п.)
  ├─ handlers/         # handlers + embed-митации (handlers/migrations/*.sql)
  └─ shared/           # общие утилиты (fsmutil, защита от повторов и т. д.)
internal/models/       # модели домена (User, Score, Period, Class)
.github/workflows/     # CI (Go build/test)
Dockerfile
docker-compose.yml
Makefile
go.mod, go.sum
```

## Быстрый старт (Docker)

1. Подготовьте `.env` рядом с `docker-compose.yml`:
   ```env
   BOT_TOKEN=123456:ABC-DEF...   # токен бота от @BotFather
   ADMIN_ID=123456789            # Telegram ID главного админа
   # DATABASE_URL переопределять не нужно — в compose используется внутренняя сеть
   ```
2. Запуск:
   ```bash
   docker compose up -d --build
   ```
3. Проверить логи:
   ```bash
   docker compose logs -f bot
   ```
Бот применит миграции сам и начнёт принимать апдейты.

> По умолчанию PostgreSQL проброшен на `localhost:5433` (внутри сети — `postgres:5432`).

## Локальная разработка (без Docker)

Требуется Go и запущенный PostgreSQL.

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/school?sslmode=disable'
export BOT_TOKEN='...'
export ADMIN_ID='...'
go run ./cmd/bot
```

### Миграции

- Миграции лежат в `internal/bot/handlers/migrations/*.sql` и вшиты через `embed.FS`.
- На старте приложения вызывается `goose.Up()` — всё применится автоматически.
- Для ручного управления (если понадобится):
  ```bash
  # пример: применить локальные миграции из папки
  goose -dir internal/bot/handlers/migrations postgres "$DATABASE_URL" up
  goose -dir internal/bot/handlers/migrations postgres "$DATABASE_URL" status
  ```

## Переменные окружения

| Ключ        | Обязательно | Пример/значение                                    |
|-------------|-------------|----------------------------------------------------|
| `BOT_TOKEN` | да          | Токен Telegram бота                               |
| `ADMIN_ID`  | да          | Числовой Telegram ID администратора               |
| `DATABASE_URL` | да      | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `GIT_SHA`   | нет         | SHA коммита для логов/метрик (если прокидываете)  |
| `TZ`        | нет         | Часовой пояс контейнера (в Dockerfile: Europe/Bucharest) |

## Makefile (основные цели)

Если у вас установлен `make`:

```bash
make up         # поднять postgres+bot
make restart    # перезапустить только bot
make logs       # смотреть логи
make down       # остановить контейнеры (томи с данными останутся)
make nuke       # ⚠ удалить и контейнеры, и том с БД
make test       # go test -race ./...
make lint       # go vet ./...
```

> Реальный список целей смотрите в [`Makefile`](./Makefile).

## База данных (схема, по верхам)

- `users` — пользователи Telegram с ролью, привязкой к классу и (для родителей) к ребёнку.
- `classes` — классы 1–11 × А/Б/В/Г/Д, поле `collective_score` для командного рейтинга.
- `categories`, `score_levels` — справочники категорий и «весов» (100/200/300).
- `scores` — начисления/списания баллов (+ комментарии, статус, автор/утверждающий).
- `parents_students` — связи родитель ↔ ребёнок.
- `periods` — учебные периоды.

Миграции созданы и заполняют базовые справочники (см. `internal/bot/handlers/migrations`).

## Разграничение доступа (суть)

- **Учитель/Администрация/Админ** — все операции с баллами (с ограничениями по активности), подтверждение/экспорт.
- **Родитель** — привязка к ребёнку, просмотр/подтверждения (по логике проекта).
- **Ученик** — просмотр собственного рейтинга (`/my_score`).

Фактические проверки смотрите в `internal/bot/shared/fsmutil` и обработчиках (`internal/app` и `internal/bot/handlers`).

## Сборка Docker-образа

```bash
docker build -t telegram-school-bot:local .
docker run --rm -e BOT_TOKEN=... -e ADMIN_ID=... -e DATABASE_URL=... telegram-school-bot:local
```

Образ финализирован на **gcr.io/distroless/base-debian12** (минимум уязвимостей).

## CI

GitHub Actions (`.github/workflows/ci.yml`) кеширует `go mod`, гоняет `go test -race`.
Если в пайплаине нужны миграции/интеграционные тесты — подготовьте секреты и БД.

## Лицензия

Проект распространяется под лицензией, указанной в корне репозитория (LICENSE). Если файл отсутствует — права принадлежат владельцу проекта.

---

**Заметки по продакшену**: включайте алерты по отказам БД, бэкапы, вращение логов, мониторинг задержек Telegram API; ограничивайте права админов и используйте `ADMIN_ID` whitelist. Настройте регулярный экспорт и тесты миграций.
