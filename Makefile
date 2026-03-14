.PHONY: setup build up down shell claude

setup:
	@./generate-env.sh

build: setup
	docker compose build

up: setup
	docker compose up -d --build

down:
	docker compose down

shell:
	docker exec -it claude_workspace bash

claude:
	docker exec -it claude_workspace claude
