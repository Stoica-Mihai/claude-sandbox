# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions. It includes a web dashboard for managing and interacting with Claude Code sessions from a browser. Sessions are created and managed **only** from the dashboard — direct CLI `claude` is disabled inside the container.

## Architecture

Four services, each its own container:

- **`sessiond/`** — Session host daemon (Go). Owns every claude process end-to-end: spawns it under a directly-owned PTY (`creack/pty`), mirrors all output into a per-session `charmbracelet/x/vt` emulator (2000-line scrollback) for the session's whole life, and serves a framed protocol over unix sockets on a shared volume — `control.sock` for spawn/list/kill, one `<name>.sock` per session for attach streams. No dtach anywhere. **Sessions survive backend rebuilds** (they end only when the sessions container itself is rebuilt/restarted).
- **`backend/`** — API bridge (Go). HTTP API (list/spawn/kill/history/settings/prefs/upload), SSE, and the WebSocket terminal endpoint, all delegating session work to sessiond over the control socket; each WS connection is bridged 1:1 to a session socket. Holds no PTYs and no terminal state. Listens on `:8081` (`BACKEND_PORT`), no exposed host ports — reachable only on the internal compose network.
- **`frontend/`** — Dashboard UI (Go; HTMX templates + a self-contained Futurism stylesheet, no CSS-framework CDN) and a reverse proxy to the backend for `/api`, `/events` (SSE), and `/ws` (WebSocket), plus `/api/share/*` to the holesail sidecar. Listens on `DASHBOARD_PORT` (default 8080), the only published port. `BACKEND_URL=http://backend:8081`, `HOLESAIL_URL=http://holesail:9000`.
- **`holesail/`** — Share-tunnel sidecar (Node wrapper around the `holesail` npm package). Owns at most one secure Holesail P2P tunnel targeting `frontend:8080`; control API on `:9000`, internal network only. Key persisted in the `holesail-share-key` volume (the secure connection string is `hs://s000<key>`, so it survives restarts); boots private always.

Supporting files:

- **Dockerfile.sessions** — Multi-stage. Builder compiles sessiond; runtime is the heavy session environment — Debian with bash, git, curl, bubblewrap, qrencode, npm, Go, gcc, uv, OpenSpec, and Claude Code + plugins. Non-root user `claude`. Rebuilding it ends running sessions, so it only watches `sessiond/`.
- **Dockerfile.backend** — Multi-stage; slim runtime (binary + curl + ca-certificates, same UID/GID user as sessions so the shared socket/upload volumes work). No claude, no dtach, no Go.
- **Dockerfile.frontend** — Multi-stage; minimal runtime (no tmux/socat/claude), just the frontend binary.
- **Dockerfile.holesail** — Multi-stage Node; builder runs `npm ci` against the committed lockfile (the supply-chain boundary), runtime is `node:22-bookworm-slim` + curl, non-root `node` user.
- **docker-compose.yml** — Defines `sessions`, `backend`, `frontend`, and `holesail`. Mounts the workspace at `/workspace` and the scoped Claude config dir (`~/.claude-sandbox`) into **both** sessions (claude auth + cwd) and backend (index, settings editor, transcripts, directory picker); `container-settings.json` is injected **read-write** into both (editor writes, entrypoint seeds). Named volumes: `claude-sock` (sessiond sockets, shared sessions↔backend), `claude-uploads` (image paste: backend writes, claude reads), `holesail-share-key`. Does NOT mount the host's real `~/.claude` / `~/.claude.json`. IPv6 is disabled in the sessions container (`sysctls`) so claude's dual-stack startup fetches don't stall; seccomp is unconfined there for bubblewrap. Watch rules: `sessiond/` rebuilds sessions (kills sessions, rare); `backend/`+`shared/`+`sessiond/` rebuild backend (**sessions keep running**); `frontend/`+`shared/` rebuild frontend. Has healthchecks (`sessiond -ping` for sessions), log rotation, resource limits.
- **container-settings.json** — Claude Code settings for the container (plugins, MCP servers, permissions). Gitignored; seeded from `container-settings.example.json` by `generate-env.sh` (the `setup` target) on first run. Mounted read-write and copied by the entrypoint into the scoped config dir's `settings.json`. Editable locally or via the dashboard **Settings editor** (gear icon → `GET`/`PUT /api/settings`, prefs subset only).
- **entrypoint.sh** — Sessions-container entrypoint; on first run seeds the scoped config dir (`$CLAUDE_CONFIG_DIR`) with the image's baked plugins + registration, and refreshes `settings.json` from `container-settings.json`, then execs sessiond.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets (`up`, `down`, `shell`, `watch`, `build`, `rebuild`, `restart-sessions`, `restart-backend`, `restart-frontend`, `restart-holesail`).
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID, and seeds `container-settings.json` from `container-settings.example.json` if missing.

## Sessions (sessiond)

sessiond owns each claude process directly — no detach layer. The PTY and the terminal state live together in the sessions container for the session's whole life, so nothing is ever reconstructed by nudging the program to repaint.

- **Spawn** (`sessiond/registry.go`): the backend validates cwd (under `/workspace`) + uuid and sends a SPAWN op; sessiond runs `claude --session-id <uuid> --dangerously-skip-permissions` (or `--resume <uuid>`) under `pty.Start` with `TERM=xterm-256color`. `--session-id` makes the dashboard own the conversation id. No PID/meta sidecar files exist — sessiond holds live child handles; boot is a clean slate that unlinks stale sockets.
- **Session actor** (`sessiond/session.go`): one actor goroutine per session owns PTY, emulator, and viewers — `go test -race` clean. Every output chunk feeds the emulator and broadcasts to non-suspended viewers (per-viewer bounded queues, evict-on-full). PTY input writes carry a deadline so a program that stops reading stdin can't freeze the actor.
- **Terminal state** (`sessiond/termstate.go`): `charmbracelet/x/vt` emulator, 2000-line scrollback. A joining viewer gets a rendered snapshot — reset, scrollback scrolled out of the viewport, alt-screen re-enter, screen painted at absolute positions, **tracked DEC modes re-asserted** (bracketed paste ?2004, mouse ?1000/?1002/?1003/?1006, focus ?1004, app cursor keys ?1 — via `vt.Callbacks.EnableMode/DisableMode`), cursor position/visibility. The emulator query-response drain + close sentinel dance is unchanged (library races Close with Read).
- **Protocol** (`sessiond/protocol/`): length-prefixed frames (`type u8`, `len u32`) — DATA (PTY bytes), CONTROL (JSON, mirrors the WS text contract), SNAPSHOT, CLOSE (`{reason}`), ATTACH (`{cols,rows}` handshake — dims mandatory so the snapshot renders at the viewer's size), REQUEST/RESPONSE (control ops: spawn/list/kill/ping, one exchange per connection).
- **Storage**: sockets under `$CLAUDE_SOCK_DIR` (default `/home/claude/.local/state/claude/sock`, mode 0700) on the `claude-sock` named volume shared with the backend.
- **Resume / session index** (`backend/sessionindex.go`): a persisted `uuid → {cwd, created, name}` index at `$CLAUDE_CONFIG_DIR/dashboard-sessions.json` (host-mounted, survives container restarts) drives the per-folder resume list and custom names — backend-owned. Resume asks sessiond to spawn `claude --resume <uuid>` in the recorded cwd (live conversations return their existing session instead). `GET /api/sessions/history?cwd=` lists a folder's entries; rename (`PUT /api/sessions/{id}/name`) sets `index[uuid].name`. **Delete** (`DELETE /api/sessions/history/{uuid}`, keyed by conversation uuid — distinct from the kill route) permanently removes a conversation: unknown uuid errors first, a matching live session is killed, then the index entry and `projects/*/<uuid>.jsonl` transcript(s) are dropped.
- **Kill**: a control op; sessiond signals the process group (`SIGTERM`, then `SIGKILL` after a grace period) and viewers get a CLOSE frame → WS code 1000.
- **Persistence**: claude processes are sessiond's children in the sessions container, so **sessions survive backend rebuilds/restarts** (`make watch` on backend code included) — viewers reconnect and repaint from an exact snapshot with scrollback intact. Sessions end when the sessions container is rebuilt or restarted.
- **Backend store** (`backend/session.go`): in-memory view fed by spawn replies + a 5s LIST reconciliation (catches `/exit`); SSE publishes on change.
- **CLI disabled**: the `claude` shell function in `.bashrc` (and `make claude`) print a message pointing to the dashboard and exit non-zero. The dashboard spawns via sessiond, so spawning is unaffected.

## WS bridge (`backend/handlers.go`)

Each terminal WebSocket is bridged 1:1 to the session's sessiond socket; the backend holds no session state.

- WS BinaryMessage ↔ DATA frames; WS resize TextMessage → the first becomes the ATTACH handshake (binary input arriving earlier is buffered briefly), later ones forward as CONTROL frames verbatim.
- SNAPSHOT/DATA frames → WS binary; CONTROL frames (e.g. `{"type":"deactivated"}`) → WS text; CLOSE frame → WS close 1000 ("[Session ended]"); a dial failure or abrupt stream end closes abnormally so the client's backoff reconnect engages.

## Key implementation details

- Terminal input is sent as WebSocket BinaryMessage (TextMessage is reserved for JSON control like resize and deactivation).
- Multi-viewer resize: each viewer's dimensions are tracked independently in sessiond; the most recent viewer to join or type is the active one and its size wins. ATTACH carries the dims, so the snapshot always renders at the joining viewer's size (a wider snapshot would wrap on a narrower terminal). Non-active viewers are suspended (broadcast skips them) so they freeze rather than garble; a `{"type":"deactivated"}` message tells the client to `term.clear()` on next input. The active-viewer slot is only ever assigned to a currently-registered viewer (a stale resize racing an eviction can't steer the PTY). A per-viewer writer goroutine serializes frame writes.
- WebSocket auto-reconnect: on abnormal close (code != 1000) the client retries with exponential backoff (1s→…→30s cap, 10 max); normal close (1000) shows "[Session ended]".
- Copy-on-select: xterm has no built-in `copyOnSelect`, so `terminal.js` copies the selection on `mouseup` via the async Clipboard API (with an `execCommand` fallback).
- `GET /healthz` returns `{"status":"ok"}`; Docker healthchecks probe it.
- CSS font rules must use the `body` selector, NOT `*` — the universal selector breaks xterm.js character grid measurement.
- The dashboard uses the self-contained **Futurism** design system (square corners, 2px ink borders, solid offset shadows, single `--accent`, Helvetica Neue UI font) — no Tailwind/DaisyUI/Google-Fonts CDNs. CSS is split in two layers: **`futurism.css`** is the kit (tokens + generic components) copied **verbatim** from the `futurism-design` skill — never hand-edit it; **`app.css`** holds all dashboard-specific components plus an override ledger for every intentional divergence from the kit, and loads after it. A skill update = replace `futurism.css` wholesale. Theme is a binary light/dark toggle (`theme.js`, `data-theme`/`data-theme-base`) kept in the header; the accent picker (7 colors, overrides `--accent` + `--shadow` in dark) is an inline swatch row in the settings modal's Appearance category. Both **sync across devices** server-side via `GET/PUT /api/ui-prefs` (`backend/uiprefs.go` → `dashboard-ui.json` in the persistent config dir; accent/theme validated against allowlists). localStorage stays the instant-paint cache to avoid a flash; the server is the source of truth, reconciled on load (`loadUIPrefs`) and written on every user change (`saveUIPrefs`). Single-tenant dashboard, so last write wins. These are dashboard chrome, deliberately kept out of `container-settings.json` (Claude runtime config).
- **Settings editor** (`settings.js` + `backend/settings.go`): a header gear opens a **categorized** Futurism modal (left nav: Session / Appearance / Sharing — `settingsSelectCategory`). Only **Session** persists via the footer SAVE (Appearance and Sharing act instantly, so SAVE + hint show for Session only). Session edits a whitelisted prefs subset of `container-settings.json` — `model`, `effortLevel` (low/medium/high/xhigh/max), `alwaysThinkingEnabled`, `language`, `advisorModel`. `GET/PUT /api/settings` validates + merges (preserving plugins/hooks/env), writes the source file, and refreshes `settings.json`; changes apply to **new** sessions. `advisorModel` takes a canonical id (e.g. `claude-opus-4-8`); the advisor itself only runs when `CLAUDE_CODE_ENABLE_EXPERIMENTAL_ADVISOR_TOOL=1` is set (in `container-settings.json`'s `env`), since the `/advisor` feature is gated off on this install.
- **New project from the picker**: the NEW SESSION modal's directory picker has a "+ NEW PROJECT…" row (every browse depth) that swaps in place for an inline editor — name input, `git init` checkbox (default on). `POST /api/directories {path, name, gitInit}` validates the name (`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, single segment), prefix-checks the parent under `/workspace` like the GET handler, `os.Mkdir` 0755, then optional `git init`; 400/409/500 render inline in the editor, git-init failure keeps the folder (201 + warning → kit toast). On 201 the client sets the spawn form's `cwd` to the new folder and submits it — session spawns and the terminal tab opens directly (nothing to resume in a fresh folder). Route mirrored in the frontend proxy; wire types in `shared/`.
- Session cards show a `DisplayName` (custom name or dir basename), CWD, and a live-ticking duration. Rename via `PUT /api/sessions/{terminalId}/name` (custom names persist in the session index, keyed by conversation uuid).
- **Share tunnel** (`share.js` + `holesail/server.js` + `frontend/shareguard.go`): the settings modal's **Sharing** category toggles a secure Holesail P2P tunnel to the dashboard. When public, the whole app carries a `sharing-public` body class so the header logo mark glows (the ambient "you're exposed" cue — ledger D17 — in place of a dedicated glyph). Routes: `GET /api/share/status`, `POST /api/share/start|stop|regenerate` — the frontend proxies the whole `/api/share/` prefix (method-blind, so no variant can fall through to the backend catch-all) verbatim to the sidecar via a `httputil.ReverseProxy`. The **tunnel-origin guard** 403s mutating share routes for tunnel visitors: the frontend runs a second, compose-internal listener (`TUNNEL_PORT`, default 8090) that stamps every request as tunnel-originated, and the holesail relay targets that port — tunnel origin is a property of the socket (topology), not a runtime IP/DNS heuristic. Every other route works over the tunnel (the WS origin check passes — Origin == Host end-to-end). The share-status JSON envelope is the `ShareStatus`/`ShareState` contract in `shared/types.go` (mirrored by `server.js`). The QR (vendored `qrcode-generator`, no CDN) paints fixed ink-on-paper hexes in both themes (ledger D16). No SSE for share state: status is fetched on page load and after each action. **The connection string grants full dashboard access** — the modal says so; regenerate is the kill switch. A host reboot boots the sidecar private (intended).

### Frontend assets (`frontend/web/`, `go:embed`)
- `templates/layout.html`, `templates/fragments/{sessions,directory-picker}.html`
- **Native ES modules** (no bundler/build step): the browser loads a single `<script type="module" src="/static/js/main.js">`. `main.js` is the entry — it imports each module's `init`, installs the delegated event dispatcher, and calls the inits on `DOMContentLoaded`. Modules use relative `import`/`export`; they have **no side effects at import time** (every listener/DOM touch lives in an exported `init()`, which also resets module-level state so tests get a clean slate). Templates carry **no inline `onclick`** — `actions.js` (`register(name, handler)` + `initActions()`) installs one delegated `click` listener that resolves `data-action` via `closest()`; each module registers its actions in `init()`. The in-`<head>` pre-paint theme script and the `window.ACCENTS`/`window.NEW_PROJECT_NAME_PATTERN` injection stay classic inline scripts. Modules: `actions.js` (delegation), `store.js` (client session store — parses the `#session-data` JSON embedded in the sessions fragment on load and after every sidebar swap; tabs/badge render from its `subscribe`, never by scraping card markup), `terminal.js` (xterm.js 6.0 manager/coordinator) with its extracted concerns `session-socket.js` (the `SessionSocket` WebSocket state machine — connecting/open/reconnecting/ended/lost/closed, backoff reconnect, manual `retry()` from lost on next keypress; all session I/O goes through `send()`/`sendResize()`, nothing touches the raw `ws`), `terminal-clipboard.js` (copy-on-select + image paste), `terminal-touch.js` (mobile momentum scroll), `terminal-theme.js` (xterm themes; re-exported through `terminal.js`), and `terminal-ansi.js` (status colors); plus `ui-utils.js`, `sidebar.js`, `tabs.js`, `mobile-bar.js`, `picker.js`, `history-del.js`, `rename.js`, `app-init.js`, `theme.js`, `settings.js`, `share.js`, and the `main.js` entry.
- `static/css/futurism.css` (vendored kit — verbatim) + `static/css/app.css` (app components + override ledger + responsive), `static/vendor/` (htmx, htmx-ext-sse, xterm.js 6.0 + fit/web-links/webgl addons, qrcode-generator)

## Common Commands

```bash
make up               # Auto-generate .env, build and start all services
make watch            # docker compose watch (rebuild/sync on source change)
make shell            # Open a bash shell in the sessions container
make down             # Stop the containers
make restart-sessions # Rebuild + restart the session host (ENDS running sessions)
make restart-backend  # Rebuild + restart just the backend (sessions keep running)
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

- **Scoped auth/config:** the host directory `~/.claude-sandbox` is bind-mounted into the sessions and backend containers at `/home/claude/.claude-sandbox` (the in-container `claude` user's home) and set as `$CLAUDE_CONFIG_DIR`. All Claude state (auth, sessions, projects, config) lives there — on the host it's just `~/.claude-sandbox`, isolated from the host's real `~/.claude`. There is no second host user; `/home/claude` exists only inside the container. On first run authenticate inside a dashboard session (OAuth URL → paste code); the token persists in `~/.claude-sandbox` for this sandbox only. To refresh pre-installed plugins after an image rebuild, delete `~/.claude-sandbox` (it re-seeds on next start).
- The containers create a non-root user `claude` with the host UID/GID (from `.env`) to avoid mounted-volume permission issues.
- Tokens in `mcp-config.json` and `container-settings.json` are plaintext — do not commit these to a shared remote.
- The dashboard sits behind an external auth proxy — no authentication is built in.
