# dtach-sessions Specification

## Purpose
dtach-based session lifecycle — spawning Claude Code as detached dtach masters, discovering sessions via PID sidecars, killing via the process group, persistence across dashboard-process restart, off-`/tmp` storage, and the disabled direct-CLI path. Sessions are created only from the dashboard.
## Requirements
### Requirement: Spawn Claude Code sessions as detached dtach masters
The system SHALL spawn each Claude Code session as a detached `dtach` master that holds the `claude` process, created with `dtach -n <sockdir>/claude-<hex> -E -z bash -c '...'`. The system SHALL generate a UUIDv4 conversation id and launch claude with `--session-id <uuid> --dangerously-skip-permissions` so the dashboard owns the conversation id. The inner command SHALL write the claude process PID to the PID sidecar before exec, and the spawner SHALL write a metadata sidecar containing the working directory, creation time, and the conversation uuid. The working directory MUST exist and MUST be under `/workspace`. On socket-name collision the system SHALL retry with a new random name up to 3 times.

#### Scenario: Spawn records a conversation uuid
- **WHEN** a spawn request is made with `cwd=/workspace/my-project`
- **THEN** the system SHALL generate a UUIDv4, launch `claude --session-id <uuid> --dangerously-skip-permissions`, store the uuid in the session's metadata sidecar, and record it in the session index

#### Scenario: Working directory outside workspace
- **WHEN** a spawn request specifies a cwd not under `/workspace`
- **THEN** the system SHALL reject the request with an error

### Requirement: Discover sessions via PID sidecars and liveness
The system SHALL discover sessions by scanning the metadata directory for `claude-*.pid` sidecars and probing each session's liveness via a `signal 0` check on the recorded PID. A session that fails the liveness probe SHALL be treated as gone, and its socket and metadata sidecars SHALL be unlinked. The system SHALL NOT use `tmux list-sessions`. Discovery keys off the PID sidecar (which the backend owns) rather than the socket, because dtach removes its own socket when the inner process exits.

#### Scenario: Multiple sessions running
- **WHEN** the metadata directory holds live sidecars for `claude-a1b2c3d4` and `claude-e5f6g7h8`
- **THEN** the system SHALL return both sessions with cwd and creation time from their sidecars

#### Scenario: Empty session set
- **WHEN** no live `claude-*` sessions exist
- **THEN** the system SHALL return an empty list without error

#### Scenario: Stale session from a crashed master
- **WHEN** a session's PID sidecar exists but the process is no longer alive
- **THEN** the system SHALL exclude it from the list and unlink its socket and sidecars

#### Scenario: Session list caching
- **WHEN** multiple session list requests arrive within 2 seconds
- **THEN** the system SHALL return cached results from the first scan. The cache SHALL be invalidated when a session is spawned or killed.

### Requirement: Kill sessions via the PID sidecar
The system SHALL terminate a session by reading its PID sidecar and sending `SIGTERM` to the inner process group, followed by `SIGKILL` after a short grace period if it has not exited. When the inner process exits, its dtach master SHALL exit and remove its socket. Any WebSocket connections attached to the session SHALL receive a close frame. If the PID sidecar is absent, kill SHALL be best-effort: the relay's attach SHALL be closed and the socket unlinked.

#### Scenario: Kill a running session
- **WHEN** a kill request is made for `claude-a1b2c3d4`
- **THEN** the system SHALL signal the inner process group, the session SHALL disappear from the list, the relay SHALL stop, and an SSE update event SHALL be published

#### Scenario: Kill a non-existent session
- **WHEN** a kill request targets a session that is not alive
- **THEN** the system SHALL respond with HTTP 404

### Requirement: Periodic session list polling
The system SHALL run a background goroutine that re-discovers sessions every 5 seconds and compares the result with the cached list. On change, the system SHALL update the cache, sync relays for new/gone sessions, and publish an SSE event. This detects sessions that exit without dashboard interaction.

#### Scenario: Session exits without dashboard interaction
- **WHEN** a user types `/exit` in a session and no dashboard action triggers a refresh
- **THEN** the poller SHALL detect the session's disappearance within 5 seconds and publish an SSE update event

### Requirement: Session persistence across dashboard-process restart
The system SHALL discover and list all live dtach sessions on startup. The dtach masters are children of init, independent of the backend process, so sessions created before a backend *process* restart SHALL remain interactive — users SHALL be able to attach via WebSocket and see current terminal state. Sessions do NOT survive a restart of the backend *container* (the masters live in that container).

#### Scenario: Backend process restarts with running sessions
- **WHEN** the backend process restarts while its container stays up and sessions are still running
- **THEN** both sessions SHALL appear in the list immediately and SHALL be attachable via WebSocket

#### Scenario: Backend shutdown preserves sessions
- **WHEN** the backend process is stopped without stopping the container
- **THEN** the dtach masters and their claude processes SHALL continue running and SHALL be recoverable on next startup

### Requirement: Direct CLI claude is disabled
The container SHALL provide a `claude` shell function (and a `make claude` target) that does NOT start a session. Instead it SHALL print a message directing the user to the dashboard and exit non-zero. All sessions are created via the dashboard, which spawns the real claude binary directly, so every session is captured by the relay from its first byte.

#### Scenario: User runs claude from the container shell
- **WHEN** a user runs `claude` from the container shell
- **THEN** the shell function SHALL print a message pointing to the dashboard and return non-zero, without starting a claude process

#### Scenario: Dashboard spawning is unaffected
- **WHEN** the dashboard spawns a session
- **THEN** it SHALL invoke the real claude binary directly (not the shell function), so spawning continues to work

### Requirement: Socket and metadata storage location
The system SHALL store session sockets and metadata under a non-shared, user-owned directory with mode `0700`, honoring `CLAUDE_SOCK_DIR` / `CLAUDE_META_DIR` and otherwise defaulting under `$XDG_RUNTIME_DIR/claude/{sock,meta}` with a `/home/claude/.local/state/claude/{sock,meta}` fallback. The system SHALL NOT place sockets or session metadata in a world-readable shared `/tmp` path. (Custom display names persist in the session index under `$CLAUDE_CONFIG_DIR` — see the "Persisted session index" requirement.)

#### Scenario: Storage directory permissions
- **WHEN** the system creates the socket and metadata directories
- **THEN** they SHALL be created with mode `0700` owned by the running user

### Requirement: Persisted session index
The system SHALL maintain a dashboard-owned session index at `$CLAUDE_CONFIG_DIR/dashboard-sessions.json`, mapping each conversation uuid to its working directory, creation time, and optional custom name. The index SHALL be written atomically under a mutex. Because it lives in the persistent, host-mounted config dir, the index and custom names SHALL survive container restarts. The index SHALL be the source for the per-folder resume list and for custom display names. The system SHALL NOT read claude's transcript files or depend on claude's on-disk directory layout to build the list.

#### Scenario: Index entry written on spawn
- **WHEN** a new session is spawned in `/workspace/cmux`
- **THEN** the index SHALL gain an entry keyed by the conversation uuid with that cwd and the creation time

#### Scenario: Names persist across container restart
- **WHEN** a session has been renamed and the backend container is recreated
- **THEN** the custom name SHALL still be present in the index and SHALL display in the sidebar and resume list

#### Scenario: Index unaffected by claude storage changes
- **WHEN** claude's transcript storage format or layout changes
- **THEN** the resume list SHALL still be built from the dashboard index, and at worst show no entries — it SHALL NOT error

### Requirement: Resume a recorded conversation
The system SHALL resume a conversation by spawning a new detached dtach session whose inner command runs `claude --resume <uuid> --dangerously-skip-permissions` in the conversation's recorded working directory. Resuming SHALL reuse the existing conversation uuid and SHALL NOT create a duplicate index entry. A resumed conversation SHALL behave as a normal live session (relay, sidebar, kill).

#### Scenario: Resume an existing conversation
- **WHEN** a resume request is made for a known conversation uuid
- **THEN** the system SHALL spawn a dtach session running `claude --resume <uuid>` in that conversation's cwd and it SHALL appear as a live session

#### Scenario: Resume is delegated to claude
- **WHEN** the conversation cannot be resumed (e.g. claude has no such transcript)
- **THEN** claude SHALL report the condition in the session terminal; the dashboard SHALL NOT parse transcripts to validate resumability

