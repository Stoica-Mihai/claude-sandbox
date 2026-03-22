.PHONY: setup build rebuild up up-clean down shell claude

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

down:
	docker compose down

shell:
	docker exec -it claude_workspace bash

claude:
	docker exec -it claude_workspace claude
