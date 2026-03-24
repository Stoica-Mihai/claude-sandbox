## ADDED Requirements

### Requirement: Relay tracks last activity timestamp
The `Relay` struct SHALL track when the session last produced output via a `lastActivity time.Time` field protected by `lastActivityMu sync.RWMutex`. The timestamp SHALL be updated in `processOutput` only when there are normal-mode segments AND no resize occurred within the last 2 seconds AND no input was sent within the last 500ms (to suppress resize redraws and keystroke echoes). A `GetLastActivity() time.Time` getter SHALL safely return the timestamp.

#### Scenario: Real output triggers activity update
- **WHEN** the relay processes output with normal-mode segments, more than 2 seconds after the last resize and more than 500ms after the last input
- **THEN** the relay SHALL update `lastActivity` to the current time

#### Scenario: Resize redraw does not trigger activity
- **WHEN** the relay processes output within 2 seconds of a tmux resize
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
