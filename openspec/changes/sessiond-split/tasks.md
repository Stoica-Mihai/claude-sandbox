## 1. Groundwork

- [x] 1.1 Verify `charmbracelet/x/vt` mode-state API: can bracketed paste / mouse / app-cursor mode state be read from the emulator? Record the answer in design.md's Open Questions; if not exposed, note the DECSET/DECRST tracker goes into sessiond's write path (D6 fallback)
- [x] 1.2 Scaffold `sessiond/` Go module: `main.go`, `protocol/` package (frame codec: type u8 + len u32; op types SPAWN/LIST/KILL; frame types DATA/CONTROL/SNAPSHOT/CLOSE; ATTACH handshake struct), `go.mod` with `creack/pty` + `charmbracelet/x/vt`
- [x] 1.3 Unit-test the frame codec round-trip (encode/decode all frame types, oversize rejection, truncated-read errors)

## 2. sessiond core

- [x] 2.1 Move `termstate.go` into sessiond; extend `Snapshot()` with mode re-assertion (?2004, ?1000/?1002/?1003/?1006, ?1) per task 1.1's answer; port its tests
- [x] 2.2 Session actor: port the relay actor loop (viewer registry, per-viewer queues + evict-on-full, active-viewer/suspension/deactivated logic, snapshot-on-attach) minus reconnect loop, generation guards, resize flap, `awaitingSnapshot` — ATTACH carries dims so the snapshot renders immediately
- [x] 2.3 Fix in transit: active-viewer slot only assignable to registered viewers (evicted-conn resize/input cannot become `lastResizer`); add regression test for the resize-racing-eviction interleaving
- [x] 2.4 Spawn/kill: `pty.Start` of claude with TERM + cwd + `--session-id`/`--resume` flag, name generation, child `Wait` watcher → session teardown (CLOSE to viewers, socket unlink, registry drop); kill = SIGTERM → grace → SIGKILL on the process group
- [x] 2.5 Input write deadline on the PTY master; error surfaces to the writing viewer, actor keeps running; test with a stopped reader
- [x] 2.6 Sockets: control listener (SPAWN/LIST/KILL request-response) + per-session listeners (ATTACH→SNAPSHOT→stream); boot = mkdir 0700 + stale-socket cleanup; `-ping` flag for the healthcheck
- [x] 2.7 Port relay actor tests to the sessiond actor; add attach-handshake and LIST tests; `go test -race ./...` clean

## 3. Backend as bridge

- [x] 3.1 Protocol client package use in backend (replace directive → `../sessiond`); spawn/resume/kill/list in `lifecycle.go`/`session.go` delegate to control ops; index recording and validation (workspace, uuid regex) stay backend-side
- [x] 3.2 Rewrite the WS handler as the per-connection bridge: dial session socket on upgrade, ATTACH on first client resize, WS-binary↔DATA, WS-text↔CONTROL, sessiond CLOSE→WS 1000, dial failure/other termination→abnormal close
- [x] 3.3 Delete `relay.go`, `termstate.go` (moved), `discovery.go`, sidecar helpers in `paths.go`, `dtach` references; poll loop becomes 5s LIST reconciliation; `sessionstate.go` store keeps name/cwd/created/uuid fed from LIST + spawn replies
- [x] 3.4 Update backend tests: bridge tested against a fake sessiond socket (attach/snapshot/close-code mapping); DeleteHistory live-kill path now resolves via the store; drop dead relay/discovery tests

## 4. Containers and compose

- [x] 4.1 `Dockerfile.sessions`: today's backend runtime stage minus dtach, running sessiond; entrypoint.sh (config-dir seeding) moves here
- [x] 4.2 Slim `Dockerfile.backend`: builder + binary + curl + ca-certificates, shared UID/GID user, no claude/npm/Go/dtach
- [x] 4.3 docker-compose.yml: add `sessions` service (heavy limits, seccomp/IPv6 settings move from backend, healthcheck `sessiond -ping`, watch `./sessiond/`); backend gets socket volume + `/workspace` + config-dir mounts, `depends_on: sessions: service_healthy`, watch `./backend/ ./shared/ ./sessiond/`; named volume for `$CLAUDE_SOCK_DIR`
- [x] 4.4 Makefile: `restart-sessions` (pattern rule already covers it — verify), `make shell` targets the sessions container
- [x] 4.5 Update CLAUDE.md architecture section (four services, sessiond protocol, persistence semantics, no dtach)

## 5. Verify end-to-end

- [ ] 5.1 `make up`; spawn/kill/resume/rename/history-delete from the dashboard; two-viewer active-size takeover; image paste upload
- [ ] 5.2 The headline scenario: start a session, produce scrollback, `make restart-backend` — session survives, viewer reconnects, exact repaint with scrollback, no blank terminal, no size flap
- [ ] 5.3 Claude `/exit` → WS close 1000 "[Session ended]"; sessions-container restart → clean slate, stale sockets gone; `go test -race` green in backend and sessiond
- [ ] 5.4 Kill dtach: confirm no references remain in code, Dockerfiles, specs, or CLAUDE.md; remove the package from images
