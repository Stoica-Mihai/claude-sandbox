# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions. It includes a web dashboard for managing and interacting with Claude Code sessions from a browser. Sessions are created and managed **only** from the dashboard — direct CLI `claude` is disabled inside the container.

## Architecture

Three services, each its own container:

- **`backend/`** — API + session manager (Go). Owns session lifecycle (spawn/kill/discover), the per-session relay, SSE, and the WebSocket terminal endpoint. Listens on `:8081` (`BACKEND_PORT`), no exposed host ports — reachable only on the internal compose network.
- **`frontend/`** — Dashboard UI (Go; HTMX templates + a self-contained Futurism stylesheet, no CSS-framework CDN) and a reverse proxy to the backend for `/api`, `/events` (SSE), and `/ws` (WebSocket), plus `/api/share/*` to the holesail sidecar. Listens on `DASHBOARD_PORT` (default 8080), the only published port. `BACKEND_URL=http://backend:8081`, `HOLESAIL_URL=http://holesail:9000`.
- **`holesail/`** — Share-tunnel sidecar (Node wrapper around the `holesail` npm package). Owns at most one secure Holesail P2P tunnel targeting `frontend:8080`; control API on `:9000`, internal network only. Key persisted in the `holesail-share-key` volume (the secure connection string is `hs://s000<key>`, so it survives restarts); boots private always.

Supporting files:

- **Dockerfile.backend** — Multi-stage. Builder compiles the backend binary; runtime is Debian-based with bash, git, curl, dtach, bubblewrap, qrencode, npm, Go, gcc, uv, OpenSpec, and Claude Code + plugins. Non-root user `claude`.
- **Dockerfile.frontend** — Multi-stage; minimal runtime (no tmux/socat/claude), just the frontend binary.
- **Dockerfile.holesail** — Multi-stage Node; builder runs `npm ci` against the committed lockfile (the supply-chain boundary), runtime is `node:22-bookworm-slim` + curl, non-root `node` user.
- **docker-compose.yml** — Defines `backend` and `frontend` services. Mounts the workspace at `/workspace` and a scoped, host-isolated Claude config dir (`~/.claude-sandbox`) into the backend; injects `container-settings.json` **read-write** (the in-dashboard settings editor persists edits to it). Does NOT mount the host's real `~/.claude` / `~/.claude.json`. Disables IPv6 in the backend (`sysctls: net.ipv6.conf.*.disable_ipv6=1`) so claude's dual-stack startup fetches don't stall on an unroutable IPv6 address. Has healthchecks, log rotation, resource limits.
- **container-settings.json** — Claude Code settings for the container (plugins, MCP servers, permissions). Gitignored; seeded from `container-settings.example.json` by `generate-env.sh` (the `setup` target) on first run. Mounted read-write and copied by the entrypoint into the scoped config dir's `settings.json`. Editable locally or via the dashboard **Settings editor** (gear icon → `GET`/`PUT /api/settings`, prefs subset only).
- **entrypoint.sh** — Backend entrypoint; on first run seeds the scoped config dir (`$CLAUDE_CONFIG_DIR`) with the image's baked plugins + registration, and refreshes `settings.json` from `container-settings.json`.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets (`up`, `down`, `shell`, `watch`, `build`, `rebuild`, `restart-backend`, `restart-frontend`, `restart-holesail`).
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID, and seeds `container-settings.json` from `container-settings.example.json` if missing.

## Sessions (dtach)

Sessions are persisted with **dtach** (a thin detach layer — no terminal emulation), not tmux. This keeps the raw byte stream intact so xterm.js handles mouse selection/copy natively.

- **Spawn** (`backend/session.go`): generates a UUIDv4 and runs `dtach -n <sock> -E -z bash -c 'echo $$ > <pid>; exec claude --session-id <uuid> --dangerously-skip-permissions'`. Passing `--session-id` makes the dashboard own the claude conversation id (no parsing claude's transcript format). The inner bash writes the claude PID to a sidecar (for kill); the spawner writes a JSON metadata sidecar (cwd, created, session_id). Working dir must be under `/workspace`.
- **Storage**: sockets and metadata live under `$CLAUDE_SOCK_DIR` / `$CLAUDE_META_DIR` (default `/home/claude/.local/state/claude/{sock,meta}`, mode 0700) — not world-readable `/tmp`.
- **Resume / session index** (`backend/sessionindex.go`): a persisted `uuid → {cwd, created, name}` index at `$CLAUDE_CONFIG_DIR/dashboard-sessions.json` (host-mounted, survives container restarts) drives the per-folder resume list and custom names. Resume spawns a dtach session running `claude --resume <uuid>` in the recorded cwd. `GET /api/sessions/history?cwd=` lists a folder's entries; rename (`PUT /api/sessions/{id}/name`) sets `index[uuid].name` (resolved from the live session's `session_id`), so names persist and show in both the sidebar and the resume list. **Delete** (`DELETE /api/sessions/history/{uuid}`, keyed by conversation uuid — distinct from the kill route) permanently removes a conversation: `SessionManager.DeleteHistory` errors on unknown uuid, kills the live session first if one matches, then drops the index entry and deletes the `projects/*/<uuid>.jsonl` transcript(s). Exposed as an inline two-step confirm on each resume-list row. Note: the frontend is a per-route proxy, so this route is mirrored in both `backend/handlers.go` and `frontend/handlers.go`.
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
- The dashboard uses the self-contained **Futurism** design system (square corners, 2px ink borders, solid offset shadows, single `--accent`, Helvetica Neue UI font) — no Tailwind/DaisyUI/Google-Fonts CDNs. CSS is split in two layers: **`futurism.css`** is the kit (tokens + generic components) copied **verbatim** from the `futurism-design` skill — never hand-edit it; **`app.css`** holds all dashboard-specific components plus an override ledger for every intentional divergence from the kit, and loads after it. A skill update = replace `futurism.css` wholesale. Theme is a binary light/dark toggle (`theme.js`, `data-theme`/`data-theme-base`) kept in the header; the accent picker (7 colors, overrides `--accent` + `--shadow` in dark) is an inline swatch row in the settings modal's Appearance category. Both **sync across devices** server-side via `GET/PUT /api/ui-prefs` (`backend/uiprefs.go` → `dashboard-ui.json` in the persistent config dir; accent/theme validated against allowlists). localStorage stays the instant-paint cache to avoid a flash; the server is the source of truth, reconciled on load (`loadUIPrefs`) and written on every user change (`saveUIPrefs`). Single-tenant dashboard, so last write wins. These are dashboard chrome, deliberately kept out of `container-settings.json` (Claude runtime config).
- **Settings editor** (`settings.js` + `backend/settings.go`): a header gear opens a **categorized** Futurism modal (left nav: Session / Appearance / Sharing — `settingsSelectCategory`). Only **Session** persists via the footer SAVE (Appearance and Sharing act instantly, so SAVE + hint show for Session only). Session edits a whitelisted prefs subset of `container-settings.json` — `model`, `effortLevel` (low/medium/high/xhigh/max), `alwaysThinkingEnabled`, `language`, `advisorModel`. `GET/PUT /api/settings` validates + merges (preserving plugins/hooks/env), writes the source file, and refreshes `settings.json`; changes apply to **new** sessions. `advisorModel` takes a canonical id (e.g. `claude-opus-4-8`); the advisor itself only runs when `CLAUDE_CODE_ENABLE_EXPERIMENTAL_ADVISOR_TOOL=1` is set (in `container-settings.json`'s `env`), since the `/advisor` feature is gated off on this install.
- **New project from the picker**: the NEW SESSION modal's directory picker has a "+ NEW PROJECT…" row (every browse depth) that swaps in place for an inline editor — name input, `git init` checkbox (default on). `POST /api/directories {path, name, gitInit}` validates the name (`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, single segment), prefix-checks the parent under `/workspace` like the GET handler, `os.Mkdir` 0755, then optional `git init`; 400/409/500 render inline in the editor, git-init failure keeps the folder (201 + warning → kit toast). On 201 the client sets the spawn form's `cwd` to the new folder and submits it — session spawns and the terminal tab opens directly (nothing to resume in a fresh folder). Route mirrored in the frontend proxy; wire types in `shared/`.
- Session cards show a `DisplayName` (custom name or dir basename), CWD, and a live-ticking duration. Rename via `PUT /api/sessions/{terminalId}/name` (custom names persisted to a 0600 file in the metadata dir).
- **Share tunnel** (`share.js` + `holesail/server.js` + `frontend/shareguard.go`): the settings modal's **Sharing** category toggles a secure Holesail P2P tunnel to the dashboard. When public, the whole app carries a `sharing-public` body class so the header logo mark glows (the ambient "you're exposed" cue — ledger D17 — in place of a dedicated glyph). Routes: `GET /api/share/status`, `POST /api/share/start|stop|regenerate` — the frontend proxies them verbatim to the sidecar (which serves the same paths). The **tunnel-origin guard** 403s these routes when `RemoteAddr` is the holesail container (resolved via Docker DNS, 10s cache, one forced re-resolve on a miss, fail-open only before the first successful resolution) so tunnel visitors can't operate the share controls; every other route works over the tunnel (the WS origin check passes — Origin == Host end-to-end). The QR (vendored `qrcode-generator`, no CDN) paints fixed ink-on-paper hexes in both themes (ledger D16). No SSE for share state: status is fetched on page load and after each action. **The connection string grants full dashboard access** — the modal says so; regenerate is the kill switch. A host reboot boots the sidecar private (intended).

### Frontend assets (`frontend/web/`, `go:embed`)
- `templates/layout.html`, `templates/fragments/{sessions,directory-picker}.html`
- **Native ES modules** (no bundler/build step): the browser loads a single `<script type="module" src="/static/js/main.js">`. `main.js` is the entry — it imports each module's `init`, installs the delegated event dispatcher, and calls the inits on `DOMContentLoaded`. Modules use relative `import`/`export`; they have **no side effects at import time** (every listener/DOM touch lives in an exported `init()`, which also resets module-level state so tests get a clean slate). Templates carry **no inline `onclick`** — `actions.js` (`register(name, handler)` + `initActions()`) installs one delegated `click` listener that resolves `data-action` via `closest()`; each module registers its actions in `init()`. The in-`<head>` pre-paint theme script and the `window.ACCENTS`/`window.NEW_PROJECT_NAME_PATTERN` injection stay classic inline scripts. Modules: `actions.js` (delegation), `terminal.js` (xterm.js 6.0 manager/coordinator) with its extracted concerns `terminal-connection.js` (WebSocket relay + reconnect), `terminal-clipboard.js` (copy-on-select + image paste), `terminal-touch.js` (mobile momentum scroll), `terminal-theme.js` (xterm themes; re-exported through `terminal.js`), and `terminal-ansi.js` (status colors); plus `ui-utils.js`, `sidebar.js`, `tabs.js`, `mobile-bar.js`, `picker.js`, `history-del.js`, `rename.js`, `app-init.js`, `theme.js`, `settings.js`, `share.js`, and the `main.js` entry.
- `static/css/futurism.css` (vendored kit — verbatim) + `static/css/app.css` (app components + override ledger + responsive), `static/vendor/` (htmx, htmx-ext-sse, xterm.js 6.0 + fit/web-links/webgl addons, qrcode-generator)

## Common Commands

```bash
make up               # Auto-generate .env, build and start backend + frontend
make watch            # docker compose watch (rebuild/sync on source change)
make shell            # Open a bash shell in the backend container
make down             # Stop the containers
make restart-backend  # Rebuild + restart just the backend
make restart-frontend # Rebuild + restart just the frontend
make restart-holesail # Rebuild + restart just the share-tunnel sidecar
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
