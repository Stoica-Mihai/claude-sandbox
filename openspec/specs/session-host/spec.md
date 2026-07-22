# session-host Specification

## Purpose
The sessiond daemon — a standalone session host that owns claude sessions end-to-end. sessiond spawns each Claude Code session directly under a PTY it owns, feeds all output into a continuous per-session vt emulator (so joining viewers get rendered snapshots, never raw replays), and serves clients over a unix-socket protocol: a control socket for SPAWN/LIST/KILL ops and one socket per session for attach streams. It runs in the dedicated `sessions` container, separate from the backend, so sessions and their terminal state persist across backend container restarts and rebuilds.

## Requirements

### Requirement: sessiond owns claude sessions end-to-end
The system SHALL run a `sessiond` daemon that spawns each Claude Code session directly under a pseudo-terminal it owns (`creack/pty`), with no detach layer in between. Spawn SHALL run `claude --session-id <uuid> --dangerously-skip-permissions` (or `--resume <uuid>` for resumes) in the requested working directory with `TERM=xterm-256color`, generate the `claude-<hex>` session name, and reply with the session name. Kill SHALL signal the session's process group with `SIGTERM`, wait a short grace period, then `SIGKILL` if still alive. dtach SHALL NOT be installed or invoked anywhere in the system.

#### Scenario: Spawn a new conversation
- **WHEN** a SPAWN op arrives with `cwd=/workspace/my-project` and a conversation uuid
- **THEN** sessiond SHALL start claude under an owned PTY in that directory and reply with the generated session name

#### Scenario: Kill a session
- **WHEN** a KILL op arrives for a live session
- **THEN** sessiond SHALL signal the process group SIGTERM, escalate to SIGKILL after the grace period if needed, close all attach connections with a session-ended close, and remove the session from its registry

#### Scenario: dtach is gone
- **WHEN** the sessions and backend images are inspected
- **THEN** neither SHALL contain the dtach binary, and no code path SHALL reference dtach sockets or attach processes

### Requirement: Continuous per-session terminal state
sessiond SHALL feed every PTY output chunk into a per-session vt emulator (2000-line scrollback) for the session's entire life, independent of whether any viewer is attached. The emulator SHALL never be discarded or replaced while its session lives, so no repaint nudge (SIGWINCH flap, `-r winch`) is ever needed to reconstruct state.

#### Scenario: Output with no viewers attached
- **WHEN** claude produces output while zero viewers are attached
- **THEN** the emulator SHALL record it, and a later viewer's snapshot SHALL include it

#### Scenario: State survives viewer churn
- **WHEN** all viewers detach and a new viewer attaches later
- **THEN** the snapshot SHALL reflect the full emulator state including scrollback accumulated while unattached

### Requirement: Snapshot rendered at requested dimensions with mode restoration
On attach, sessiond SHALL render a snapshot at the exact dimensions carried by the ATTACH handshake: terminal reset, scrollback history scrolled out of the viewport, alt-screen re-enter when active, the screen painted row-by-row at absolute positions, cursor position and visibility, **and** re-assertion of tracked terminal modes — bracketed paste (`?2004`), mouse reporting (`?1000/?1002/?1003/?1006`), and application cursor keys (`?1`) — so the joining terminal behaves identically to one that watched the byte stream live.

#### Scenario: Snapshot at the viewer's size
- **WHEN** a viewer attaches with `cols=120, rows=30`
- **THEN** sessiond SHALL resize the emulator per the active-viewer rules and render the snapshot at the dimensions in effect, never at a previous viewer's stale dimensions

#### Scenario: Bracketed paste survives a join
- **WHEN** claude has enabled bracketed paste and a new viewer attaches
- **THEN** the snapshot SHALL include the bracketed-paste enable sequence so pastes in the new viewer are bracketed

### Requirement: Unix-socket protocol
sessiond SHALL listen on a control socket (`control.sock`) for request/response ops — SPAWN, LIST, KILL — and on one socket per session for attach streams. Frames SHALL be length-prefixed (`type u8`, `len u32`, payload) with types DATA (raw PTY bytes for terminal sessions, opaque JSON event/input lines for chat sessions — both directions), CONTROL (JSON control messages), SNAPSHOT (rendered replay, terminal sessions only), and CLOSE. The ATTACH handshake SHALL carry the viewer's initial dimensions for terminal sessions; for chat sessions dimensions are not required (see `chat-session-host`). A terminal ATTACH SHALL be answered with a SNAPSHOT frame followed by live DATA; a chat ATTACH SHALL be answered with live DATA only, no SNAPSHOT. The CONTROL JSON shapes SHALL mirror the WebSocket text-message contract (`resize`, `deactivated`) byte-compatibly so the backend bridge translates without interpretation. The SPAWN request SHALL carry a `kind` field (`terminal` or `chat`); an absent or empty `kind` SHALL be treated as `terminal` for backward compatibility.

#### Scenario: Attach handshake (terminal)
- **WHEN** a client connects to a terminal session socket and sends ATTACH with `{cols, rows}`
- **THEN** sessiond SHALL register it as a viewer, reply with one SNAPSHOT frame, and stream subsequent output as DATA frames

#### Scenario: Attach handshake (chat)
- **WHEN** a client connects to a chat session socket and sends ATTACH
- **THEN** sessiond SHALL register it as a viewer and stream subsequent events as DATA frames, with no SNAPSHOT frame

#### Scenario: List sessions
- **WHEN** a LIST op arrives on the control socket
- **THEN** sessiond SHALL reply with every live session's name, cwd, creation time, and conversation uuid from its in-memory registry, regardless of kind

#### Scenario: SPAWN without a kind defaults to terminal
- **WHEN** a SPAWN op arrives with no `kind` field (or an empty one)
- **THEN** sessiond SHALL spawn a terminal (PTY) session, exactly as before this capability existed

### Requirement: Kind-aware session dispatch
The registry SHALL dispatch a SPAWN op to the PTY-based terminal actor or the pipe-based chat actor based on the request's `kind` field. LIST, KILL, and shutdown SHALL operate uniformly across both kinds through a shared minimal interface (kill, exit-notification, listener serving) — a caller of LIST or KILL SHALL NOT need to know or care which kind a session is.

#### Scenario: Spawn dispatches by kind
- **WHEN** a SPAWN op arrives with `kind=chat`
- **THEN** the registry SHALL start the chat actor (see `chat-session-host`) rather than the PTY actor

#### Scenario: Kill is kind-agnostic
- **WHEN** a KILL op arrives for a session name
- **THEN** the registry SHALL terminate it the same way regardless of whether it is a terminal or chat session

#### Scenario: One live child per conversation uuid across kinds
- **WHEN** a conversation uuid has a live terminal session and a SPAWN for the same uuid with `kind=chat` arrives without the terminal session first being killed
- **THEN** the registry's behavior SHALL NOT result in two simultaneously live children for the same uuid — a caller performing a mode switch is expected to kill the existing child first

### Requirement: Viewer fan-out and active-viewer policy live in sessiond
Each attach connection SHALL be an independent viewer with its own bounded outbound queue; a viewer whose queue overflows SHALL be evicted rather than blocking the session actor. Active-viewer selection, PTY resizing to the active viewer's dimensions, suspension of non-active viewers, and `deactivated` notifications SHALL follow the multi-viewer-resize capability, executed inside sessiond. The active-viewer slot SHALL only ever be assigned to a connection currently registered in the viewer set, so a resize racing an eviction cannot pin the PTY size to a dead connection.

#### Scenario: Slow viewer is evicted
- **WHEN** a viewer's outbound queue fills during a broadcast
- **THEN** sessiond SHALL evict that viewer and continue serving the others without blocking

#### Scenario: Resize racing an eviction
- **WHEN** a resize frame from a viewer arrives after that viewer has been evicted
- **THEN** sessiond SHALL NOT make the evicted connection the active viewer and SHALL NOT apply its dimensions

### Requirement: Session persistence across backend restarts
Claude processes SHALL be children of sessiond in the `sessions` container, so rebuilding or restarting the backend container SHALL NOT terminate sessions or discard terminal state. After a backend restart, a reconnecting viewer SHALL receive an exact snapshot (including scrollback) from the live emulator.

#### Scenario: Backend rebuild during make watch
- **WHEN** `make watch` rebuilds the backend container while sessions are running
- **THEN** every session SHALL keep running, and reconnecting viewers SHALL repaint from the emulator snapshot with scrollback intact

### Requirement: Clean-slate boot
sessiond SHALL NOT adopt pre-existing claude processes at boot (its container starting fresh implies none can exist). Boot SHALL remove stale session sockets from the socket volume. sessiond SHALL NOT write PID or metadata sidecar files; liveness SHALL be tracked via in-memory child-process handles, and a session SHALL be dropped (registry, socket, viewers closed) when its child exits.

#### Scenario: Stale sockets at boot
- **WHEN** sessiond starts and the socket volume contains sockets from a previous run
- **THEN** it SHALL unlink them before listening

#### Scenario: Claude exits on its own
- **WHEN** a session's claude process exits (e.g. `/exit`)
- **THEN** sessiond SHALL close that session's attach connections with a session-ended CLOSE, remove its socket, and drop it from LIST results

### Requirement: Bounded input writes
Input DATA written to a session's PTY SHALL use a write deadline so a program that stops reading stdin fails the write with an error to that viewer instead of blocking the session actor.

#### Scenario: Program stops reading stdin
- **WHEN** input is written while the PTY buffer is full past the deadline
- **THEN** the write SHALL fail, the session actor SHALL keep processing output and other viewers, and the writing viewer SHALL receive an error

### Requirement: Sessions container and health
The `sessions` compose service SHALL run sessiond with the heavy runtime image (claude, git, plugins — the current backend runtime stage, minus dtach) and both it and the backend SHALL mount the socket volume (mode 0700, shared UID), `/workspace`, and `$CLAUDE_CONFIG_DIR`. The container healthcheck SHALL probe the control socket (`sessiond -ping`). The backend service SHALL depend on the sessions service being healthy.

#### Scenario: Healthcheck
- **WHEN** the sessions container is healthy
- **THEN** `sessiond -ping` SHALL connect to the control socket and exit 0, and Docker SHALL mark the service healthy

#### Scenario: Race detector is clean
- **WHEN** the sessiond test suite runs with `-race`
- **THEN** no data races SHALL be reported

### Requirement: Reactivate control op
sessiond SHALL accept a `reactivate` CONTROL frame on a session stream. It SHALL make the sending viewer active (via the active-viewer policy: resize the PTY to the viewer's dimensions, suspend the others, notify them), then send that viewer a fresh SNAPSHOT frame rendered at its dimensions. A `reactivate` from a connection that is not a registered viewer SHALL be ignored. Unlike a DATA frame, it SHALL write nothing to the PTY.

#### Scenario: Reactivate makes the requester active and repaints it
- **WHEN** a registered but suspended viewer sends a `reactivate` CONTROL frame
- **THEN** sessiond SHALL set it active, resize the PTY to its dimensions, suspend and notify the previously active viewer, and enqueue it a fresh SNAPSHOT frame

#### Scenario: Reactivate writes nothing to the PTY
- **WHEN** sessiond handles a `reactivate` frame
- **THEN** it SHALL NOT write to the session PTY
