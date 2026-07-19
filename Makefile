.PHONY: setup build rebuild up down shell claude watch

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

# Not in .PHONY: make skips implicit/pattern-rule search for phony targets.
# --no-deps: restarting one service must not recreate its dependencies
# (restart-backend would otherwise take the sessions container — and every
# running claude session — down with it).
restart-%:
	docker compose up -d --build --no-deps $*

watch:
	docker compose watch

shell:
	docker exec -it claude_sessions bash

claude:
	@echo "Direct CLI claude is disabled — sessions are created from the dashboard:"
	@echo "  http://localhost:$${DASHBOARD_PORT:-8080}"
