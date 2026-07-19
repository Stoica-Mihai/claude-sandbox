# dtach-sessions Delta

## MODIFIED Requirements

### Requirement: Periodic session list polling
The system SHALL run a background goroutine that reconciles the backend's in-memory session store against sessiond's LIST op every 5 seconds. On change, the system SHALL update the store and publish an SSE event. This detects sessions that exit without dashboard interaction. The poll SHALL NOT scan sidecar files or probe PIDs.

#### Scenario: Session exits without dashboard interaction
- **WHEN** a user types `/exit` in a session and no dashboard action triggers a refresh
- **THEN** the poller SHALL detect the session's disappearance from LIST within 5 seconds and publish an SSE update event

### Requirement: Socket and metadata storage location
The system SHALL store the sessiond control and per-session sockets on a compose named volume mounted in both the sessions and backend containers, in a directory with mode `0700` owned by the shared-UID user, honoring `CLAUDE_SOCK_DIR`. The system SHALL NOT place sockets in a world-readable shared `/tmp` path. No per-session metadata or PID sidecar files SHALL be written. (Custom display names persist in the session index under `$CLAUDE_CONFIG_DIR` — see the "Persisted session index" requirement.)

#### Scenario: Storage directory permissions
- **WHEN** sessiond creates the socket directory at boot
- **THEN** it SHALL be created with mode `0700` owned by the running user

### Requirement: Resume a recorded conversation
The system SHALL resume a conversation by asking sessiond to spawn `claude --resume <uuid> --dangerously-skip-permissions` in the conversation's recorded working directory. Resuming SHALL reuse the existing conversation uuid and SHALL NOT create a duplicate index entry. A resumed conversation SHALL behave as a normal live session (viewer attach, sidebar, kill).

#### Scenario: Resume an existing conversation
- **WHEN** a resume request is made for a known conversation uuid
- **THEN** the system SHALL spawn a session running `claude --resume <uuid>` in that conversation's cwd and it SHALL appear as a live session

#### Scenario: Resume is delegated to claude
- **WHEN** the conversation cannot be resumed (e.g. claude has no such transcript)
- **THEN** claude SHALL report the condition in the session terminal; the dashboard SHALL NOT parse transcripts to validate resumability

## REMOVED Requirements

### Requirement: Spawn Claude Code sessions as detached dtach masters
**Reason**: dtach is removed; sessions are spawned directly under a sessiond-owned PTY.
**Migration**: See session-host "sessiond owns claude sessions end-to-end". UUID generation, `--session-id`, cwd validation, and index recording are unchanged in behavior; the metadata/PID sidecars and dtach socket-collision retry no longer exist.

### Requirement: Discover sessions via PID sidecars and liveness
**Reason**: No sidecars exist; sessiond holds live child-process handles and answers LIST from its in-memory registry.
**Migration**: Backend discovery becomes the LIST reconciliation in the modified "Periodic session list polling" requirement; sessiond-side lifecycle is in session-host "Clean-slate boot".

### Requirement: Kill sessions via the PID sidecar
**Reason**: Kill is a sessiond control op signalling its own child's process group; no sidecar is read.
**Migration**: See session-host "sessiond owns claude sessions end-to-end" (kill scenario). WebSocket close-frame behavior on kill is unchanged.

### Requirement: Session persistence across dashboard-process restart
**Reason**: The promise was unrealizable — the backend binary is its container's main process, so dtach masters died with every backend exit. Replaced by a stronger, real guarantee.
**Migration**: See session-host "Session persistence across backend restarts": sessions now survive backend container rebuilds; they end when the sessions container is rebuilt or the host restarts.
