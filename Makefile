# Полный перезапуск с чистого тома БД и пересборкой образов
restart:
	docker compose down -v
	docker compose build --no-cache
	docker compose up -d
	docker compose logs -f bot

# Быстрый перезапуск без пересборки образов
reup:
	docker compose down -v
	docker compose up -d
	docker compose logs -f bot

# Только пересобрать и перезапустить (без удаления volume)
rebuild:
	docker compose build --no-cache
	docker compose up -d
	docker compose logs -f bot

# Полезняшки
logs:
	docker compose logs -f bot

psql:
	docker exec -it $$(docker ps -qf name=postgres) psql -U school -d school

migrate:
	docker compose up migrate

migrate-status:
	goose -dir ./migrations postgres "$(DATABASE_URL)" status

migrate-up:
	goose -dir ./migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir ./migrations postgres "$(DATABASE_URL)" down

migrate-redo:
	goose -dir ./migrations postgres "$(DATABASE_URL)" redo
