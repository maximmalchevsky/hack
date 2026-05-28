.PHONY: help up down logs ps restart \
	dev-up dev-down dev-logs dev-ps dev-restart dev-build \
	build build-api build-worker build-scheduler build-web \
	migrate migrate-down migrate-create \
	tidy lint vet test \
	web-install web-dev web-build \
	seed clean

# ===== Help =====
help:
	@echo "WorkTime Sync — команды Makefile"
	@echo ""
	@echo "Среда (Docker Compose) — prod-like:"
	@echo "  make up                — поднять все сервисы (включая web + caddy)"
	@echo "  make down              — остановить все сервисы"
	@echo "  make restart           — перезапустить все"
	@echo "  make logs              — следить за логами"
	@echo "  make ps                — статус контейнеров"
	@echo ""
	@echo "Dev-режим (без web и caddy, фронт через npm run dev):"
	@echo "  make dev-up            — поднять postgres+redis+api+worker+scheduler"
	@echo "  make dev-down          — остановить"
	@echo "  make dev-logs          — логи"
	@echo "  make dev-restart       — перезапуск"
	@echo "  make dev-build         — пересобрать api/worker/scheduler"
	@echo "  make web-dev           — параллельно: фронт через Vite на :5173"
	@echo ""
	@echo "Сборка:"
	@echo "  make build             — собрать все Docker-образы"
	@echo "  make build-api         — собрать только api"
	@echo "  make build-worker      — собрать только worker"
	@echo "  make build-scheduler   — собрать только scheduler"
	@echo "  make build-web         — собрать только web"
	@echo ""
	@echo "Миграции (через docker compose run migrate):"
	@echo "  make migrate           — применить все миграции (up)"
	@echo "  make migrate-down      — откатить одну миграцию"
	@echo "  make migrate-create NAME=foo  — создать новую миграцию"
	@echo ""
	@echo "Backend (Go) — локально:"
	@echo "  make tidy              — go mod tidy"
	@echo "  make vet               — go vet ./..."
	@echo "  make lint              — go build + go vet"
	@echo ""
	@echo "Frontend (SvelteKit) — локально:"
	@echo "  make web-install       — pnpm install"
	@echo "  make web-dev           — pnpm dev"
	@echo "  make web-build         — pnpm build"
	@echo ""
	@echo "Прочее:"
	@echo "  make seed              — загрузить демо-данные (спринт 1 день 4)"
	@echo "  make clean             — удалить volumes и build-артефакты"

# ===== Docker Compose =====
up:
	docker compose up -d --build
	@echo "API:    http://localhost:8080/healthz"
	@echo "Web:    http://localhost:3000"
	@echo "Caddy:  http://localhost (proxies api+web)"

down:
	docker compose down

restart: down up

logs:
	docker compose logs -f --tail=100

ps:
	docker compose ps

build:
	docker compose build

build-api:
	docker compose build api

build-worker:
	docker compose build worker

build-scheduler:
	docker compose build scheduler

build-web:
	docker compose build web


DEV_COMPOSE := docker compose -f docker-compose.dev.yml

dev-up:
	$(DEV_COMPOSE) up -d --build
	@echo ""
	@echo "API:      http://localhost:8080/healthz"
	@echo "Swagger:  http://localhost:8080/swagger"
	@echo "Postgres: localhost:5432  (user/db: worktimesync)"
	@echo "Redis:    localhost:6379"
	@echo ""
	@echo "Запустить фронт локально:"
	@echo "  make web-dev"

dev-down:
	$(DEV_COMPOSE) down

dev-logs:
	$(DEV_COMPOSE) logs -f --tail=100

dev-ps:
	$(DEV_COMPOSE) ps

dev-restart: dev-down dev-up

dev-build:
	$(DEV_COMPOSE) build

# ===== Миграции =====
migrate:
	docker compose run --rm migrate -path=/migrations \
		-database="postgres://$${POSTGRES_USER:-worktimesync}:$${POSTGRES_PASSWORD:-worktimesync_dev_pass}@postgres:5432/$${POSTGRES_DB:-worktimesync}?sslmode=disable" \
		up

migrate-down:
	docker compose run --rm migrate -path=/migrations \
		-database="postgres://$${POSTGRES_USER:-worktimesync}:$${POSTGRES_PASSWORD:-worktimesync_dev_pass}@postgres:5432/$${POSTGRES_DB:-worktimesync}?sslmode=disable" \
		down 1

migrate-create:
	@if [ -z "$(NAME)" ]; then echo "usage: make migrate-create NAME=add_some_table"; exit 1; fi
	@n=$$(printf "%04d" $$(( $$(ls migrations/*.up.sql 2>/dev/null | wc -l) + 1 ))); \
	touch migrations/$${n}_$(NAME).up.sql migrations/$${n}_$(NAME).down.sql; \
	echo "created migrations/$${n}_$(NAME).{up,down}.sql"

# ===== Backend (Go) =====
tidy:
	go mod tidy

vet:
	go vet ./...

lint: vet
	go build ./...


web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build


seed:
	@echo "seed запускается автоматически при старте api (internal/bootstrap)."
	@echo "Креды admin'a выводятся в логи api при первом старте — make dev-logs."

clean:
	docker compose down -v
	rm -rf web/.svelte-kit web/build
	@echo "очищено: volumes, web/.svelte-kit, web/build"
