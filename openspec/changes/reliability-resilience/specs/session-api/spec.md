## MODIFIED Requirements

### Requirement: Stale session file cleanup
The session discovery process SHALL automatically remove `~/.claude/sessions/<pid>.json` files when the referenced PID is no longer alive. This prevents dead session entries from accumulating on disk and cluttering the session list with ghost entries that will never become active again.

#### Scenario: Dead PID detected during discovery
- **WHEN** `discoverSessions()` reads a session file and determines via `syscall.Kill(pid, 0)` that the PID is no longer alive
- **THEN** the system SHALL delete the session file from `~/.claude/sessions/` and log a debug message indicating the cleanup, and the dead session SHALL NOT be included in the returned list

#### Scenario: Alive PID detected during discovery
- **WHEN** `discoverSessions()` reads a session file and determines that the PID is still alive
- **THEN** the system SHALL include the session in the returned list as before, with `Alive: true`, and SHALL NOT delete the file

#### Scenario: File deletion fails
- **WHEN** the system attempts to delete a stale session file but the deletion fails (e.g., permission error)
- **THEN** the system SHALL log a warning and continue processing the remaining session files. The stale session SHALL be included in the returned list with `Alive: false` as a fallback.

#### Scenario: Cleanup does not affect managed sessions
- **WHEN** a managed session (spawned by the dashboard) has its PTY process exit but is still being cleaned up by `waitProcess()`
- **THEN** the cleanup SHALL only target session files on disk. The managed session's in-memory state is handled separately by the session manager's `waitProcess()` goroutine and the merge logic in `ListSessions()`.

### Requirement: Health check endpoint
The system SHALL expose a `GET /healthz` endpoint that returns HTTP 200 when the dashboard server is running and able to accept requests. This endpoint is used by Docker's health check mechanism to detect unresponsive containers.

#### Scenario: Server is healthy
- **WHEN** a GET request is made to `/healthz`
- **THEN** the server SHALL respond with HTTP 200 and a JSON body `{"status": "ok"}`

#### Scenario: Server is unresponsive
- **WHEN** the Go HTTP server has stopped accepting connections or is deadlocked
- **THEN** the `/healthz` request SHALL time out, and Docker's health check SHALL mark the container as unhealthy after the configured number of retries

#### Scenario: Health check does not require authentication
- **WHEN** a GET request is made to `/healthz` from any origin
- **THEN** the server SHALL respond without requiring any authentication headers or cookies, since the endpoint is intended for infrastructure probing only
