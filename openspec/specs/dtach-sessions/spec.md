# dtach-sessions Specification

## Purpose
Backend-side lifecycle for claude sessions hosted by sessiond — periodic session-list polling, socket storage location, resuming recorded conversations, the persisted session index, and the disabled direct-CLI path. Sessions are created only from the dashboard; spawning, killing, PTY ownership, and terminal state are specified in `session-host`.
## Requirements
### Requirement: Periodic session list polling
The system SHALL run a background goroutine that reconciles the backend's in-memory session store against sessiond's LIST op every 5 seconds. On change, the system SHALL update the store and publish an SSE event. This detects sessions that exit without dashboard interaction. The poll SHALL NOT scan sidecar files or probe PIDs.

#### Scenario: Session exits without dashboard interaction
- **WHEN** a user types `/exit` in a session and no dashboard action triggers a refresh
- **THEN** the poller SHALL detect the session's disappearance from LIST within 5 seconds and publish an SSE update event

### Requirement: Direct CLI claude is disabled
The container SHALL provide a `claude` shell function (and a `make claude` target) that does NOT start a session. Instead it SHALL print a message directing the user to the dashboard and exit non-zero. All sessions are created via the dashboard, which spawns the real claude binary directly, so every session is captured by the relay from its first byte.

#### Scenario: User runs claude from the container shell
- **WHEN** a user runs `claude` from the container shell
- **THEN** the shell function SHALL print a message pointing to the dashboard and return non-zero, without starting a claude process

#### Scenario: Dashboard spawning is unaffected
- **WHEN** the dashboard spawns a session
- **THEN** it SHALL invoke the real claude binary directly (not the shell function), so spawning continues to work

### Requirement: Socket and metadata storage location
The system SHALL store the sessiond control and per-session sockets on a compose named volume mounted in both the sessions and backend containers, in a directory with mode `0700` owned by the shared-UID user, honoring `CLAUDE_SOCK_DIR`. The system SHALL NOT place sockets in a world-readable shared `/tmp` path. No per-session metadata or PID sidecar files SHALL be written. (Custom display names persist in the session index under `$CLAUDE_CONFIG_DIR` — see the "Persisted session index" requirement.)

#### Scenario: Storage directory permissions
- **WHEN** sessiond creates the socket directory at boot
- **THEN** it SHALL be created with mode `0700` owned by the running user

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
The system SHALL resume a conversation by asking sessiond to spawn `claude --resume <uuid> --dangerously-skip-permissions` in the conversation's recorded working directory. Resuming SHALL reuse the existing conversation uuid and SHALL NOT create a duplicate index entry. A resumed conversation SHALL behave as a normal live session (viewer attach, sidebar, kill).

#### Scenario: Resume an existing conversation
- **WHEN** a resume request is made for a known conversation uuid
- **THEN** the system SHALL spawn a session running `claude --resume <uuid>` in that conversation's cwd and it SHALL appear as a live session

#### Scenario: Resume is delegated to claude
- **WHEN** the conversation cannot be resumed (e.g. claude has no such transcript)
- **THEN** claude SHALL report the condition in the session terminal; the dashboard SHALL NOT parse transcripts to validate resumability

