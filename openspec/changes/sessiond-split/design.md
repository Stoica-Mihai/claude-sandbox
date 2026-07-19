# Design: sessiond-split

## Context

Terminal state (the PTY byte history) currently lives behind dtach, which discards it; the backend keeps a derived vt-emulator cache that goes stale or empty on every attach gap and gets repopulated by asking the program to repaint (`-r winch`, one-row resize flap). The backend binary is the container's main process, so every backend exit tears down the PID namespace — dtach masters included. Persistence is therefore zero in every automated path, while the compensation machinery (reconnect loop, generation guards, flap, deferred snapshots) is the repo's top bug source.

Constraints: the WS viewer contract (binary = terminal I/O, text = JSON control, `deactivated` message, close-code semantics) must not change — frontend Go and all JS stay untouched. Sidecar-era storage rules (0700, non-`/tmp`) still apply to the socket volume. The dashboard session index (`dashboard-sessions.json`) keeps its format and stays backend-owned.

## Goals / Non-Goals

**Goals:**
- Sessions survive backend rebuilds/restarts (`make watch` on `backend/` leaves claude running; viewers reconnect and repaint exactly).
- Terminal state has one owner: the emulator lives next to the PTY it mirrors, for the session's whole life.
- Delete the compensation machinery: dtach, attach subprocess, `waitForSocket`, reconnect loop, generation guards, resize flap, winch nudge, deferred-snapshot bookkeeping.
- Fix in transit: `lastResizer` ghost-conn race; snapshot restores terminal modes, not just pixels.

**Non-Goals:**
- Merging the frontend service into the backend (explicitly rejected — UI churn must not restart the API).
- Sessions surviving a `sessions`-container rebuild or host reboot.
- Frontend dev-asset hot-reload, picker/render-model cleanup (separate changes).
- Authentication (external proxy, unchanged).

## Decisions

**D1 — One daemon, actor per session (not one supervisor process per session).**
sessiond is a single process; each session is an actor goroutine owning PTY, emulator, and viewer fan-out — today's relay actor, relocated. Per-session supervisor processes would buy crash isolation but cost adoption logic, N× plumbing, and a spawn protocol; sessiond is small and rarely rebuilt, so its blast radius is acceptable and identical in kind to today's backend.

**D2 — Sockets: one control socket + one socket per session, on a shared named volume.**
`$CLAUDE_SOCK_DIR` moves to a compose named volume mounted in both containers (mode 0700, owned by the shared-UID user). `control.sock` carries request/response ops (spawn, list, kill); `<session-name>.sock` carries attach streams. Per-session sockets keep stream framing trivial and reuse today's path layout. Alternatives: gRPC (dependency + codegen for three ops), WS-over-unix (drags an HTTP stack into sessiond), single multiplexed socket (session-id routing and head-of-line concerns for zero benefit).

**D3 — Framing mirrors the WS contract 1:1.**
Length-prefixed frames: `[type u8][len u32][payload]`. Types: `DATA` (raw PTY bytes, both directions), `CONTROL` (JSON — resize, deactivated), `SNAPSHOT` (rendered replay), `CLOSE`. The backend bridge translates WS BinaryMessage↔DATA and TextMessage↔CONTROL mechanically, holding no session state. The `deactivated` JSON travels sessiond→bridge→WS byte-identical to today.

**D4 — ATTACH requires dimensions; snapshot is immediate, not deferred.**
The attach handshake carries `{cols, rows}`; sessiond replies with a snapshot rendered at exactly those dimensions, then streams. The bridge dials the session socket when the WS connects but sends ATTACH on the viewer's first resize message (the client already sends one on open). This makes today's `awaitingSnapshot` deferral structural instead of stateful and deletes that flag class.

**D5 — Viewer fan-out, active-viewer resize, suspension: all in sessiond.**
Each attach connection is a viewer. Per-viewer outbound queues with evict-on-full (today's `viewerQueueSize` semantics) live in sessiond; the bridge is a dumb pipe per WS connection. The `lastResizer` fix lands here: the active-viewer slot may only be assigned to a connection currently registered in the viewer set.

**D6 — Snapshot restores modes.**
Snapshot = reset, scrollback scroll-out, alt-screen re-enter, screen paint, cursor position/visibility (today's algorithm) **plus** re-assertion of tracked terminal modes: bracketed paste (?2004), mouse reporting (?1000/1002/1003/1006), application cursor keys (?1). If `charmbracelet/x/vt` exposes mode state, read it; otherwise sessiond tracks DECSET/DECRST for that fixed set on its write path (verify lib surface at implementation start — see Risks).

**D7 — Spawn moves into sessiond; index stays in the backend.**
sessiond owns the child process end-to-end: name generation, `pty.Start` of `claude --session-id/--resume … --dangerously-skip-permissions` with `TERM=xterm-256color`, cwd, kill (SIGTERM → grace → SIGKILL on the process group). The backend keeps API-level validation (workspace path, uuid regex, index membership) and records `dashboard-sessions.json` after a successful SPAWN reply. `$CLAUDE_CONFIG_DIR` and `/workspace` mount into **both** containers: sessions needs auth + cwd; backend needs index, settings editor files, transcript glob/delete, and directory browsing.

**D8 — PID/meta sidecars and adoption are deleted, not ported.**
Sidecars existed so a restarted backend could re-find dtach masters. Under sessiond, claude processes are its children: sessiond cannot restart without its container (same main-process topology), so there is never a live process to adopt. Boot = clean slate + stale-socket cleanup; liveness = in-memory process handles (child wait status), no signal-0 probes, no 5s sidecar scan. `discovery.go` and the sidecar helpers in `paths.go` are removed. The backend's poll loop degrades to a cheap LIST reconciliation against the control socket.

**D9 — Container/build layout.**
`Dockerfile.sessions` = today's backend runtime stage (claude, git, plugins, bubblewrap, entrypoint seeding `$CLAUDE_CONFIG_DIR`) running `sessiond`. `Dockerfile.backend` runtime slims to the Go binary + curl (healthcheck) — no claude, no dtach, no npm; dtach leaves both images. Both run the same UID/GID build-arg user so the socket volume is shared cleanly. Compose: `sessions` service (watch: `./sessiond/`), `backend` (watch: `./backend/` + `./sessiond/` for the protocol package; rebuilding backend is now harmless to sessions), healthcheck via `sessiond -ping` against the control socket; backend `depends_on` sessions healthy. Makefile gains `restart-sessions`.

**D10 — Module layout.**
New top-level `sessiond/` Go module: `sessiond/` (daemon main + session actors) and `sessiond/protocol` (frame codec + op types), consumed by the backend via a `replace` directive — same pattern as `shared/`. `shared/` itself is untouched (backend↔frontend wire contract only).

**D11 — Input backpressure.**
Input writes to the PTY get a write deadline (PTY masters are pollable; `os.File` deadlines work). A blocked program (e.g. flow-controlled) fails the write with an error surfaced to the viewer instead of freezing the session actor — removes the actor-freeze hazard that exists today in `handleInput`.

## Risks / Trade-offs

- [vt library may not expose mode state] → D6 fallback: a ~30-line DECSET/DECRST tracker for a fixed mode set on sessiond's write path; verify the lib surface as the first implementation task.
- [sessiond crash kills all sessions] → accepted: identical blast radius to today's backend crash, but sessiond is small, dependency-light, and rebuilt rarely; the churny code (API/UI) can no longer take sessions down.
- [Socket volume permission mismatch between containers] → both images create the user from the same UID/GID build args; volume initialized 0700 by sessiond at boot.
- [Protocol adds a hop backend↔sessiond] → unix-socket stream copy; negligible vs the WS hop that already exists, and it replaces a PTY-through-dtach hop.
- [Two heavy-ish images during transition] → backend image shrinks in the same change (dtach/claude/npm removed); net image weight drops.
- [Stale specs (`pty-relay` ring buffer, `session-api` lastActivity) diverge further if deltas are sloppy] → deltas in this change explicitly remove/replace the dead requirements.

## Migration Plan

Single cutover release — no live-session migration exists to do (sessions do not survive today's deploys):
1. Land sessiond + slim backend + compose in one change; `make up` recreates the stack; index/config files untouched on disk.
2. Rollback = revert the commit range and `make up`; no data format changes anywhere.

## Open Questions

*(both resolved at implementation start)*

- ~~`charmbracelet/x/vt` mode-state API surface~~ — resolved: `vt.Callbacks.EnableMode/DisableMode` deliver `ansi.Mode` transitions (fire inside `emu.Write` under our lock, same as the CursorVisibility callback). No DECSET/DECRST fallback tracker needed. Tracked set: `?1` cursor keys, `?1000/?1002/?1003/?1006` mouse, `?1004` focus events, `?2004` bracketed paste; re-emitted in snapshots via `ansi.SetMode`.
- ~~Socket volume mountpoint~~ — resolved: keep the existing default (`CLAUDE_SOCK_DIR=/home/claude/.local/state/claude/sock`); the compose named volume mounts at `/home/claude/.local/state/claude` in both containers, so path defaults stay unchanged.
