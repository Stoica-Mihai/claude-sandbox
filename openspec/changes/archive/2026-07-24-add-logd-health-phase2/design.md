## Context

Authoritative design: `docs/logd-phase2-health-design.md` (approved 2026-07-24). This summarizes; that doc is the source of truth. Builds on Phase 1 (logd pipeline + query/SSE, landed). Phase 2 = server/plumbing only; the dashboard UI (logs view + status strip) is Phase 3.

## Goals / Non-Goals

**Goals:**
- Live per-service up/down for all five containers, aggregated by logd, queryable + streamable.
- Black-box `/healthz` probing (tests the real serving path), shallow per endpoint (no cascade).
- Non-root, self-contained, no Docker-socket coupling (Phase 1 ethos).

**Non-Goals:**
- Any UI (Phase 3). Readiness/startup probes, `/metrics`, per-dependency detail, error-rate — YAGNI at single-host scale. holesail log ingestion (unrelated).

## Decisions

- **HTTP `/healthz` probe over log-heartbeat or Docker-socket.** Probing the real endpoint is truthful (black-box) and true-health, not just liveness; sessiond's endpoint is ~15 lines of Go. Docker-socket is authoritative but couples to the host (rejected, as in Phase 1). Heartbeat only proves the log pipe (rejected once sessiond's endpoint proved cheap).
- **Shallow endpoints, aggregation is the deep view.** Each `/healthz` checks only itself (AWS/SRE cascade hazard); logd's `/api/status` is the fleet view. This also fixes the frontend's current deep `/healthz` (it proxies to backend → cascades).
- **Edge-debounced state (2 fails → down, 1 success → up).** Absorbs a single dropped poll; `Since` marks transitions. Cheap and readable.
- **last-log-seen from the store, not a second probe.** logd already ingests every line; stamp `lastSeen[service]` on `add`. Free liveness-adjacent signal.
- **Reuse Phase 1 machinery.** Status SSE mirrors the logs SSE hub (bounded per-subscriber, evict-on-full); the frontend guarded-logd proxy handler is factored to serve both `/api/logs*` and `/api/status*`.

## Risks / Trade-offs

- **sessiond gains a TCP surface** (it was socket-only). → Internal network only, no published port, health-only; its real work stays on the control socket. Minor, accepted.
- **Changing frontend `/healthz` semantics** (was backend-proxied). → It's the correct behavior (self-liveness) and fixes a latent cascade in frontend's own Docker healthcheck; internal only.
- **Probe adds a little internal traffic** (5 GETs / 2 s). → Trivial; localhost-class hops on the compose net.
- **`/healthz` is liveness, not deep health** — a wedged-but-listening service could read `up`. → Acceptable for a fleet up/down panel; last-log-seen gives a secondary staleness hint. Deeper checks are out of scope.

## Migration Plan

1. `shared`: add `ServiceStatus`, status routes, sessiond health port const.
2. sessiond: extract the `-ping` socket check, add `/healthz` listener + main wiring; compose env.
3. frontend: self-`/healthz`; status proxy + guard refactor; WS-dial-noise → debug.
4. logd: health monitor + last-seen + `/api/status`(+stream); start poller in main.
5. Verify: build/test/vet all modules; live — `curl /api/status`, watch a service flip during `make restart-backend`.

Rollback: additive; removing the poller + status routes and reverting frontend `/healthz` restores Phase-1 behavior.

## Open Questions

None — status detail (up/down + since + last-log-seen), guard policy (all-method tunnel 403), sessiond probe depth (control-socket, shallow), and debounce (2 fails) were settled in brainstorming.
