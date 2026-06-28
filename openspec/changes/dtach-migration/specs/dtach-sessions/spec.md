# Spec: dtach-sessions

**Spec Path:** `specs/dtach-sessions/spec.md`
**Change Type:** ADDED

---

## ADDED Requirements

### Requirement: Spawn Claude Code sessions as detached dtach masters
The system SHALL spawn each Claude Code session as a detached `dtach` master that holds the `claude` process. The session SHALL be created with `dtach -n <sockdir>/claude-<hex> -E -z` where `<hex>` is 8 random hexadecimal characters. The inner command SHALL write the claude process PID to `<metadir>/claude-<hex>.pid` before exec, and the spawner SHALL write a metadata sidecar `<metadir>/claude-<hex>.json` containing the working directory and creation time. The working directory MUST exist and MUST be under `/workspace`. On socket-name collision the system SHALL retry with a new random name up to 3 times.

#### Scenario: Spawn a new session
- **WHEN** a spawn request is made with `cwd=/workspace/my-project`
- **THEN** the system SHALL create a detached dtach master holding `claude --dangerously-skip-permissions`, write the PID and metadata sidecars, and the socket SHALL appear in `<sockdir>`

#### Scenario: Spawn with socket-name collision
- **WHEN** the generated socket path already exists
- **THEN** the system SHALL generate a new random name and retry, up to 3 attempts

#### Scenario: Working directory outside workspace
- **WHEN** a spawn request specifies a cwd not under `/workspace`
- **THEN** the system SHALL reject the request with an error

### Requirement: Discover sessions via socket directory and metadata sidecar
The system SHALL discover sessions by scanning `<sockdir>` for `claude-*` sockets and joining each with its metadata sidecar to produce name, working directory, and creation time. The system SHALL NOT use `tmux list-sessions`. A socket whose master is not alive SHALL be treated as gone, and its socket and sidecars SHALL be unlinked.

#### Scenario: Multiple sessions running
- **WHEN** `<sockdir>` contains live sockets `claude-a1b2c3d4` and `claude-e5f6g7h8`
- **THEN** the system SHALL return both sessions with cwd and creation time from their sidecars

#### Scenario: Empty socket directory
- **WHEN** `<sockdir>` contains no `claude-*` sockets
- **THEN** the system SHALL return an empty list without error

#### Scenario: Stale socket from a crashed master
- **WHEN** a `claude-*` socket exists but its dtach master is no longer alive
- **THEN** the system SHALL treat it as gone, exclude it from the list, and unlink the socket and its sidecars

#### Scenario: Session list caching
- **WHEN** multiple session list requests arrive within 2 seconds
- **THEN** the system SHALL return cached results from the first directory scan. The cache SHALL be invalidated when a session is spawned or killed.

### Requirement: Kill sessions via the PID sidecar
The system SHALL terminate a session by reading `<metadir>/claude-<hex>.pid` and sending `SIGTERM` to the inner process group, followed by `SIGKILL` after a short grace period if it has not exited. When the inner process exits, its dtach master SHALL exit and remove its socket. Any WebSocket connections attached to the session SHALL receive a close frame. If the PID sidecar is absent, kill SHALL be best-effort: the relay's attach SHALL be closed and the socket unlinked.

#### Scenario: Kill a running session
- **WHEN** a kill request is made for `claude-a1b2c3d4`
- **THEN** the system SHALL signal the inner process group, the socket SHALL disappear from `<sockdir>`, and an SSE update event SHALL be published

#### Scenario: Kill a non-existent session
- **WHEN** a kill request targets a name with no live socket
- **THEN** the system SHALL respond with HTTP 404

#### Scenario: Kill a session with no PID sidecar
- **WHEN** a kill request targets a session whose PID sidecar is missing
- **THEN** the system SHALL close the relay attach and unlink the socket as a best-effort termination

### Requirement: Periodic session list polling
The system SHALL run a background goroutine that scans `<sockdir>` every 5 seconds and compares the result with the cached list. On change (a session added or gone), the system SHALL update the cache, sync relays, and publish an SSE event. This detects sessions that exit or are created without dashboard interaction.

#### Scenario: Session exits without dashboard interaction
- **WHEN** a user types `/exit` in a Claude Code session and no dashboard action triggers a refresh
- **THEN** the poller SHALL detect the socket's disappearance within 5 seconds and publish an SSE update event

#### Scenario: Session created from CLI
- **WHEN** a user runs `claude` from the CLI, creating a new dtach session
- **THEN** the poller SHALL detect the new socket within 5 seconds and publish an SSE update event

### Requirement: Session persistence across dashboard restarts
The system SHALL discover and list all live dtach sessions on startup. Sessions created before a dashboard restart SHALL be fully interactive — users SHALL be able to attach via WebSocket and see current terminal state. Dashboard shutdown SHALL NOT terminate dtach sessions.

#### Scenario: Dashboard restarts with running sessions
- **WHEN** the dashboard process restarts while sessions `claude-a1b2c3d4` and `claude-e5f6g7h8` are still running
- **THEN** both sessions SHALL appear in the list immediately and SHALL be attachable via WebSocket

#### Scenario: Dashboard shutdown preserves sessions
- **WHEN** the dashboard process is stopped
- **THEN** the dtach masters and their claude processes SHALL continue running and SHALL be recoverable on next startup

### Requirement: Direct CLI claude is disabled
The container SHALL provide a `claude` shell function that does NOT start a session. Instead it SHALL print a message directing the user to create sessions from the dashboard and SHALL exit non-zero. All sessions are created via the dashboard (which spawns the real claude binary itself), so every session is captured by the relay from its first byte and is fully controllable in the browser.

#### Scenario: User runs claude from the container shell
- **WHEN** a user runs `claude` from the container shell
- **THEN** the shell function SHALL print a message pointing to the dashboard and return a non-zero exit code, without starting a claude process

#### Scenario: Dashboard spawning is unaffected
- **WHEN** the dashboard spawns a session
- **THEN** it SHALL invoke the real claude binary directly (not the shell function), so spawning continues to work

### Requirement: Socket and metadata storage location
The system SHALL store session sockets and metadata under a non-shared, user-owned directory with mode `0700`, defaulting to `$XDG_RUNTIME_DIR/claude/{sock,meta}` and falling back to `/home/claude/.local/state/claude/{sock,meta}`. The system SHALL NOT place sockets or session metadata in a world-readable shared `/tmp` path.

#### Scenario: Storage directory permissions
- **WHEN** the system creates the socket and metadata directories
- **THEN** they SHALL be created with mode `0700` owned by the running user
