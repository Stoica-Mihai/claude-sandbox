## 1. Shared log-record contract

- [x] 1.1 Add the `LogRecord` type `{ts, service, level, msg, attrs, raw}` (JSON tags) to the `shared/` module as the wire contract, with any level constants used by producers/consumer.
- [x] 1.2 Add `golang.org/x/sys/unix` to the `shared` module `go.mod` and tidy.

## 2. Shared ingestion (`shared/env.go`) — design §1

- [x] 2.1 Change `InitLogging()` → `InitLogging(service string)`; `LOG_DIR` unset keeps the current stderr `TextHandler`-only path unchanged.
- [x] 2.2 Implement a `multiHandler` (`slog.Handler`) delegating `Enabled`/`Handle`/`WithAttrs`/`WithGroup` to both children.
- [x] 2.3 Implement a mutex-guarded switchable file writer: `O_APPEND|O_CREATE|O_WRONLY`, size-check → rename-shift (5 generations, ~20 MB cap) → open-new → re-`dup2` under the write lock.
- [x] 2.4 `LOG_DIR` set path: `orig := unix.Dup(2)`; open `<LOG_DIR>/<service>.log`; `unix.Dup2(f.Fd(), 2)`; install `multiHandler` of `slog.JSONHandler`→file and `slog.TextHandler`→`os.NewFile(orig,…)` console; add `service` attr.
- [x] 2.5 Hold the file and console `*os.File` in package-level vars for process lifetime (never closed).
- [x] 2.6 Graceful degrade: any open/`dup2` error ⇒ warn to console and continue stderr-only; never crash.
- [x] 2.7 Unit tests: `LOG_DIR` set ⇒ JSON line in file + text line on console mirror; direct `os.Stderr.Write` (simulated panic) lands in file but not console; `LOG_DIR` unset ⇒ stderr-only, no file, no redirect; file-open failure ⇒ degrades, no crash.

## 3. Wire producers

- [x] 3.1 `backend/main.go`: call `InitLogging("backend")`.
- [x] 3.2 `frontend/main.go`: call `InitLogging("frontend")`; read `LOGD_URL` env (default `http://logd:8082`).
- [x] 3.3 `sessiond/main.go`: verify it calls the shared hook; wire `InitLogging("sessiond")` if not.

## 4. logd service — tailer (`logd/tailer.go`) — design §2

- [x] 4.1 New `logd/` Go module wired to the `shared` module (matching the other services' setup).
- [x] 4.2 Poll loop (~300 ms) over a `/logs/*.log` glob excluding `logd*.log`; per-file `{inode, offset}`; tolerate absent files at boot.
- [x] 4.3 Read `offset`→EOF, split lines, advance offset only to the last `\n`; buffer the remainder (partial-line rule).
- [x] 4.4 Bound the pending buffer (~1 MB): overflow ⇒ emit truncated `raw` record + resync at next `\n`; idle-timeout (~2 s) flush of a crash-truncated tail as `raw`.
- [x] 4.5 Rotation detection (inode change / size shrink); drain the rotated generation keyed on the tracked inode before switching to the new file.
- [x] 4.6 Parse each line: JSON ⇒ normalized `LogRecord`; non-JSON ⇒ `raw` record at level `error`, ingest timestamp.
- [x] 4.7 Persist offsets to `/state/offsets.json` via atomic temp-file + rename; flush ~1 s and on shutdown; resume from checkpoint on start.
- [x] 4.8 Tests: offset resume across restart; zero-loss across N-generation rotation (M lines spanning several rotations all surface); partial-line buffering; inode-change rotation; non-JSON→raw; `-race` clean.

## 5. logd service — store + API (`logd/store.go`, `logd/handlers.go`) — design §2

- [x] 5.1 In-memory bounded ring buffer fed by the tailer (recent fast-path + SSE fan-out only).
- [x] 5.2 Query scan over files (live + rotated `.1..N`) for in-scope services: bounded newest-first, `O(limit)` result ring, k-way `ts` merge across services.
- [x] 5.3 `GET /api/logs` with `service`, `level`, `since`/`until` (RFC3339 + relative like `-15m`), `q` (case-insensitive substring over the raw on-disk line), `limit` (default 500, capped).
- [x] 5.4 `GET /api/logs/stream` SSE: replay matching ring tail then live; per-subscriber bounded queue, evict-on-full (view-drop, never blocks tailer).
- [x] 5.5 `GET /healthz` ⇒ `{"status":"ok"}`; `main.go` binds `:8082`, runs tailer + HTTP server, clean shutdown flushes offsets.
- [x] 5.6 Tests: filter/query correctness, newest-first k-way merge, limit cap, bounded scan memory; SSE replay-then-live; slow-subscriber eviction doesn't block the tailer; healthz.

## 6. Frontend proxy + guard (`frontend/handlers.go`) — design §3

- [x] 6.1 Build `logdProxy` via `newReverseProxy(LOGD_URL, …)` (SSE auto-flush); add to `Server`.
- [x] 6.2 `handleLogsProxy`: 403 (JSON envelope) for **every** method when `isTunnelRequest(r)`; else proxy verbatim.
- [x] 6.3 Register both `mux.HandleFunc("/api/logs", …)` and `("/api/logs/", …)` so neither falls through to the `/api/` catch-all.
- [x] 6.4 Tests: bare `/api/logs` and `/api/logs/` both route to the guarded proxy (not the backend catch-all); all-method tunnel-origin 403; non-tunnel GET proxied.

## 7. Compose + images — design §5

- [x] 7.1 `docker-compose.yml`: new `logd` service (build with `GO_VERSION` + `*uidgid`, `*logging`, `*healthcheck` on `/healthz`, `develop.watch` on `./logd/` + `./shared/`, `claude-net`, no published port); no `depends_on` from frontend (non-critical).
- [x] 7.2 New named volumes `logs` (RW on sessions/backend/frontend, RO on logd) and `logd-state` (`/state` on logd); add `LOG_DIR=/logs` env to the three producers (NOT logd); add `LOGD_URL` to frontend; add `*uidgid` build arg to frontend.
- [x] 7.3 `Dockerfile.sessions`, `Dockerfile.backend`, `Dockerfile.frontend`: `RUN mkdir -p /logs && chown claude:claude /logs`.
- [x] 7.4 New `Dockerfile.logd`: multi-stage build, slim runtime + curl, non-root `claude`, `RUN mkdir -p /state && chown claude:claude /state`.

## 8. Integration verification

- [x] 8.1 Build all Go modules and run all unit tests (`-race` where applicable); fix failures.
- [x] 8.2 Bring the stack up; confirm the three producers write `/logs/<svc>.log`, `docker logs` stays human-readable text, and `logd` is healthy. (Verified: backend/frontend/sessiond .log present + JSON; docker logs text; logd healthy.)
- [x] 8.3 Smoke the API: `curl` `GET /api/logs` (filters), `GET /api/logs/stream` (live lines). (Verified via the dashboard origin; tunnel-origin 403 covered by TestLogsTunnelForbiddenAllMethods.)
- [x] 8.4 Re-tidy consumer go.sum (frontend/backend/sessiond) for the new transitive `x/sys` dep so the isolated Docker builds resolve it (caught by the live smoke; host go.work masked it).
