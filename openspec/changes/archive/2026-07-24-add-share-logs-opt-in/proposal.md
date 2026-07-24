# Opt-in log sharing over the tunnel

## Why
Today the frontend 403s **every** tunnel-originated request to `/api/logs*` and
`/api/status*`, unconditionally — logs may carry secrets, so the Logs surface is
LAN-only and shows "Log service unavailable" to any tunnel visitor. That is the
right default, but it is not a choice: a host who deliberately wants to watch
logs from a phone over the tunnel has no way to allow it.

Make log/status exposure a **host-controlled scope of the share**, not a fixed
rule. The dashboard is always shared; logs (and the status strip that rides with
them) are shared only when the host opts in, and the opt-in is **off by default
on every publish** — fail-closed.

## What Changes
- Add a frontend-owned, in-memory `shareLogsEnabled` flag (default `false`). The
  logs/status tunnel guard rejects tunnel requests **unless** the flag is set.
- Reset the flag to `false` on every share lifecycle mutation
  (`start`/`stop`/`regenerate`), so each publish starts with logs private; a
  frontend restart also lands off.
- Add frontend-native routes `GET /api/share/logs` (read `{enabled}`, allowed
  over the tunnel) and `POST /api/share/logs {enabled}` (set it; **403 over the
  tunnel** so a visitor cannot self-grant). Registered as specific patterns so
  they win over the `/api/share/` proxy prefix — the holesail sidecar never sees
  them and is **not modified**.
- One flag gates **both** `/api/logs*` and `/api/status*` (the Logs page's
  status strip needs `/api/status`; status is low-sensitivity but travels with
  logs).
- Sharing panel gains a **Share logs** toggle in the public state, off by
  default, reflecting/writing the new endpoint.

## Impact
- Affected specs: `share-tunnel` (new requirement), `log-aggregator` (guard now
  conditional), `service-health` (guard now conditional).
- Affected code: `frontend/handlers.go` (flag, routes, guard, reset),
  `frontend/web/templates/layout.html` + `frontend/web/static/js/share.js`
  (toggle), `shared/routes.go` + `shared/types.go` (route const + envelope).
- holesail sidecar: **unchanged** (no supply-chain rebuild).
- Security posture unchanged by default (logs stay LAN-only until a host on the
  main listener explicitly opts in per share session).
