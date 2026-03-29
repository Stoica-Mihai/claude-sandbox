# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions. It includes a web dashboard for managing and interacting with Claude Code sessions from a browser.

## Architecture

- **Dockerfile** — Multi-stage build. The `builder` stage compiles the Go dashboard binary. The runtime stage is Debian-based with bash, git, curl, socat, tmux, bubblewrap, Go, gcc, uv (Python package manager), OpenSpec, and Claude Code + plugins pre-installed. Runs as non-root user `claude`.
- **docker-compose.yml** — Defines the `claude-env` service. Maps host Claude session/auth files into the container and injects container-specific settings and MCP config as read-only volumes. Mounts the project workspace at `/workspace`. Dashboard port is configurable via `DASHBOARD_PORT` env var (default 8080).
- **docker-compose.dev.yml** — Development override for dashboard iteration. Mounts `./dashboard/` source into the container and compiles on startup. Use via `make dev`. Not auto-merged — must be invoked explicitly.
- **container-settings.json** — Claude Code settings for the container environment (plugins, MCP servers, permissions). Mounted read-only at `/home/claude/.claude/settings.json`.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets for build, start, stop, and shell access.
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID.
- **.dockerignore** — Excludes non-build files from the Docker context.

## Dashboard (`dashboard/`)

A Go web server serving an HTMX + Tailwind/DaisyUI dashboard for managing Claude Code sessions.

- **`dashboard/main.go`** — Entry point, HTTP server on `:8080`, graceful shutdown.
- **`dashboard/session.go`** — Session manager: tmux-based session lifecycle (spawn, kill, discover via `tmux list-sessions`), cached session list with background polling.
- **`dashboard/handlers.go`** — HTTP handlers, SSE streaming, WebSocket terminal relay via ephemeral `tmux attach` PTY per connection.
- **`dashboard/broker.go`** — SSE pub/sub event broker.
- **`dashboard/web/`** — Embedded templates and static assets via `go:embed`.
  - `templates/layout.html` — Full page with sidebar, tabbed terminal view, responsive mobile layout.
  - `templates/fragments/sessions.html` — Session list HTMX fragment.
  - `templates/fragments/directory-picker.html` — Directory browser HTMX fragment.
  - `static/js/terminal.js` — xterm.js 6.0 manager with WebSocket relay, WebGL addon, clipboard image paste.
  - `static/js/views.js` — View mode switching, tab management, mobile sidebar drawer.
  - `static/js/theme.js` — 10-theme switcher with localStorage persistence.
  - `static/css/style.css` — Custom styles.
  - `static/vendor/` — Vendored htmx.min.js, xterm.js 6.0, and addons (fit, web-links, webgl).

### Key implementation details

- All sessions run inside tmux. The dashboard spawns `tmux new-session -d -s claude-<hex>` and discovers sessions via `tmux list-sessions -F`. There is no managed vs external distinction — all tmux sessions with the `claude-` prefix are equal.
- The dashboard uses a custom relay (`relay.go`) instead of `tmux attach` for the viewer path. Each session gets a unix socket relay via `tmux pipe-pane` + socat for bidirectional I/O. This bypasses tmux's terminal emulation layer, giving xterm.js native control over mouse events (selection, scroll).
- The relay tracks alternate screen state: strips `\x1b[?1049h/l` from viewer output (xterm.js stays in normal mode), routes TUI-mode output to viewers only (not ring buffer), routes normal-mode output to both viewers and the 1MB ring buffer. On reconnect, the ring buffer is replayed for clean history without TUI artifacts.
- Terminal input is sent as WebSocket BinaryMessage (TextMessage is reserved for JSON control like resize and deactivation). Input goes to the pane via the socat unix socket — no process spawning per keystroke.
- Multi-viewer resize: each viewer's terminal dimensions are tracked independently. When a viewer sends input and isn't the active viewer, `ResizeToViewer` resizes tmux to that viewer's dimensions (mimics tmux `window-size latest`). Non-active viewers are suspended (broadcast skips them via `atomic.Bool`) so they see frozen — not garbled — display. A `{"type":"deactivated"}` text message tells the client to `term.clear()` on next input. Per-connection `writeMu` serializes all WebSocket writes to prevent gorilla/websocket concurrent-write corruption.
- WebSocket auto-reconnect: on abnormal close (code != 1000), the client retries with exponential backoff (1s→2s→4s...→30s cap, 10 max). Shows "[Reconnecting... (attempt N)]" inline. On success, scrollback replays via `AddViewer`. Normal close (1000) shows "[Session ended]" with no retry.
- `GET /healthz` returns `{"status":"ok"}`. Docker healthcheck probes it every 10s so `restart: unless-stopped` can detect hangs.
- CSS font rules must use `body` selector, NOT `*` — the universal selector breaks xterm.js character grid measurement.
- The `claude` shell function (in `.bashrc`) wraps every CLI invocation in a tmux session, making it visible in the dashboard. Uses `command -v claude` to resolve the binary path and `TMUX=` to avoid nesting warnings.
- `tmux.conf` sets `mouse off` (xterm.js handles mouse natively), `window-size latest`, and `smcup@:rmcup@` (for CLI `tmux attach` users).
- A background polling goroutine checks `tmux list-sessions` every 5 seconds, syncs relays for new/gone sessions, and publishes SSE events on every tick (so session card durations and activity state stay fresh).
- Session cards show a `DisplayName` (custom name or directory basename), CWD as secondary text, and a live-ticking duration (client-side `setInterval` from `data-created` timestamp). A rename button (✏) opens a DaisyUI modal (bottom sheet on mobile, centered on desktop) to set a custom name via `PUT /api/sessions/{terminalId}/name`. Names are in-memory only — lost on restart.

## Common Commands

```bash
make up       # Auto-generate .env, build and start the container
make dev      # Start with dashboard source mounted (recompiles on restart, no image rebuild)
make shell    # Open a bash shell in the container
make claude   # Run Claude Code in the container
make down     # Stop the container
```

The `claude` shell function (defined in Dockerfile) wraps `claude --dangerously-skip-permissions` inside a tmux session — every invocation is visible in the dashboard. The container itself is the sandbox.

The dashboard is available at `http://localhost:8080` after `make up` (or the port set by `DASHBOARD_PORT` in `.env`).

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
