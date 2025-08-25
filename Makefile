.PHONY: up up-all build restart down nuke logs

.PHONY: test bench lint

test:
	GOFLAGS=-count=1 go test -race ./...

bench:
	go test -run '^$$' -bench . ./internal/db -benchtime=3s -benchmem

lint:
	go vet ./...

# Поднять без изменения БД (если образов нет — скачаются, БД остаётся)
up:
	docker compose up -d postgres bot

# Поднять c пересборкой образов, БД остаётся
up-all: build
	docker compose up -d postgres bot

build:
	docker compose build

# Перезапустить только приложение (БД не трогаем)
restart:
	docker compose restart bot

# Остановить контейнеры, НО ТОМ С ДАННЫМИ НЕ УДАЛЯТЬ
down:
	docker compose down --remove-orphans

# <<< ОПАСНО >>> Полный сброс: удалить контейнеры + ТОМ С БД
nuke:
	docker compose down -v --remove-orphans

logs:
	docker compose logs -f bot
