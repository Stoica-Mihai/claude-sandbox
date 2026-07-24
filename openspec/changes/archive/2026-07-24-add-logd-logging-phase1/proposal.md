## Why

The four containers (`sessiond`, `backend`, `frontend`, `holesail`) each emit logs reachable only via `docker logs` per container — there is no single place to see or query them, and a crash's most important lines are the easiest to miss. Phase 1 delivers the server side of an in-house aggregator (`logd`): a durable, complete log pipeline plus a query API the agent can `curl` and a live-tail stream. The human-facing Futurism UI is a separate later phase.

## What Changes

- **Durable service logging (`shared`).** `InitLogging()` becomes `InitLogging(service string)`. In-container (`LOG_DIR` set) each service writes structured JSON log records to a durable per-service file on a shared volume, `dup2`s fd 2 onto that file so panics / `log.Fatal` / raw stderr are captured too, and mirrors human-readable text to the original console so `docker logs` keeps working. Size-based rotation (5 generations × ~20 MB). Graceful degrade to stderr-only if the file can't be opened — never crashes the service. **BREAKING** (internal only): `InitLogging` signature changes; all three Go mains are updated in this change.
- **New `logd` service.** A Go service that tails the per-service files read-only with a persisted byte-offset checkpoint (at-least-once, crash-safe, rotation-aware), and serves a query API (`GET /api/logs`), an SSE live-tail (`GET /api/logs/stream`), and `GET /healthz`. Files are the query source of truth (bounded newest-first scan across live + rotated generations); an in-memory ring buffer backs only the recent fast-path and live-tail fan-out.
- **Frontend proxy + guard.** A verbatim reverse proxy to `logd`, exposed at both `/api/logs` and `/api/logs/`, with an all-method tunnel-origin 403 (stricter than share — logs may contain secrets).
- **Compose + images.** New `logd` service, `logs` (shared, RW to producers / RO to logd) and `logd-state` volumes, `Dockerfile.logd`, producer images pre-create the `/logs` dir, `LOG_DIR` env on the three producers.
- **Out of scope:** the §4 Logs UI (later phase).

## Capabilities

### New Capabilities
- `service-logging`: how each service durably persists its own logs — structured JSON file sink with fd-level crash capture, console mirror, bounded rotation, and graceful degradation. (Design §1.)
- `log-aggregator`: the `logd` service and its exposure — complete file tailing with offset checkpointing, the query API and SSE live-tail with their completeness guarantees, and the guarded frontend proxy + deployment wiring. (Design §2, §3, §5.)

### Modified Capabilities
<!-- None. No existing spec's requirements change: logging behavior was previously unspecified, and the tunnel-origin primitive (share-tunnel) is reused unchanged rather than modified. -->

## Impact

- **Code:** `shared/env.go` (+ `shared/` log-record contract, new `golang.org/x/sys/unix` dep); `backend/main.go`, `frontend/main.go`, `sessiond/main.go` (pass service name); new `logd/` Go module; `frontend/handlers.go` + `frontend/main.go` (logd proxy, route, guard, `LOGD_URL`).
- **Infra:** `docker-compose.yml` (logd service, `logs` + `logd-state` volumes, frontend `*uidgid`, `LOG_DIR`/`LOGD_URL` env); `Dockerfile.sessions`, `Dockerfile.backend`, `Dockerfile.frontend` (`/logs` dir precreate); new `Dockerfile.logd`.
- **Runtime:** `docker logs <svc>` for the three producers stays human-readable text; their fd 2 is redirected to the log file (crash capture). `logd` is non-critical — the frontend does not depend on it; `/api/logs` 502s gracefully if it is down.
