## ADDED Requirements

### Requirement: API provides endpoint to export session scrollback
The session API SHALL expose `GET /api/sessions/{terminalId}/export` which returns the full scrollback buffer of the specified session as plain text with `Content-Disposition: attachment` header.

#### Scenario: Valid session export
- **WHEN** a GET request is made to `/api/sessions/{terminalId}/export` for an existing active session
- **THEN** the response SHALL be 200 with `Content-Type: text/plain; charset=utf-8` and the scrollback buffer as the response body

#### Scenario: Session not found
- **WHEN** a GET request is made to `/api/sessions/{terminalId}/export` for a non-existent terminal ID
- **THEN** the response SHALL be 404
