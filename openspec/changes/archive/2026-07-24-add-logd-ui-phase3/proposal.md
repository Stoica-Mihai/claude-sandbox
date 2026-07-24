## Why

Phases 1–2 built the log pipeline + health monitor and exposed `GET /api/logs`, `/api/logs/stream`, `/api/status`, `/api/status/stream` (all proxied + tunnel-guarded). The human still has no UI — only `curl`. Phase 3 adds the **Futurism Logs dashboard**: a `/logs` surface with a live per-service status strip, filters, search, live-tail, and scroll-loaded history. Frontend only — no server/API work. Locked design + mockup: `docs/logd-phase3-ui-design.md`, `docs/logd-ui-mockup.html`.

## What Changes

- **New `/logs` route + view** in the frontend, rendered in the dashboard shell: status strip + filter bar + dense mono log list + live/paused footer.
- **Status strip** — one chip per health-probed peer (backend, sessiond, frontend, holesail; not logd), up=ink dot / down=blinking-accent, last-seen; from `/api/status` + SSE.
- **Filters** mirror the query API — service `.seg`, level `.seg` (exact match), search (substring `q`), **live-tail toggle**; log list newest-first, ERROR in accent + left-edge, non-JSON as `RAW`.
- **History = scroll-load + jump-to-latest** (reuse `chat-scroll.js`); live-tail follows newest via `/api/logs/stream`. No pager.
- **Shell nav (dashboard-ui change):** a pinned **sidebar-footer switcher — Dashboard · Logs · Settings** (Settings moves out of the header gear into the footer; it stays a modal, Dashboard/Logs are routes); header **sub-label reflects the surface** (`DASHBOARD`/`LOGS`); brand logo → `/`; sidebar body is **per-surface** (session list on `/`, logs-context placeholder on `/logs` — sessions never shown on logs). Collapsed rail = icons only.

## Capabilities

### New Capabilities
- `logs-ui`: the `/logs` route and Logs view — status strip, filter/search/live-tail bar, dense log list, scroll-loaded history + jump-to-latest, all wired to the Phase-1/2 APIs.

### Modified Capabilities
- `dashboard-ui`: shell gains a per-surface sidebar body + a pinned footer surface-switcher (Dashboard/Logs/Settings); the Settings trigger moves from the header gear to the footer; the header sub-label becomes the active-surface context; the brand logo links home.

## Impact

- **Frontend only:** new `GET /logs` route + a logs view fragment/template; `layout.html` gains the footer switcher, per-route sub-label + sidebar body, brand→`/` link, and drops the header settings gear; new ES modules (`logs.js` + render/events, reuse `chat-scroll.js`); logs components in `app.css` + override-ledger entries (notably the `--chip-sh` neutral offset). Vendored deps only.
- **No backend/logd/shared/API/compose change** — Phases 1–2 provide all data, already proxied + guarded.
