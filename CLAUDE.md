# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions. It includes a web dashboard for managing and interacting with Claude Code sessions from a browser. Sessions are created and managed **only** from the dashboard — direct CLI `claude` is disabled inside the container.

## Architecture

Two Go services, each its own container:

- **`backend/`** — API + session manager. Owns session lifecycle (spawn/kill/discover), the per-session relay, SSE, and the WebSocket terminal endpoint. Listens on `:8081` (`BACKEND_PORT`), no exposed host ports — reachable only on the internal compose network.
- **`frontend/`** — Dashboard UI (HTMX + Tailwind/DaisyUI templates + static assets) and a reverse proxy to the backend for `/api`, `/events` (SSE), and `/ws` (WebSocket). Listens on `DASHBOARD_PORT` (default 8080), the only published port. `BACKEND_URL=http://backend:8081`.

Supporting files:

- **Dockerfile.backend** — Multi-stage. Builder compiles the backend binary; runtime is Debian-based with bash, git, curl, dtach, bubblewrap, qrencode, npm, Go, gcc, uv, OpenSpec, and Claude Code + plugins. Non-root user `claude`.
- **Dockerfile.frontend** — Multi-stage; minimal runtime (no tmux/socat/claude), just the frontend binary.
- **docker-compose.yml** — Defines `backend` and `frontend` services. Mounts the workspace at `/workspace` and a scoped, host-isolated Claude config dir (`~/.claude-sandbox`) into the backend; injects `container-settings.json` read-only. Does NOT mount the host's real `~/.claude` / `~/.claude.json`. Has healthchecks, log rotation, resource limits.
- **container-settings.json** — Claude Code settings for the container (plugins, MCP servers, permissions). Mounted read-only and copied by the entrypoint into the scoped config dir's `settings.json`.
- **entrypoint.sh** — Backend entrypoint; on first run seeds the scoped config dir (`$CLAUDE_CONFIG_DIR`) with the image's baked plugins + registration, and refreshes `settings.json` from `container-settings.json`.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets (`up`, `down`, `shell`, `watch`, `build`, `rebuild`, `restart-backend`, `restart-frontend`).
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID.

## Sessions (dtach)

Sessions are persisted with **dtach** (a thin detach layer — no terminal emulation), not tmux. This keeps the raw byte stream intact so xterm.js handles mouse selection/copy natively.

- **Spawn** (`backend/session.go`): `dtach -n <sock> -E -z bash -c 'echo $$ > <pid>; exec claude --dangerously-skip-permissions'`. The inner bash writes the claude PID to a sidecar (for kill); the spawner writes a JSON metadata sidecar (cwd, created). Working dir must be under `/workspace`.
- **Storage**: sockets and metadata live under `$CLAUDE_SOCK_DIR` / `$CLAUDE_META_DIR` (default `/home/claude/.local/state/claude/{sock,meta}`, mode 0700) — not world-readable `/tmp`.
- **Discovery**: scans the PID sidecars in the metadata dir and probes liveness via `signal 0` on the PID; dead sessions' sockets + sidecars are unlinked. (Keys off the sidecar, not the socket, because dtach removes its own socket on exit.)
- **Kill**: reads the PID sidecar and signals the process group (`SIGTERM`, then `SIGKILL` after a grace period).
- **Persistence**: dtach masters are children of init, independent of the backend process, so sessions survive the backend *process* restarting. They do NOT survive a container restart (the masters live in the backend container).
- **CLI disabled**: the `claude` shell function in `.bashrc` (and `make claude`) print a message pointing to the dashboard and exit non-zero. The dashboard spawns the real binary directly, so spawning is unaffected.

## Relay (`backend/relay.go`)

One relay per session connects the dtach session to WebSocket viewers.

- The relay owns a single `dtach -a <sock> -E -z -r none` attach via a directly-owned PTY (`creack/pty`). It reads the PTY and broadcasts to all viewers; one attach, N viewers.
- Input is written to the attach PTY. Resize calls `pty.Setsize`; the relay imposes a size only when a viewer is present.
- Alternate-screen tracking: detects `\x1b[?1049h/l` (and `?47h/l`), routes normal-mode output to both viewers and the 1MB ring buffer, routes TUI-mode output to viewers only. On connect the ring buffer is replayed for clean history.
- Reconnect: if the attach PTY drops while the session is alive, the relay re-execs `dtach -a`, swaps the PTY under a mutex, and restarts the read loop (generation-guarded). Mutable cross-goroutine state (PTY pointer, activity timestamps, alt-screen flag) is synchronized — `go test -race` is clean.

## Key implementation details

- Terminal input is sent as WebSocket BinaryMessage (TextMessage is reserved for JSON control like resize and deactivation).
- Multi-viewer resize: each viewer's dimensions are tracked independently; the active typist's size wins (`ResizeToViewer`). Non-active viewers are suspended (broadcast skips them via `atomic.Bool`) so they freeze rather than garble; a `{"type":"deactivated"}` message tells the client to `term.clear()` on next input. Per-connection `writeMu` serializes WebSocket writes.
- WebSocket auto-reconnect: on abnormal close (code != 1000) the client retries with exponential backoff (1s→…→30s cap, 10 max); normal close (1000) shows "[Session ended]".
- Copy-on-select: xterm has no built-in `copyOnSelect`, so `terminal.js` copies the selection on `mouseup` via the async Clipboard API (with an `execCommand` fallback).
- `GET /healthz` returns `{"status":"ok"}`; Docker healthchecks probe it.
- CSS font rules must use the `body` selector, NOT `*` — the universal selector breaks xterm.js character grid measurement.
- A background poller re-discovers sessions every 5s, syncs relays, and publishes SSE so card durations/activity stay fresh.
- Session cards show a `DisplayName` (custom name or dir basename), CWD, and a live-ticking duration. Rename via `PUT /api/sessions/{terminalId}/name` (custom names persisted to a 0600 file in the metadata dir).

### Frontend assets (`frontend/web/`, `go:embed`)
- `templates/layout.html`, `templates/fragments/{sessions,directory-picker}.html`
- `static/js/terminal.js` (xterm.js 6.0 manager: WebSocket relay, WebGL addon, clipboard image paste, copy-on-select), `views.js`, `theme.js`
- `static/css/style.css`, `static/vendor/` (htmx, xterm.js 6.0 + fit/web-links/webgl addons)

## Common Commands

```bash
make up               # Auto-generate .env, build and start backend + frontend
make watch            # docker compose watch (rebuild/sync on source change)
make shell            # Open a bash shell in the backend container
make down             # Stop the containers
make restart-backend  # Rebuild + restart just the backend
make restart-frontend # Rebuild + restart just the frontend
```

The dashboard is at `http://localhost:8080` after `make up` (or the port set by `DASHBOARD_PORT`). Create sessions from the dashboard — direct CLI `claude` is disabled.

## MCP Servers

Configured via `mcp-config.json` (gitignored). Copy `mcp-config.example.json` to get started.

## Installed Plugins

Pre-installed in the container:
- `superpowers`, `skill-creator` (from claude-plugins-official)
- `claude-api`, `document-skills`, `example-skills` (from anthropic-agent-skills)
- `cli-anything` (from CLI-Anything)
- `opsx-ext`, `cli-anything-go`, `caveman` (from claude-skills / caveman)

## Notes

- **Scoped auth/config:** the host directory `~/.claude-sandbox` is bind-mounted into the backend container at `/home/claude/.claude-sandbox` (the in-container `claude` user's home) and set as `$CLAUDE_CONFIG_DIR`. All Claude state (auth, sessions, projects, config) lives there — on the host it's just `~/.claude-sandbox`, isolated from the host's real `~/.claude`. There is no second host user; `/home/claude` exists only inside the container. On first run authenticate inside a dashboard session (OAuth URL → paste code); the token persists in `~/.claude-sandbox` for this sandbox only. To refresh pre-installed plugins after an image rebuild, delete `~/.claude-sandbox` (it re-seeds on next start).
- The container creates a non-root user `claude` with the host UID/GID (from `.env`) to avoid mounted-volume permission issues.
- Tokens in `mcp-config.json` and `container-settings.json` are plaintext — do not commit these to a shared remote.
- The dashboard sits behind an external auth proxy — no authentication is built in.
