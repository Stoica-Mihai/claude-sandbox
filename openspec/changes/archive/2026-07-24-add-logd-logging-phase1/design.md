## Context

Authoritative design: `docs/logd-design.md` (reviewed exhaustively; concern log A–OO folded in). This document summarizes the decisions and risks; the design doc is the source of truth for load-bearing detail.

The four containers log only to their own `docker logs`. Phase 1 builds the server side of an in-house aggregator with a hard **completeness invariant** (never drop a line) and a query API + live-tail. The human-facing Futurism UI (design §4) is a separate later phase and is out of scope here.

The build-vs-adopt decision (adopting VictoriaLogs + Fluent Bit) was evaluated and recorded in the design doc; the user chose a small integrated Go service built on the *same* durable-file + tracked-offset invariant the mature collectors use.

## Goals / Non-Goals

**Goals:**
- Durable, complete capture of all four (v1: three Go) services' logs, including crash output.
- A `curl`-able query API and an SSE live-tail, exposed through the frontend behind the existing auth + tunnel-guard boundary.
- Non-root, self-contained, no host-path coupling — consistent with the rest of the stack.

**Non-Goals:**
- The Logs UI (design §4) — later phase.
- holesail (Node) ingestion — v2.
- Regex/LogsQL-grade query language, retention/compaction beyond rotation, metrics/alerting.
- Power-loss durability (no per-line `fsync`).

## Decisions

- **Durable local file + tracked offset, not push.** Each service appends to a shared-volume file; `logd` tails read-only from a persisted offset (at-least-once). The hot path is a local append — never a droppable network buffer. Chosen over a push pipeline because completeness is an invariant, not a tradeoff.
- **fd-2 redirect (`dup2`) for crash capture.** A `slog`-only sink misses panics / `log.Fatal` / raw stderr — the lines that matter most. `dup2`'ing fd 2 onto the file makes fd 2 *be* the file, capturing them via one synchronous `write(2)` before teardown, with no reader goroutine (hence no exit race). A console `TextHandler` mirror is added back on a saved original fd so `docker logs` stays human-readable. Alternative rejected: tailing Docker's own `json-file` logs — eliminates the app-side change but forces `logd` to run privileged with host-path coupling (breaks on rootless/podman/moved data-root), violating the non-root ethos.
- **Files are the query source of truth; the ring buffer is only a cache.** Queries scan the live file + rotated generations (bounded newest-first, `O(limit)` result ring, k-way `ts` merge across services) so a query can never miss a line on disk. The in-memory ring backs only the recent fast-path and SSE fan-out.
- **Poll tailer (~300 ms), not inotify** — inotify over Docker volume/overlay mounts is unreliable; polling is simple and correct.
- **Bounded rotation (5 generations × ~20 MB)** so a burst cannot rotate data out from under the poll loop at realistic rates, and disk is bounded.
- **All-method tunnel 403 on `/api/logs*`** (stricter than share, which allows GET) because logs may contain secrets. Both `/api/logs` and `/api/logs/` are registered so the bare path cannot fall through to the backend catch-all and bypass the guard.

## Risks / Trade-offs

- **The one honest limit (LL):** the durable buffer is N generations × cap (~100 MB/service). `logd` being down loses nothing while the files exist, but if `logd` stays down while a service out-produces the whole buffer, the oldest data rotates away before ingest. → Inherent to every file-tail collector; mitigation is the buffer size (raise N). With `restart: unless-stopped` (seconds of downtime) and Info-level rates this margin is enormous.
- **At-least-once duplicates:** a crash between offset flush and re-read may re-emit a few lines in the tail/ring. → Acceptable and dedupe-able; queries read files directly and do not duplicate.
- **fd-2 redirect quiets raw `docker logs`:** raw crash text goes to the file only, structured lines still mirror to the console. → Acceptable; `logd`/the file is the crash-reading surface.
- **`InitLogging` signature change is BREAKING for callers.** → Internal only; all three Go mains updated in this change.
- **Secrets in logs, no redaction (accepted risk I):** same trust boundary as the terminal the viewer already has; tunnel is 403'd all-method. Redaction is out of scope.
- **fd lifetime:** the file and console `*os.File` must be package-level and never closed, or a GC finalizer would close fd 2's target. → Explicitly held for process lifetime.
- **Rotation vs concurrent writers:** the file handler writes through a mutex-guarded switchable writer (size-check + rotate under the write lock). → Raw fd-2 writes during the tiny pre-`dup2` window land in `.1` and are drained; no loss.

## Migration Plan

1. Land `shared` logging change + wire the three mains (behind `LOG_DIR`; unset = today's behavior, so no test/local impact).
2. Add `logd`, compose wiring, Dockerfiles.
3. Add the frontend proxy + guard.
4. Verify live: real logs flowing, zero-loss rotation test green, `curl` the query API + SSE, tunnel 403.

Rollback: the feature is additive and gated on `LOG_DIR`/`logd` presence; removing the `logd` service and unsetting `LOG_DIR` reverts to prior behavior (frontend `/api/logs` simply 502s, dashboard unaffected).

## Open Questions

None — the design doc's review loop (A–OO) closed the outstanding concerns. Tunable constants (exact ring size, idle-timeout, poll interval) are finalized in code with the tests as the gate.
