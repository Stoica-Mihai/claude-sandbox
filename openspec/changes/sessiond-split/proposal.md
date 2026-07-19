# Proposal: sessiond-split

## Why

dtach was adopted so sessions survive a backend restart, but the container topology voids that: the backend binary is the container's main process (`Dockerfile.backend` ENTRYPOINT→exec, `init: true`), so any backend exit — crash, `docker stop`, `make watch` rebuild — tears down the PID namespace and the dtach masters with it. dtach today delivers zero persistence while forcing the relay to treat terminal state as a reconstructable cache: the one-row resize flap, the `-r winch` repaint nudge, the attach reconnect loop with generation guards, scrollback loss on every relay swap, and blank terminals after backend restarts all exist to compensate. The last three commits on `main` are all fixes in this area — the design keeps generating bugs.

## What Changes

- **New `sessiond` daemon** (own Go module): spawns claude directly under a PTY it owns (`creack/pty`), keeps a `charmbracelet/x/vt` emulator + scrollback fed from that PTY for the session's whole life, and serves a framed protocol over per-session unix sockets: attach (snapshot + live stream), input, resize, kill, plus spawn/list/delete-support on a control socket.
- **New `sessions` container** runs sessiond with the heavy runtime (claude, git, plugins — today's backend image). It is rebuilt rarely; claude processes live here.
- **Backend container becomes a thin bridge**: WS viewers ↔ sessiond protocol. Its image drops claude/dtach/npm and goes slim. Backend rebuilds (`make watch`) no longer kill sessions; viewers reconnect and repaint from an exact snapshot.
- **dtach removed entirely** — package, spawn command, attach subprocess, `waitForSocket`, reconnect loop, generation guards, resize flap, `-r winch` nudge. **BREAKING** for any workflow attaching to dtach sockets directly (none exist; CLI is disabled).
- Relay actor logic (viewer fan-out, active-viewer resize, suspension, snapshot-at-viewer-size) moves into sessiond mostly unchanged; the backend keeps only WS handling.
- Fix in transit: the `lastResizer` ghost-conn race (a resize racing an eviction can pin the PTY size to a dead connection), and snapshot fidelity for terminal modes (bracketed paste, mouse reporting, application cursor keys) so a joining viewer's terminal behaves identically to one that watched the bytes live.
- Sidecar file formats (PID/meta), the session index, resume semantics, and the HTTP API surface (routes, status codes, payloads) are unchanged.

## Capabilities

### New Capabilities

- `session-host`: the sessiond daemon — PTY ownership, per-session emulator state, the attach/input/resize/spawn/kill protocol, the sessions container, adoption on sessiond restart, and session persistence across backend restarts.

### Modified Capabilities

- `dtach-sessions`: spawn/discover/kill/persistence requirements re-target sessiond (no dtach masters, no socket-collision retry against dtach sockets); persistence upgrades from "survives backend *process* restart" (never realizable) to "survives backend container rebuild". Index, resume, CLI-disabled, and storage-location requirements stay.
- `pty-relay`: the relay is no longer a `dtach -a` attach owner; ring-buffer and alt-screen-filter requirements (already superseded in code by the vt emulator) are replaced by emulator snapshot requirements owned by session-host; the backend side shrinks to a WS↔protocol bridge.
- `web-terminal`: replay wording moves from "reset + ring buffer" to "rendered emulator snapshot"; new scenario — viewers reattach with intact scrollback after a backend restart; input path wording updates.
- `multi-viewer-resize`: behavior unchanged; the resize mechanism becomes a protocol op applied by sessiond to its own PTY instead of `pty.Setsize` on an attach PTY.
- `session-api`: spawn/kill/discovery requirement text stops naming dtach (delegation to session-host; same routes, same codes); the stale "Relay tracks last activity timestamp" requirement (not implemented in the current relay) is removed.
- `dev-workflow`: a third service Dockerfile (`Dockerfile.sessions`) joins the build; compose watch rules keep `./backend/` rebuilds away from the sessions container; new scenario — sessions survive a backend rebuild during `make watch`.

## Impact

- **Code**: new `sessiond/` module; `backend/relay.go` + `backend/termstate.go` largely move/rewrite; `backend/session.go`, `lifecycle.go`, `discovery.go` delegate to the protocol client; `backend/handlers.go` WS handler re-points at the bridge. Frontend Go and JS untouched (WS contract identical).
- **Build/infra**: `Dockerfile.sessions` (heavy, from today's backend runtime stage), `Dockerfile.backend` slims down; `docker-compose.yml` gains the `sessions` service + shared socket volume; Makefile gains `restart-sessions`.
- **Dependencies**: `creack/pty`, `charmbracelet/x/vt` move to sessiond; dtach dropped from images.
- **Migration**: none live — sessions do not survive today's deploys, so cutover starts clean. Sidecar/index formats unchanged.
- **Tests**: relay actor tests port to sessiond; new protocol characterization test; backend bridge tested against a fake sessiond socket.
