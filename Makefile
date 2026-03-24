.PHONY: setup build rebuild up up-clean dev down shell claude

setup:
	@./generate-env.sh

build: setup
	docker compose build

rebuild: setup
	docker compose build --no-cache

up: setup
	docker compose up -d --build

up-clean: rebuild
	docker compose up -d

dev: setup
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build

down:
	docker compose down

shell:
	docker exec -it claude_workspace bash

claude:
	docker exec -it claude_workspace claude
