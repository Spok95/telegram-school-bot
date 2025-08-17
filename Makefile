.PHONY: restart build up down logs

restart: down build up

build:
	docker compose build

up:
	docker compose up -d postgres
	docker compose up -d bot

down:
	docker compose down -v --remove-orphans

logs:
	docker compose logs -f bot
