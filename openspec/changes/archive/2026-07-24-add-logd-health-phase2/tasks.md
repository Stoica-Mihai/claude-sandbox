## 1. Shared contract

- [x] 1.1 Add `ServiceStatus{Service, State, Since, LastLogSeen *time.Time}` (JSON tags) to `shared/`.
- [x] 1.2 Add routes `RouteStatus="/api/status"`, `RouteStatusStream="/api/status/stream"`, and a sessiond health-port default const to `shared/`.

## 2. sessiond /healthz (`sessiond/health.go`)

- [x] 2.1 Extract the control-socket probe used by `-ping` into a reusable function (single source for `-ping` and `/healthz`).
- [x] 2.2 Add an internal HTTP listener on `SESSIOND_HEALTH_PORT` (default `:8083`) serving `GET /healthz` → 200 `{"status":"ok"}` when the socket check passes, 503 otherwise. Shallow (self only).
- [x] 2.3 Wire it into `sessiond/main.go` (start alongside the control server; bind failure warns + continues, non-fatal).
- [x] 2.4 Tests: `/healthz` returns 200 when the probe passes, 503 when it fails; shares the `-ping` fn.

## 3. Frontend: self-healthz + status proxy/guard (`frontend/handlers.go`, `frontend/proxy.go`)

- [x] 3.1 Replace `mux.Handle("GET /healthz", s.backendProxy)` with a local handler returning 200 `{"status":"ok"}` (frontend self-liveness, not backend-proxied).
- [x] 3.2 Factor `handleLogsProxy`'s tunnel guard into one shared guarded-logd handler; register it for `/api/logs`, `/api/logs/`, `/api/status`, `/api/status/` (all via the existing `logdProxy`).
- [x] 3.3 Downgrade the `resp == nil` WS-dial failure in `proxy.go` from `slog.Error` to `slog.Debug`; keep `resp != nil` non-404 at `Warn`; 404→1000 unchanged.
- [x] 3.4 Tests: `GET /healthz` served locally (200 even with backend unreachable); `/api/status` + `/api/status/` routed to the guarded proxy (not backend catch-all); all-method tunnel 403; non-tunnel GET proxied.

## 4. logd health monitor + status API (`logd/health.go`, `logd/store.go`, `logd/handlers.go`, `logd/main.go`)

- [x] 4.1 `store.add` stamps `lastSeen[service]=now`; expose a `lastSeen()` snapshot accessor (mutex-guarded).
- [x] 4.2 Health monitor: config of `{service, healthURL}` (defaults = compose service names, env-overridable); poll loop (~2 s, 1 s timeout) with an injectable probe fn.
- [x] 4.3 Edge-debounced state per service (2 consecutive fails → `down`, 1 success → `up`, `Since` on transition); never crash on probe error; publish snapshot to a status SSE hub on transition.
- [x] 4.4 `GET /api/status` → `[]ServiceStatus` (state map merged with lastSeen), stable order. `GET /api/status/stream` → SSE (snapshot on connect + each transition; bounded per-subscriber queue, evict-on-full).
- [x] 4.5 Start the poller in `logd/main.go` (ctx-cancelled with the server); register the status routes.
- [x] 4.6 Tests: state machine (single-blip no flip; 2-fail → down; recovery → up; `Since` updates) with injected probe, `-race`; lastSeen surfaced; `/api/status` shape + stable order; SSE snapshot-on-connect + on-transition; slow subscriber doesn't block.

## 5. Compose

- [x] 5.1 `docker-compose.yml`: add `SESSIOND_HEALTH_PORT` env to `sessions` (no published port — internal only). logd health targets default to service names; add explicit envs only if overriding.
- [x] 5.2 `docker compose config -q` validates.

## 6. Integration verification

- [x] 6.1 Build + `-race` test all affected modules (shared, sessiond, logd, frontend); vet clean.
- [x] 6.2 Bring the stack up; `curl /api/status` shows all five `up` with last-log-seen; `/api/status/stream` emits a snapshot. (Human gate.)
- [x] 6.3 Flip test: `make restart-backend` → status shows backend `down` then `up`; confirm frontend `/healthz` stays 200 while backend is down; confirm the WS-dial ERROR flood is gone (now debug). (Human gate.)
