## Context

Authoritative design: `docs/logd-phase3-ui-design.md`; locked mockup: `docs/logd-ui-mockup.html` (verified via headless render, dark/light, collapsed/expanded). Frontend-only phase over the landed Phase-1/2 APIs.

## Goals / Non-Goals

**Goals:** a Futurism Logs surface (`/logs`) in the dashboard shell — live status strip, filters/search/live-tail, dense log list with scroll-loaded history — reusing existing frontend patterns (ES-module `init()`, delegated `actions.js`, `chat-scroll.js`, the SSE client, the guarded logd proxy).

**Non-Goals:** any server/API/compose change (Phases 1–2 done); logs-specific sidebar menus (later); export, regex query, retention, per-line expansion, alerting.

## Decisions

- **New route `/logs` in the shared shell**, not a separate app. `layout.html` renders per-surface: sub-label + sidebar body switch on the route; the footer switcher + header are shared. Bookmarkable, and the sub-label is the "where am I".
- **Sidebar-footer switcher (Dashboard·Logs·Settings)** over a header button — the peer-surfaces belong together, pinned bottom (Slack/VS Code pattern), and it survives the mobile drawer with zero header crowding. Settings relocates here (stays a modal).
- **Per-surface sidebar body; sessions never on `/logs`** — separation. Logs body is a placeholder for future logs menus.
- **Collapse = icon-only rail** — matches the live 48px overlay rail (text can't fit); logs are viewed at the rail (expanded overlays the main).
- **History = scroll-load + jump-to-latest, no pager** — matches real log tools and this app's chat; reuse `chat-scroll.js` sticky-scroll rather than reinvent.
- **Status/level color = ink/accent/muted only** (law 4): up=ink, down=blink-accent; ERROR=accent + left-edge. Boxy status chips use a **per-theme-contrasting `--ink` offset** (the kit's `shadow-mute` vanishes on carbon) — an override-ledger entry.
- **Reuse, don't duplicate:** SSE client + event→patch translation mirror `chat-events.js`/`chat-render.js` structure (pure, unit-testable translator; DOM render separate).

## Risks / Trade-offs

- **`layout.html` becomes route-aware** (sub-label, sidebar body). → Keep it a thin per-route branch driven by the handler; the session views stay untouched.
- **Settings relocation** touches existing chrome. → Additive-in-spirit: the modal + its logic are unchanged; only the trigger moves to the footer.
- **Expanded sidebar overlays the logs main** (live drawer model). → Accepted; logs are viewed at the collapsed rail, expand only to switch surfaces (documented).
- **Render fidelity** hard to eyeball blind. → Verify with the same headless-chromium screenshot method used to build the mockup, in both themes + collapsed/expanded + mobile.

## Migration Plan

1. Route + shell: `GET /logs`; `layout.html` footer switcher, per-route sub-label + sidebar body, brand→`/`, drop header gear.
2. Logs view template + CSS (status strip, filter bar, log list, footer, jump pill) with override-ledger entries.
3. JS: `logs.js` (manager) + render/events; reuse `chat-scroll.js` + the SSE client; wire `/api/logs`, `/api/logs/stream`, `/api/status`, `/api/status/stream`.
4. Verify: frontend build/tests; live headless render (dark/light, collapsed/expanded, mobile); flip a service and watch a chip; logd-down → unavailable state.

Rollback: additive; removing the `/logs` route + footer entries + logs assets restores the prior dashboard (Settings can revert to the header gear).

## Open Questions

None — settled across the mockup iteration (see the design doc's "Locked decisions").
