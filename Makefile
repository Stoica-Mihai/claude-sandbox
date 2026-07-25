.PHONY: setup build rebuild up down shell claude watch ssh-keygen

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

# Generate a dedicated SSH key for in-container `git push` (gitignored ./ssh,
# mounted at ~/.ssh). Add the printed PUBLIC key to GitHub — a per-repo Deploy
# key (write access) is safest; avoid reusing your personal key.
ssh-keygen:
	@mkdir -p ssh && chmod 700 ssh
	@if [ -f ssh/id_ed25519 ]; then \
		echo "ssh/id_ed25519 already exists — its public key:"; \
	else \
		ssh-keygen -t ed25519 -N '' -C 'claude-sandbox' -f ssh/id_ed25519 >/dev/null && \
		chmod 600 ssh/id_ed25519 && \
		echo "Generated ssh/id_ed25519. Add this PUBLIC key to GitHub (Deploy key preferred):"; \
	fi
	@echo; cat ssh/id_ed25519.pub; echo
	@echo "Then use SSH remotes in your repos (git@github.com:owner/repo.git)."
