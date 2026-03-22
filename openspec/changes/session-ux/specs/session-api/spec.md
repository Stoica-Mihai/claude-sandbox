## MODIFIED Requirements

### Requirement: Track last activity timestamp per managed session
The `ManagedSession` struct SHALL include a `LastActivity time.Time` field that records the most recent time PTY output was read. The `readPTY` goroutine SHALL update `LastActivity` to `time.Now()` each time it successfully reads bytes from the PTY (inside the `if n > 0` block). The `DisplaySession` struct SHALL include `LastActivity time.Time` and a pre-formatted `LastActiveStr string` field for template rendering. The `ListSessions` method SHALL populate `LastActiveStr` with a human-readable relative time (e.g., "2s ago", "1m ago") for managed sessions, and leave it empty for external sessions.

#### Scenario: Activity timestamp updates on PTY read
- **WHEN** the `readPTY` goroutine reads bytes from the PTY for a managed session
- **THEN** the session's `LastActivity` field SHALL be updated to the current time

#### Scenario: Last active appears in session list
- **WHEN** `ListSessions` builds a `DisplaySession` for a managed session with `LastActivity` set
- **THEN** the `LastActiveStr` field SHALL contain a human-readable relative time string (e.g., "3s ago", "2m ago") computed from `time.Now() - LastActivity`

#### Scenario: No activity yet
- **WHEN** a managed session has just been spawned and `readPTY` has not yet read any bytes
- **THEN** `LastActivity` SHALL be zero-valued and `LastActiveStr` SHALL be empty or "idle"

#### Scenario: External session has no activity data
- **WHEN** `ListSessions` builds a `DisplaySession` for an external (non-managed) session
- **THEN** `LastActivity` SHALL be zero-valued and `LastActiveStr` SHALL be empty

### Requirement: Optional session name field
The `ManagedSession` struct SHALL include a `Name string` field for an optional user-assigned label. The `DisplaySession` struct SHALL include a `Name string` field and a `DisplayName string` field. `DisplayName` SHALL be `Name` if non-empty, otherwise `DirName` (the directory basename). The `ListSessions` method SHALL populate both fields when building display sessions. The name SHALL be stored in-memory only — it is lost when the dashboard process restarts.

#### Scenario: Session with custom name
- **WHEN** a managed session has `Name` set to "backend refactor"
- **THEN** the `DisplaySession` SHALL have `Name` = "backend refactor" and `DisplayName` = "backend refactor"

#### Scenario: Session without custom name
- **WHEN** a managed session has `Name` set to "" (empty)
- **THEN** the `DisplaySession` SHALL have `Name` = "" and `DisplayName` = the directory basename (e.g., "my-project")

#### Scenario: External session display name
- **WHEN** an external session is rendered as a `DisplaySession`
- **THEN** `Name` SHALL be "" and `DisplayName` SHALL be the directory basename

### Requirement: Session name update endpoint
The system SHALL expose a `PUT /api/sessions/:terminalId/name` endpoint that accepts a JSON body `{"name": "string"}` and updates the in-memory `Name` field on the corresponding `ManagedSession`. After updating, the endpoint SHALL publish an SSE update event so all connected clients refresh the session list. The endpoint SHALL respond with HTTP 200 on success. If the terminal ID is not found or does not refer to a managed session, the endpoint SHALL respond with HTTP 404.

#### Scenario: Rename a managed session
- **WHEN** a PUT request is made to `/api/sessions/abc123/name` with body `{"name": "backend refactor"}`
- **THEN** the system SHALL update the managed session's `Name` field, publish an SSE event, and respond with HTTP 200

#### Scenario: Clear a session name
- **WHEN** a PUT request is made to `/api/sessions/abc123/name` with body `{"name": ""}`
- **THEN** the system SHALL clear the `Name` field (reverting display to directory basename), publish an SSE event, and respond with HTTP 200

#### Scenario: Rename a non-existent session
- **WHEN** a PUT request is made to `/api/sessions/unknown/name`
- **THEN** the system SHALL respond with HTTP 404
