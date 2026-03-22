# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions. It includes a web dashboard for managing and interacting with Claude Code sessions from a browser.

## Architecture

- **Dockerfile** — Debian-based image with bash, git, curl, socat, bubblewrap, Go, uv (Python package manager), OpenSpec, and Claude Code + plugins pre-installed. Runs as non-root user `claude`. Builds the dashboard Go binary during image build.
- **docker-compose.yml** — Defines the `claude-env` service. Maps host Claude session/auth files into the container and injects container-specific settings and MCP config as read-only volumes. Mounts the project workspace at `/workspace`. Exposes port 8080 for the dashboard.
- **container-settings.json** — Claude Code settings for the container environment (plugins, MCP servers, permissions). Mounted read-only at `/home/claude/.claude/settings.json`.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets for build, start, stop, and shell access.
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID.
- **.dockerignore** — Excludes non-build files from the Docker context.

## Dashboard (`dashboard/`)

A Go web server serving an HTMX + Tailwind/DaisyUI dashboard for managing Claude Code sessions.

- **`dashboard/main.go`** — Entry point, HTTP server on `:8080`, graceful shutdown.
- **`dashboard/session.go`** — Session manager: PTY spawning via `creack/pty`, session discovery from `~/.claude/sessions/*.json`, scrollback ring buffer for reattach.
- **`dashboard/handlers.go`** — HTTP handlers, SSE streaming, WebSocket terminal relay.
- **`dashboard/broker.go`** — SSE pub/sub event broker.
- **`dashboard/ringbuffer.go`** — Circular byte buffer for terminal scrollback.
- **`dashboard/web/`** — Embedded templates and static assets via `go:embed`.
  - `templates/layout.html` — Full page with sidebar, tabbed terminal view, responsive mobile layout.
  - `templates/fragments/sessions.html` — Session list HTMX fragment.
  - `templates/fragments/directory-picker.html` — Directory browser HTMX fragment.
  - `static/js/terminal.js` — xterm.js manager with WebSocket relay.
  - `static/js/views.js` — View mode switching, tab management, mobile sidebar drawer.
  - `static/js/theme.js` — Dark/light theme toggle.
  - `static/css/style.css` — Custom styles.
  - `static/vendor/` — Vendored htmx.min.js, xterm.js, and addons.

### Key implementation details

- Sessions are discovered globally from `~/.claude/sessions/*.json` (not project-scoped).
- PTYs are spawned with `pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 120})` to avoid cursor positioning issues.
- Terminal input is sent as WebSocket BinaryMessage (TextMessage is reserved for JSON control like resize).
- CSS font rules must use `body` selector, NOT `*` — the universal selector breaks xterm.js character grid measurement.
- On mobile, the xterm textarea is set readonly; a visible input bar handles all input to avoid broken IME composition.
- The `claude` command is called via `exec.LookPath("claude")` with `--dangerously-skip-permissions` flag (shell aliases don't work with `os/exec`).
- `TERM=xterm-256color` is set in spawned process environment.

## Common Commands

```bash
make up       # Auto-generate .env, build and start the container
make shell    # Open a bash shell in the container
make claude   # Run Claude Code in the container
make down     # Stop the container
```

The `claude` alias (defined in Dockerfile) expands to `claude --dangerously-skip-permissions` — the container itself is the sandbox.

The dashboard is available at `http://localhost:8080` after `make up`.

## MCP Servers

MCP servers are configured via `mcp-config.json` (gitignored). Copy `mcp-config.example.json` to get started.

## Installed Plugins

The container comes with these Claude Code plugins pre-installed:
- `superpowers`, `skill-creator` (from claude-plugins-official)
- `claude-api`, `document-skills`, `example-skills` (from anthropic-agent-skills)
- `cli-anything` (from CLI-Anything)
- `opsx-ext`, `cli-anything-go` (from claude-skills)

## Notes

- The container creates a non-root user `claude` with the host UID/GID (from `.env`) to avoid file permission issues with mounted volumes.
- `container-settings.json` enables experimental agent teams (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`).
- Tokens in `mcp-config.json` and `container-settings.json` are plaintext — do not commit these to a shared remote.
- The dashboard sits behind an external auth proxy — no authentication is built in.
