# session-api Specification

## Purpose
HTTP API for managing Claude Code sessions — listing, spawning, killing, and browsing directories.

## Requirements
### Requirement: List active Claude Code sessions across all directories
The system SHALL expose a `GET /api/sessions` endpoint that returns all currently running Claude Code sessions inside the container, regardless of which working directory they were started in. Session discovery SHALL scan the per-session PID sidecars in the metadata directory and filter to sessions with the `claude-` name prefix. The endpoint SHALL return session name, working directory, creation time, and whether a dashboard WebSocket is currently attached.

#### Scenario: Sessions running in different directories
- **WHEN** a GET request is made to `/api/sessions` and dtach sessions are running in `/workspace/api-service`, `/workspace/frontend`, and `/workspace/infra`
- **THEN** the response SHALL include all three sessions, each showing its respective working directory

#### Scenario: No sessions are running
- **WHEN** a GET request is made to `/api/sessions` and no sessions with the `claude-` prefix exist
- **THEN** the response SHALL render an empty state

### Requirement: Session discovery — global scope via metadata sidecars
The system SHALL discover sessions by scanning the PID sidecars in the metadata directory, filtering to sessions whose name starts with `claude-`, and probing liveness via `signal 0` on the recorded PID. The working directory and creation time SHALL be read from each session's JSON metadata sidecar. The system SHALL NOT read `~/.claude/sessions/*.json` files and SHALL NOT run `tmux list-sessions`.

#### Scenario: Multiple sessions exist
- **WHEN** the system scans the metadata directory and finds sidecars for live processes `claude-a1b2c3d4` and `claude-e5f6g7h8`
- **THEN** the system SHALL return both `claude-` prefixed sessions with their creation time and working directory

#### Scenario: Session process no longer alive
- **WHEN** a `signal 0` liveness probe on a session's recorded PID fails
- **THEN** the system SHALL treat that session as dead, unlink its socket and sidecars, and exclude it from the returned list

### Requirement: Spawn a new Claude Code session
The system SHALL expose a `POST /api/sessions` endpoint to create a new interactive Claude Code session in a specified directory, accepting a JSON body `{"cwd": ...}`. Sessions SHALL be created only from the dashboard; direct CLI `claude` is disabled inside the container. The session SHALL be spawned as a detached dtach master (`dtach -n`) running `claude --dangerously-skip-permissions`. The session name SHALL follow the pattern `claude-<hex>` (8 hex characters). The working directory MUST exist inside the container and MUST be under `/workspace`. The spawned session name SHALL be returned as the terminal ID for WebSocket attachment.

#### Scenario: Valid directory
- **WHEN** a POST request is made to `/api/sessions` with body `{"cwd":"/workspace/my-project"}`
- **THEN** the system SHALL spawn a detached dtach session running claude in the specified directory, publish an SSE update event, and return a JSON body `{"session_name": ...}` whose value is the `claude-<hex>` terminal ID for WebSocket attachment

#### Scenario: Directory does not exist
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` that does not exist
- **THEN** the system SHALL respond with HTTP 400 and an error message

#### Scenario: Directory outside /workspace
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` outside `/workspace`
- **THEN** the system SHALL respond with HTTP 400 and an error message indicating the directory must be under `/workspace`

### Requirement: Kill a Claude Code session
The system SHALL expose a `DELETE /api/sessions/{terminalId}` endpoint to terminate a running session by its session name. The system SHALL read the session's PID sidecar and signal the inner process group to terminate the session. All running `claude-` prefixed sessions SHALL be killable.

#### Scenario: Kill a session
- **WHEN** a DELETE request is made to `/api/sessions/{terminalId}` for a running session
- **THEN** the system SHALL signal the inner process group via the PID sidecar, publish an SSE update event, and respond with HTTP 200

#### Scenario: Kill a non-existent session
- **WHEN** a DELETE request is made to `/api/sessions/{terminalId}` for a session name that does not exist
- **THEN** the system SHALL respond with HTTP 404

### Requirement: List directories under /workspace
The system SHALL expose an endpoint to browse directories inside `/workspace` for use by the directory picker when spawning new sessions.

#### Scenario: List top-level workspace directories
- **WHEN** a GET request is made to `/api/directories`
- **THEN** the response SHALL render directory names directly under `/workspace`

#### Scenario: List subdirectories
- **WHEN** a GET request is made to `/api/directories?path=subdir`
- **THEN** the response SHALL render directory names inside `/workspace/subdir`

#### Scenario: Path traversal attempt
- **WHEN** a GET request is made to `/api/directories?path=../../etc`
- **THEN** the system SHALL respond with HTTP 400 and reject the request

### Requirement: Upload an image to a session
The system SHALL expose a `POST /api/sessions/{terminalId}/upload` endpoint that accepts a multipart form image (field `image`) and saves it to a per-session upload directory, returning the saved path as JSON. The upload size MUST NOT exceed 10 MB, and only `image/png`, `image/jpeg`, `image/gif`, and `image/webp` content types SHALL be accepted. The terminalId MUST NOT contain path-traversal characters, and the target session MUST have an active relay.

#### Scenario: Valid image upload
- **WHEN** a POST request with a PNG image in the `image` field is made to `/api/sessions/claude-a1b2c3d4/upload` for a session with an active relay
- **THEN** the system SHALL save the file under the session's upload directory and respond with HTTP 200 and a JSON body containing the saved `path`

#### Scenario: Unsupported content type
- **WHEN** a POST request uploads a file whose content type is not one of the allowed image types
- **THEN** the system SHALL respond with HTTP 400 and an error message

#### Scenario: Upload to a nonexistent session
- **WHEN** a POST request targets a session that has no active relay
- **THEN** the system SHALL respond with HTTP 404

### Requirement: Relay tracks last activity timestamp
The `Relay` struct SHALL track when the session last produced output via a `lastActivity time.Time` field protected by `lastActivityMu sync.RWMutex`. The timestamp SHALL be updated in `processOutput` only when there are normal-mode segments AND no resize occurred within the last 2 seconds AND no input was sent within the last 500ms (to suppress resize redraws and keystroke echoes). A `GetLastActivity() time.Time` getter SHALL safely return the timestamp.

#### Scenario: Real output triggers activity update
- **WHEN** the relay processes output with normal-mode segments, more than 2 seconds after the last resize and more than 500ms after the last input
- **THEN** the relay SHALL update `lastActivity` to the current time

#### Scenario: Resize redraw does not trigger activity
- **WHEN** the relay processes output within 2 seconds of a resize
- **THEN** the relay SHALL NOT update `lastActivity`

#### Scenario: Keystroke echo does not trigger activity
- **WHEN** the relay processes output within 500ms of user input
- **THEN** the relay SHALL NOT update `lastActivity`

### Requirement: Session list includes display name
The `ListSessions` method SHALL enrich each `DisplaySession` with a `DisplayName string` field computed from the SessionManager's name map (custom name if set, otherwise `DirName`). Enrichment SHALL happen on every `ListSessions` call, not just on cache miss.

#### Scenario: Session with custom name
- **WHEN** a session has a custom name set via `SetSessionName`
- **THEN** `DisplaySession.DisplayName` SHALL be the custom name

#### Scenario: Session without custom name
- **WHEN** a session has no custom name
- **THEN** `DisplaySession.DisplayName` SHALL be `DirName` (directory basename)

### Requirement: Session rename endpoint
The system SHALL provide a `PUT /api/sessions/{terminalId}/name` endpoint that accepts a JSON body `{"name":"..."}` and updates the session's display name in memory. An empty name SHALL clear the custom name. After updating, the system SHALL publish an SSE event so all clients refresh. If the session does not exist, the server SHALL return HTTP 404. Session names SHALL be cleaned up from the map when sessions are removed in `syncRelays`.

#### Scenario: Set a custom name
- **WHEN** a PUT request with `{"name":"my-project"}` is sent to `/api/sessions/claude-abc123/name`
- **THEN** the session's display name SHALL be "my-project" and an SSE update SHALL be published

#### Scenario: Clear a custom name
- **WHEN** a PUT request with `{"name":""}` is sent
- **THEN** the custom name SHALL be removed and the session SHALL revert to showing its directory basename

#### Scenario: Rename nonexistent session
- **WHEN** a PUT request targets a session that does not exist
- **THEN** the server SHALL return HTTP 404

### Requirement: Health check endpoint
The system SHALL expose a `GET /healthz` endpoint that returns HTTP 200 when the dashboard server is running and able to accept requests. This endpoint is used by Docker's health check mechanism to detect unresponsive containers.

#### Scenario: Server is healthy
- **WHEN** a GET request is made to `/healthz`
- **THEN** the server SHALL respond with HTTP 200 and a JSON body `{"status":"ok"}`

#### Scenario: Server is unresponsive
- **WHEN** the Go HTTP server has stopped accepting connections or is deadlocked
- **THEN** the `/healthz` request SHALL time out, and Docker's health check SHALL mark the container as unhealthy after the configured number of retries

#### Scenario: Health check does not require authentication
- **WHEN** a GET request is made to `/healthz` from any origin
- **THEN** the server SHALL respond without requiring any authentication headers or cookies
