## 1. Project Setup

- [ ] 1.1 Create `dashboard/` directory in the project root with `go.mod` (module name: `claude-dashboard`) and dependencies: `github.com/creack/pty`, `github.com/gorilla/websocket`
- [ ] 1.2 Vendor frontend assets into `dashboard/static/vendor/`: download htmx.min.js (v2), xterm.js, xterm-addon-fit.js, xterm-addon-web-links.js and their CSS
- [ ] 1.3 Create `dashboard/static/` directory structure: `static/vendor/`, `static/css/`, `static/js/`
- [ ] 1.4 Create `dashboard/templates/` directory with Go HTML templates: `layout.html`, `fragments/sessions.html` (session list partial), `fragments/directory-picker.html` (directory listing partial)
- [ ] 1.5 Create `dashboard/web/embed.go` with `go:embed` directives for templates and static assets (matching tunnel-hub's pattern)

## 2. Go Server Skeleton

- [ ] 2.1 Create `dashboard/main.go` — HTTP server with `net/http.ServeMux`, embedded static file serving, listen on `:8080`
- [ ] 2.2 Set up route registration: `GET /` (dashboard page), `GET /fragments/sessions` (session list fragment), `POST /api/sessions` (spawn), `DELETE /api/sessions/{terminalId}` (kill), `GET /api/directories` (directory listing), `GET /events` (SSE stream), `/ws/terminal/{terminalId}` (WebSocket upgrade)
- [ ] 2.3 Implement Go HTML template parsing and rendering with `html/template`, loading from embedded filesystem, rendering into `bytes.Buffer` before writing response

## 3. Session Manager (Backend)

- [ ] 3.1 Create `dashboard/session.go` — session manager struct with `sync.RWMutex`-protected in-memory map of managed sessions (terminalId → PTY + process + scrollback buffer)
- [ ] 3.2 Implement session discovery: read all `*.json` files from `~/.claude/sessions/` (global directory, not project-scoped), parse `{pid, sessionId, cwd, startedAt}` from each, cross-reference PIDs with `syscall.Kill(pid, 0)` for liveness, skip malformed files with a log warning, merge with managed sessions by PID (dashboard-spawned sessions that also appear in the directory become a single entry with both terminal access and Claude metadata)
- [ ] 3.3 Implement spawn: validate `cwd` is under `/workspace` and exists, start `claude --dangerously-skip-permissions` via `os/exec.Command` + `creack/pty.Start()`, store in session map, return terminal ID, publish SSE update event
- [ ] 3.4 Implement kill: send `syscall.SIGTERM` to the PTY process, clean up session map entry, publish SSE update event
- [ ] 3.5 Implement scrollback ring buffer (10K lines) per session — goroutine reads PTY output, writes to ring buffer and any attached WebSocket

## 4. SSE Pub/Sub

- [ ] 4.1 Implement SSE event broker — struct with subscriber map (`sync.RWMutex`-protected), `Subscribe()` returns buffered channel (size 1), `Unsubscribe()` removes and closes channel
- [ ] 4.2 Implement `GET /events` SSE endpoint — set `Content-Type: text/event-stream`, subscribe to broker, write `event: update\ndata: \n\n` on each channel receive, flush after each write, unsubscribe on client disconnect
- [ ] 4.3 Wire session manager to publish events on spawn, kill, and PTY exit

## 5. HTTP Handlers

- [ ] 5.1 Implement `GET /` — render full dashboard page from `layout.html` with initial session list, HTMX SSE connection via `hx-ext="sse" sse-connect="/events"`
- [ ] 5.2 Implement `GET /fragments/sessions` — render `sessions.html` partial (HTMX fragment) with current session list, triggered by `sse:update`
- [ ] 5.3 Implement `POST /api/sessions` — parse form body `cwd`, spawn session, return updated session list fragment
- [ ] 5.4 Implement `DELETE /api/sessions/{terminalId}` — kill session, return updated session list fragment
- [ ] 5.5 Implement `GET /api/directories` — validate `path` query param against traversal, list directories under `/workspace`, render `directory-picker.html` partial

## 6. WebSocket Terminal (Backend)

- [ ] 6.1 Implement WebSocket upgrade handler at `/ws/terminal/{terminalId}` using `gorilla/websocket`
- [ ] 6.2 Implement bidirectional relay: goroutine reads PTY → writes WebSocket, goroutine reads WebSocket → writes PTY stdin
- [ ] 6.3 Implement resize handling: parse JSON resize messages `{"type":"resize","cols":N,"rows":N}` from client, call `pty.Setsize()`
- [ ] 6.4 Implement session lifecycle: on PTY exit send WebSocket close frame, on WebSocket disconnect detach (keep PTY alive), on reattach replay scrollback buffer

## 7. Frontend — Layout & View Modes

- [ ] 7.1 Build `layout.html` template — HTML shell with Tailwind CDN, DaisyUI CDN, vendored HTMX, SSE extension CDN. Header with logo, session count badge, view mode toggle (single/split/grid icon buttons in a DaisyUI join group), and theme toggle. Body with `hx-ext="sse" sse-connect="/events"`. Fixed sidebar + main content area. Reference mockup: `mockups/layout-a-sidebar.html`
- [ ] 7.2 Build sidebar section in `layout.html` — fixed-width left panel (w-72) with "Sessions" header, "New" button, session list container with `hx-get="/fragments/sessions" hx-trigger="sse:update" hx-swap="innerHTML"`. Include split-mode hint banner (hidden by default, shown via JS when split mode active)
- [ ] 7.3 Build `fragments/sessions.html` — session list partial: session cards with left border accent (emerald for active, blue for split-right), status dot + PID + time ago, directory path, DaisyUI badge (green "running" / yellow "external"), kill button visible on hover. Cards have `onclick` for left pane and `oncontextmenu`/shift+click for right pane
- [ ] 7.4 Build `fragments/directory-picker.html` — DaisyUI modal with breadcrumb navigation, directory list with radio selection, folder icons, subdirectory drill-down via `hx-get="/api/directories?path=..."` and `hx-target`, search/filter input, "Launch Session" confirm button via `hx-post`

## 8. Frontend — Single Terminal View

- [ ] 8.1 Build single view container (`#viewSingle`) — tab bar with session tabs (active tab highlighted, close button per tab), full-width terminal viewport div, status bar (PID, session ID, directory, dimensions, uptime)
- [ ] 8.2 Implement tab management in JS — open/close tabs, switch active tab, connect/disconnect xterm.js instances per tab

## 9. Frontend — Split Terminal View

- [ ] 9.1 Build split view container (`#viewSplit`) — two terminal panes side by side (flex row, each `flex-1 min-w-0`), each with its own header (session name, maximize/close buttons) and compact status bar
- [ ] 9.2 Implement draggable split divider — mousedown/mousemove handler that adjusts pane flex-basis, cursor `col-resize`, visual handle indicator that highlights on hover
- [ ] 9.3 Implement split pane assignment — click opens in left pane, shift+click or right-click opens in right pane, sidebar cards get `.in-split` class for right-pane sessions (blue border accent)
- [ ] 9.4 Wire each split pane to its own xterm.js instance and WebSocket connection, re-fit on divider drag

## 10. Frontend — Grid Overview

- [ ] 10.1 Build grid view container (`#viewGrid`) — responsive 2-column grid of session cards (DaisyUI cards with `grid grid-cols-2 gap-4`)
- [ ] 10.2 Build grid card template — session name, PID, duration, status badge, mini terminal preview (small dark box with recent output text, blinking cursor), kill button
- [ ] 10.3 Build "New Session" placeholder card — dashed border, plus icon, click opens directory picker modal
- [ ] 10.4 Build external session card variant — dashed border, reduced opacity, warning badge, placeholder instead of terminal preview
- [ ] 10.5 Implement grid card click — switch to single view and open that session's terminal

## 11. Frontend — Shared JS

- [ ] 11.1 Create `dashboard/static/js/terminal.js` — xterm.js manager: create/destroy terminal instances, connect WebSocket to `/ws/terminal/:id`, handle fit addon resize, send resize messages `{"type":"resize","cols":N,"rows":N}` to server, manage multiple concurrent connections for split mode
- [ ] 11.2 Create `dashboard/static/js/views.js` — view mode switching: toggle visibility of `#viewSingle`, `#viewSplit`, `#viewGrid`, update active button state in header join group, persist view mode in localStorage, restore on page load
- [ ] 11.3 Create `dashboard/static/js/theme.js` — dark/light theme toggle with DaisyUI `data-theme` attribute and localStorage persistence
- [ ] 11.4 Create `dashboard/static/css/style.css` — custom styles: session card hover/active/in-split transitions, cursor blink animation, pulse-alive animation, split divider styling, grid card hover lift, grain overlay, custom scrollbar, stagger-in entrance animations. Use Outfit font for UI, JetBrains Mono for terminal/code (Google Fonts CDN)

## 12. Container Integration

- [ ] 12.1 Update `Dockerfile` to copy `dashboard/` source, run `go build` during image build, place binary at `/home/claude/dashboard-server`
- [ ] 12.2 Add port mapping `8080:8080` to `docker-compose.yml`
- [ ] 12.3 Replace `command: sleep infinity` with `command: /home/claude/dashboard-server` in `docker-compose.yml`

## 13. Testing & Verification

- [ ] 13.1 Build the container image and verify the Go dashboard server starts on port 8080
- [ ] 13.2 Verify `GET /` returns the full dashboard HTML with HTMX, DaisyUI, xterm.js, and all three view mode containers
- [ ] 13.3 Verify SSE connection establishes at `/events` and receives update events on session changes
- [ ] 13.4 Spawn a session via the UI, verify terminal ID is returned and session appears in the sidebar across all connected clients
- [ ] 13.5 Verify single view: terminal renders with colors, slash command autocomplete, tab switching works
- [ ] 13.6 Verify split view: two terminals side by side, divider draggable, shift+click assigns right pane, both terminals functional simultaneously
- [ ] 13.7 Verify grid view: all sessions shown as cards with mini previews, clicking a card opens single view terminal
- [ ] 13.8 Verify session survives browser disconnect and can be reattached with scrollback replay
- [ ] 13.9 Verify dark/light theme toggle works and persists across reloads
- [ ] 13.10 Verify view mode persists in localStorage across reloads
- [ ] 13.11 Verify `docker exec -it claude_workspace bash` still works with the new entrypoint
- [ ] 13.12 Verify path traversal is blocked on `/api/directories`
