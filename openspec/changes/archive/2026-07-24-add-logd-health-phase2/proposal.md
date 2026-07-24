## Why

logd (Phase 1) aggregates logs but has no view of whether each service is actually *up*. Operators and agents want live fleet health — "is each container serving right now" — next to the logs. Phase 2 makes logd a **synthetic health monitor**: it probes every service's `/healthz` and exposes aggregated per-service status. This is the server side; the dashboard UI (logs view + status strip) is Phase 3.

## What Changes

- **sessiond gains a shallow `/healthz`** on a new internal HTTP port (`:8083`) — its first TCP surface (internal network only). Returns 200 when its control socket is accepting, 503 otherwise, reusing the `sessiond -ping` check. Docker healthcheck stays `-ping` (unchanged).
- **frontend serves its own shallow `/healthz`** instead of proxying it to backend (`GET /healthz` currently forwards to backend — reports the wrong service and cascades: frontend's healthcheck fails when backend is down). **BREAKING** for the meaning of `frontend:8080/healthz` (now self, not backend) — intended and internal.
- **logd health monitor**: polls all five services' `/healthz` (~2 s, 1 s timeout), edge-debounced state (2 fails → down, 1 success → up), tracks last-log-seen per service, and serves `GET /api/status` + `GET /api/status/stream` (SSE).
- **frontend proxies `/api/status` + `/api/status/`** through the existing guarded logd proxy (all-method tunnel 403, same as `/api/logs`).
- **WS-dial-noise downgrade**: the `resp == nil` (backend unreachable) path in `frontend/proxy.go` drops from `ERROR` to `DEBUG` — the status panel is now the authoritative "backend down" signal.
- **Out of scope:** all UI (logs view + status strip) → Phase 3.

## Capabilities

### New Capabilities
- `service-health`: shallow per-service `/healthz` liveness endpoints, logd's synthetic health monitor (probe + debounced state + last-log-seen), the `/api/status` query + SSE contract, and its guarded frontend exposure.

### Modified Capabilities
<!-- None. The frontend self-healthz and WS-dial-noise downgrade are implementation corrections, not requirement changes to log-aggregator or service-logging; the new health behavior is captured under service-health. -->

## Impact

- **Code:** `sessiond/` (new `health.go` + main wiring, extract the `-ping` socket check), `logd/` (new `health.go` monitor, `store.go` last-seen, `handlers.go` status routes, `main.go` start poller), `frontend/handlers.go` (self-healthz + status proxy/guard refactor), `frontend/proxy.go` (log downgrade), `shared/` (`ServiceStatus` type, status routes, sessiond health port const).
- **Infra:** `docker-compose.yml` (sessiond `SESSIOND_HEALTH_PORT` env; optional logd health-target envs — defaults match service names).
- **Runtime:** rebuilding `sessions` to add `health.go` ends running sessions (one-time, as any `sessiond/` change). `frontend:8080/healthz` now reflects frontend only.
