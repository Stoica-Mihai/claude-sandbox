# Spec: session-api

**Spec Path:** `specs/session-api/spec.md`
**Change Type:** MODIFIED

---

## MODIFIED Requirements

### Requirement: Spawn a new Claude Code session
The system SHALL expose `POST /api/sessions` to create a session. The JSON body SHALL include `cwd` (a directory under `/workspace`) and MAY include `resume` (a conversation uuid). When `resume` is absent the system SHALL start a new conversation (`claude --session-id <new-uuid>`). When `resume` is present the system SHALL reopen that conversation (`claude --resume <uuid>`) in its recorded cwd. The response SHALL return the new dtach session name for WebSocket attachment.

#### Scenario: Start a new session
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux"}`
- **THEN** the system SHALL spawn a new conversation and return its session name

#### Scenario: Resume a previous session
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux","resume":"<uuid>"}`
- **THEN** the system SHALL spawn a dtach session running `claude --resume <uuid>` in `/workspace/cmux` and return its session name

### Requirement: Session rename endpoint
The system SHALL expose `PUT /api/sessions/{terminalId}/name` to set or clear a session's custom name. The name SHALL be stored in the session index keyed by the session's conversation uuid (resolved from the live session's metadata sidecar), so it persists and appears in both the sidebar and the resume list. Clearing the name SHALL remove it.

#### Scenario: Rename persists by conversation uuid
- **WHEN** a live session is renamed to "relay fixes"
- **THEN** the system SHALL set the name on that conversation's index entry, and the name SHALL appear in the sidebar and in that folder's resume list

## ADDED Requirements

### Requirement: Session history endpoint
The system SHALL expose `GET /api/sessions/history?cwd=<path>` returning the previous sessions recorded for that working directory, as a JSON array of `{uuid, created, name}` sorted by creation time descending. The list SHALL come from the dashboard session index, not from claude's transcript files.

#### Scenario: List a folder's previous sessions
- **WHEN** `GET /api/sessions/history?cwd=/workspace/cmux` is called and three sessions were created there
- **THEN** the system SHALL return those three entries (uuid, created, optional name), newest first

#### Scenario: Folder with no history
- **WHEN** `GET /api/sessions/history?cwd=/workspace/empty` is called and no sessions were created there
- **THEN** the system SHALL return an empty array
