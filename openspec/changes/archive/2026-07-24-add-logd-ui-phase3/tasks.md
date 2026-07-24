## 1. Route + shell (dashboard-ui)

- [x] 1.1 Add `GET /logs` to the frontend mux → renders `layout.html` with a logs context flag (sub-label `logs`, sidebar body = logs). `/` unchanged (sub-label `dashboard`, body = sessions).
- [x] 1.2 `layout.html`: header sub-label rendered from the route context; brand logo becomes a link to `/`; remove the header settings gear.
- [x] 1.3 `layout.html`: add the pinned sidebar-footer nav — Dashboard (terminal glyph → `/`), Logs (lines glyph → `/logs`, active-marked by context), Settings (gear → opens the existing modal). Icon-only in the collapsed rail, icon+label expanded.
- [x] 1.4 `layout.html`: sidebar body is per-surface — session list on `/`, logs-context placeholder on `/logs` (no session cards, no `+ NEW`, shead reads `Logs`).
- [x] 1.5 Wire Settings-open to the footer entry (reuse the existing `open-settings` action); confirm the modal still works from its new trigger.
- [x] 1.6 Persist the active session (localStorage) + restore-if-live on dashboard load (deferred past manager inits so the reopened instance isn't wiped), so a `/logs` round-trip returns to the same session.

## 2. Logs view template + CSS (logs-ui)

- [x] 2.1 Logs view template/fragment: status strip, filter bar (service seg, level seg, search, live-tail toggle, count), log list container, footer (summary + live/paused), jump-to-latest pill.
- [x] 2.2 `app.css`: logs components (`.lz-*` or app-named), status chips, log rows/levels, jump pill, live indicator — ink/accent/muted only, square/2px/offset laws. Add override-ledger entries (notably the neutral `--chip-sh` offset vs kit `shadow-mute`).
- [x] 2.3 Responsive: log rows collapse (time/service/message) on mobile; sidebar is the existing off-canvas drawer.

## 3. Logs JS (logs-ui)

- [x] 3.1 `logs.js` — view manager: create/destroy, initial `GET /api/logs` fetch + render, filter state (service/level/search) applied as query params, `init()` registers `data-action`s (no import-time side effects).
- [x] 3.2 `logs-events.js` (pure, unit-testable, mirroring `chat-events.js`): log record/stream-event → render patches; filter predicate matching the API (exact service/level, substring q).
- [x] 3.3 `logs-render.js`: row rendering (time/service/level/msg/attrs, ERROR accent + left-edge, RAW), status-strip rendering.
- [x] 3.4 Live-tail via `/api/logs/stream` SSE (reuse the existing SSE client); status strip via `/api/status` + `/api/status/stream`.
- [x] 3.5 History: reuse `chat-scroll.js` for follow/pause + jump-to-latest; scroll-up lazily loads older (`until` = oldest-shown ts). No pager.
- [x] 3.6 logd-unreachable (proxy 502) → render the unavailable state; dashboard unaffected.
- [x] 3.7 `main.js`/`tabs.js`: open the logs surface for the `/logs` route (create a `LogsManager`), mirroring the terminal/chat manager pattern.

## 4. Tests

- [x] 4.1 `logs-events`: record/stream → patch translation; filter predicate mirrors the API (exact level/service, substring q). Pure, no DOM.
- [x] 4.2 Frontend route: `GET /logs` renders the shell with logs context (sub-label `logs`, logs body, no session cards); `/` unchanged.
- [x] 4.3 Status strip renders up/down + last-seen from a stubbed `/api/status`; a stream transition flips a chip.
- [x] 4.4 Sticky-scroll reuse: follow → scroll-up pauses + jump pill → activate resumes.
- [x] 4.5 logd-unreachable → unavailable state.

## 5. Verification

- [x] 5.1 Frontend build + `go test ./...` (+ any JS tests) green; vet clean.
- [x] 5.2 Live headless render (dark/light, collapsed/expanded, mobile) — matches the locked mockup. (Human gate.)
- [x] 5.3 Stack up + `/logs`: live-verified populated rows, status chips, the logd-down "unavailable" state, and the `/logs`→`/` session-restore round-trip (CDP + screenshots). NOT separately re-exercised live: service-flip chip transition, live-tail streaming, scroll-load-older, jump-to-latest (covered by unit tests + the mockup; confirmed working in use).
