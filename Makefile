VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT  ?= $(shell git rev-parse --short HEAD)

.PHONY: run build fmt tidy lint test bench up up-all restart down nuke logs

GO ?= go

run:
	ENV=dev HTTP_ADDR=":8080" LOG_LEVEL=debug $(GO) run ./cmd/bot

build:
	$(GO) build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o ./bin/bot ./cmd/bot

fmt:
	gofumpt -w .
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

lint:
	golangci-lint run

test:
	GOFLAGS=-count=1 $(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...

bench:
	$(GO) test -run '^$$' -bench . ./internal/db -benchtime=10s -benchmem

# Поднять без изменения БД
up:
	docker compose up -d postgres bot

# Полный пересбор образов (если понадобится)
up-all:
	docker compose up -d --build postgres bot

restart:
	docker compose restart bot

down:
	docker compose down --remove-orphans

nuke:
	docker compose down -v --remove-orphans

logs:
	docker compose logs -f bot

backup:
	docker compose exec pgbackup sh -lc 'pg_dump -h $${DB_HOST:-postgres} -U $${DB_USER:-school} -d $${DB_NAME:-school} -Fc | gzip > /backups/manual-$$(date +%F-%H%M).sql.gz'
