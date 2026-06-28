.PHONY: setup build rebuild up down shell claude restart-frontend restart-backend watch

setup:
	@./generate-env.sh

build: setup
	docker compose build

rebuild: setup
	docker compose build --no-cache

up: setup
	docker compose up -d --build

down:
	docker compose down

restart-frontend:
	docker compose up -d --build frontend

restart-backend:
	docker compose up -d --build backend

watch:
	docker compose watch

shell:
	docker exec -it claude_backend bash

claude:
	@echo "Direct CLI claude is disabled — sessions are created from the dashboard:"
	@echo "  http://localhost:$${DASHBOARD_PORT:-8080}"
