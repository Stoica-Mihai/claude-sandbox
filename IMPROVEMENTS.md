# Claude Sandbox — Improvements & Features Plan

## Context

This project is a Docker-based sandbox for running Claude Code with a Go web dashboard (HTMX + xterm.js). After thorough codebase analysis across all 3 layers (backend Go, frontend JS/HTML, Docker infra), this plan captures every actionable improvement — from race condition fixes to new features to infrastructure hardening.

---

## Phase 1: Backend Bug Fixes (Critical)

### 1.1 Fix race conditions in `backend/relay.go`

**`inAltScreen` (line 69)** — accessed without synchronization from `readLoop` goroutine (trackAltScreen) and HTTP handler goroutines (AddViewer).
- Change `inAltScreen bool` to `inAltScreen atomic.Bool`
- Update all reads (lines 187, 379, 398, 418, 441, 448) to `.Load()`
- Update all writes (lines 422, 424) to `.Store()`

**`lastInputAt` (line 67) and `lastResizeAt` (line 66)** — written from handler goroutines, read from readLoop without lock.
- Protect both with the existing `lastActivityMu`
- In `SendInput` (line 212): lock before writing `lastInputAt`
- In `resizeTmux` (line 293): lock before writing `lastResizeAt`
- In `processOutput` (line 354): snapshot both under single RLock before the `if`

**`socatConn` during reconnect** — `SendInput` can write to closed connection while `reconnect()` is swapping it.
- Add `socatMu sync.Mutex` to Relay struct
- Lock in `SendInput` around nil-check + write
- Lock in `reconnect` around close-old + assign-new

### 1.2 Fix unchecked `rand.Read()` calls

- `backend/session.go:462` — add error check, return error from `generateSessionName()`
- `backend/broker.go:65` — add error check, panic on failure (irrecoverable)

### 1.3 Improve healthcheck

File: `backend/handlers.go:61-64` — currently always returns 200 OK.

Replace with structured health response:
- Check tmux availability (`tmux list-sessions`)
- Report session count, relay count, total viewer count
- Return `{"status":"ok"|"degraded"|"unhealthy", "tmux":{...}, "sessions":{...}, "uptime":"..."}`
- HTTP 200 for ok/degraded, 503 for unhealthy

Add helper methods:
- `session.go`: `Stats() SessionHealth` — relay count, session count, viewer total
- `relay.go`: `ViewerCount() int` — len(viewers) under RLock

---

## Phase 2: Frontend Quick Wins

### 2.1 Vendor external CDN dependencies

**SSE extension** — `layout.html:15` loads from `unpkg.com`. Download and save to `frontend/web/static/vendor/htmx-ext-sse.min.js`. Update script tag to local path.

**DaisyUI + Tailwind** — `layout.html:8-9` load from CDN. Vendor locally:
- Download DaisyUI CSS to `static/vendor/daisyui.min.css`
- Download Tailwind JS to `static/vendor/tailwindcss.min.js`
- Update script/link tags

### 2.2 Tab persistence via sessionStorage

File: `frontend/web/static/js/views.js`

- Add `saveTabs()` — serialize `singleTabs` + `singleTerminalId` to `sessionStorage`
- Call after `openSessionSingle()`, `switchSingleTab()`, `closeSingleTab()`
- Add `restoreTabs()` — read from sessionStorage, validate against live session cards, re-open valid tabs
- Call from `DOMContentLoaded` after initial session list renders (use `setTimeout` 100ms)

### 2.3 Manual reconnect after WebSocket failure

File: `frontend/web/static/js/terminal.js`

After 10 retries (line 512), instead of dead-end `[Connection lost]`:
- Write `[Connection lost — press Enter to reconnect]`
- Register one-shot `term.onData()` handler that resets retryCount and calls `connectWs()`

### 2.4 Session search/filter

- Add text input in sidebar (in `layout.html`, above `#session-list`)
- Add `filterSessions(query)` in `views.js` — filter `.session-card` elements by name/CWD
- Re-apply filter after HTMX swaps (in `htmx:afterSwap` listener)

### 2.5 Connection status indicator

- Add dot + label in header (`layout.html`, near session badge)
- Track SSE state via `htmx:sseOpen` / `htmx:sseError` / `htmx:sseClose` events
- Green = connected, amber = reconnecting, red = disconnected

### 2.6 Full-screen terminal mode

- CSS class `body.fullscreen-terminal` hides header, sidebar, tab bar
- Toggle via `Alt+F` keyboard shortcut
- Exit via `Escape` when in fullscreen
- Call `TerminalManager.resizeAll()` on toggle

### 2.7 Keyboard shortcut help modal

- Add `<dialog>` in `layout.html` listing all shortcuts
- Open via `Alt+?` or `Alt+/`
- Display: Alt+N (new), Alt+W (close), Alt+1-9 (tabs), Alt+F (fullscreen)

### 2.8 Loading indicators

- Directory picker: DaisyUI `loading loading-spinner` as initial content in `#dir-picker`
- Session creation: `hx-disabled-elt="this"` on Launch button + spinner swap
- Image upload: Write `[Uploading image...]` to terminal before fetch

---

## Phase 3: Frontend Optimization

### 3.1 Optimize SSE session list re-render

Every 5s SSE tick replaces entire `#session-list` innerHTML (DOM thrashing).

Solution: Integrate `idiomorph` extension for HTMX:
- Vendor `idiomorph-ext.min.js` locally
- Change `hx-ext="sse"` to `hx-ext="sse,morph"` on body
- Change `hx-swap="innerHTML"` to `hx-swap="morph:innerHTML"` on session list
- Result: only changed DOM nodes update (preserves scroll, hover, transitions)

### 3.2 Accessibility improvements

- Session cards: add `role="button"`, `tabindex="0"`, `aria-label`
- Keyboard nav: Enter/Space activates focused card
- Session badge: add `aria-live="polite"` for screen reader announcements
- Modals: verify native `<dialog>` focus trap, add `autofocus` on first input

---

## Phase 4: New Backend Features

### 4.1 Session stats API

New endpoint: `GET /api/sessions/{terminalId}/stats`

Response: session name, display name, duration, relay status, viewer count, ring buffer usage (bytes/capacity), last activity, alt-screen state.

Add methods:
- `ringbuffer.go`: `Size() int`, `Cap() int`
- `relay.go`: `Stats() RelayStats` — aggregates viewer count, buffer usage, alt-screen state
- `handlers.go`: register route, add handler
- `frontend/handlers.go`: add proxy route

### 4.2 Terminal output download

New endpoint: `GET /api/sessions/{terminalId}/scrollback`

- Read ring buffer bytes via new `relay.Scrollback() []byte` method
- Strip ANSI escapes with regex (`\x1b\[[0-9;]*[a-zA-Z]`)
- Return as `text/plain` with `Content-Disposition: attachment` header
- Frontend: download button in controls bar, `window.open()` to endpoint
- Frontend proxy: add route in `frontend/handlers.go`

### 4.3 Configurable values via environment

New file: `backend/config.go` with `Config` struct + `LoadConfig()`.

Key values to externalize (14 total):
| Value | Current | Env Var |
|-------|---------|---------|
| Ring buffer size | 1MB | `RING_BUFFER_SIZE` |
| Poll interval | 5s | `POLL_INTERVAL` |
| Upload max size | 10MB | `UPLOAD_MAX_SIZE` |
| Reconnect retries | 3 | `RECONNECT_MAX_RETRIES` |
| Workspace root | /workspace | `WORKSPACE_ROOT` |
| Session prefix | claude- | `SESSION_PREFIX` |

Thread `Config` through constructors in `main.go`.

---

## Phase 5: Infrastructure Improvements

### 5.1 Docker image size reduction

**Backend** (`Dockerfile.backend`):
- Remove Go toolchain from runtime stage (lines 41-43) — saves ~700MB
- Remove `gcc`, `make`, `libc6-dev` from runtime apt-get (lines 36-38)
- Add BuildKit cache mounts for Go modules: `--mount=type=cache,target=/root/go/pkg/mod`

**Frontend** (`Dockerfile.frontend`):
- Switch to Alpine base for runtime (Debian -> Alpine = ~158MB -> ~15MB)
- Build with `CGO_ENABLED=0` for static binary

### 5.2 Multi-platform builds

- Use `TARGETARCH` build arg for Go download URL (supports amd64 + arm64)
- Add `make build-multiplatform` target using `docker buildx`

### 5.3 Persistent session state

- Add named Docker volume `backend-data` in `docker-compose.yml`
- Mount at `/home/claude/.claude-sandbox-data`
- Move session names file from `/tmp/claude-session-names.json` to volume (`session.go:19`)
- Move upload dir from `/tmp/uploads` to volume (`handlers.go:18`)
- Keep relay sockets in `/tmp` (ephemeral, correct behavior)

### 5.4 Frontend container hardening

In `docker-compose.yml`, frontend service:
- Add `read_only: true` + `tmpfs: [/tmp]`
- Add `cap_drop: [ALL]` (needs no special capabilities)

### 5.5 Structured JSON logging

- Switch `slog.NewTextHandler` to `slog.NewJSONHandler` in both `backend/main.go` and `frontend/main.go`
- Add `LOG_FORMAT` env var (default: `json`, option: `text`)

### 5.6 Makefile improvements

Add missing targets:
- `make dev` — referenced in CLAUDE.md but doesn't exist
- `make logs` / `make logs-backend` / `make logs-frontend`
- `make status` — `docker compose ps`
- `make clean` — `docker compose down -v --rmi local`

---

## Verification Plan

1. **Race conditions**: Build backend with `-race` flag, run concurrent WebSocket sessions
2. **Tab persistence**: Open tabs, refresh page, verify tabs restore
3. **Reconnect**: Kill backend, verify "[Connection lost — press Enter to reconnect]" appears, restart backend, press Enter, verify reconnection
4. **Search filter**: Create 5+ sessions, type partial name, verify filtering
5. **Fullscreen**: Press Alt+F, verify sidebar/header hidden, terminal fills viewport, Escape exits
6. **Healthcheck**: Stop tmux, verify `/healthz` returns unhealthy; restart, verify ok
7. **Output download**: Create session with output, hit scrollback endpoint, verify clean text file
8. **Docker size**: Compare image sizes before/after (expect backend ~1.8GB -> ~1.1GB, frontend ~158MB -> ~15MB)
9. **Persistence**: Create session with custom name, restart container, verify name persists

---

## Files Modified (Summary)

**Backend** (6 files modified, 2 new):
- `backend/relay.go` — race condition fixes, ViewerCount(), Scrollback(), Stats()
- `backend/session.go` — rand.Read fix, Stats(), configurable paths
- `backend/handlers.go` — healthcheck, stats endpoint, scrollback endpoint, CORS comment
- `backend/broker.go` — rand.Read fix
- `backend/ringbuffer.go` — Size(), Cap() methods
- `backend/main.go` — load config, pass to constructors
- `backend/config.go` — NEW: Config struct + LoadConfig()
- `backend/metrics.go` — NEW (optional): Prometheus metrics

**Frontend** (6 files modified, 3 vendor files added):
- `frontend/web/templates/layout.html` — vendor CDNs, search input, connection indicator, shortcut modal, loading states
- `frontend/web/templates/fragments/sessions.html` — ARIA attributes
- `frontend/web/static/js/views.js` — tab persistence, search filter, fullscreen, shortcuts, connection status
- `frontend/web/static/js/terminal.js` — reconnect button, observer cleanup, upload spinner
- `frontend/web/static/css/style.css` — fullscreen class, loading indicator styles
- `frontend/handlers.go` — proxy routes for stats + scrollback endpoints
- `frontend/web/static/vendor/htmx-ext-sse.min.js` — NEW (vendored)
- `frontend/web/static/vendor/idiomorph-ext.min.js` — NEW (vendored)

**Infrastructure** (4 files modified):
- `Dockerfile.backend` — remove Go toolchain from runtime, BuildKit caches
- `Dockerfile.frontend` — Alpine base, CGO_ENABLED=0
- `docker-compose.yml` — named volume, frontend hardening, JSON logging
- `Makefile` — new targets (dev, logs, clean, status)
