# session-api Delta

## MODIFIED Requirements

### Requirement: List active Claude Code sessions across all directories
The system SHALL expose a `GET /api/sessions` endpoint that returns all currently running Claude Code sessions inside the sandbox, regardless of which working directory they were started in. The backend SHALL serve the list from its in-memory store, reconciled against sessiond's LIST op (event-driven on spawn/kill plus the periodic poll). The endpoint SHALL return session name, working directory, creation time, display name, and session kind (`terminal` or `chat`).

#### Scenario: Sessions running in different directories
- **WHEN** a GET request is made to `/api/sessions` and sessions are running in `/workspace/api-service`, `/workspace/frontend`, and `/workspace/infra`
- **THEN** the response SHALL include all three sessions, each showing its respective working directory

#### Scenario: No sessions are running
- **WHEN** a GET request is made to `/api/sessions` and no sessions exist
- **THEN** the response SHALL render an empty state

#### Scenario: Session list distinguishes kind
- **WHEN** a GET request is made to `/api/sessions` and one session is a chat session while another is a terminal session
- **THEN** each entry in the response SHALL report its own `kind`

### Requirement: Spawn a new Claude Code session
The system SHALL expose `POST /api/sessions` to create a session. The JSON body SHALL include `cwd` (a directory under `/workspace`), MAY include `resume` (a conversation uuid), and MAY include `kind` (`terminal` or `chat`; absent or empty defaults to `terminal`). When `resume` is absent the system SHALL start a new conversation of the requested kind (`claude --session-id <new-uuid>` under a PTY for `terminal`, or the stream-json pipe invocation for `chat`). When `resume` is present the system SHALL reopen that conversation in its recorded cwd, as the requested kind (`claude --resume <uuid>`, PTY or pipe per `kind`) — the requested kind MAY differ from the kind the conversation last ran as (mode switch). The backend SHALL validate the cwd, uuid, and kind, delegate the spawn to sessiond, record the index entry (unchanged by kind — see `session-host`), and return the session name for WebSocket attachment.

#### Scenario: Start a new terminal session (default)
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux"}`
- **THEN** the system SHALL spawn a new terminal conversation via sessiond and return its session name

#### Scenario: Start a new chat session
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux","kind":"chat"}`
- **THEN** the system SHALL spawn a new chat conversation via sessiond and return its session name

#### Scenario: Resume a previous session in its original kind
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux","resume":"<uuid>","kind":"terminal"}`
- **THEN** the system SHALL spawn a session running `claude --resume <uuid>` under a PTY in that conversation's recorded cwd and return its session name

#### Scenario: Resume a previous session in the other kind (mode switch)
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux","resume":"<uuid>","kind":"chat"}` for a conversation that last ran as a terminal session
- **THEN** the system SHALL spawn a chat session running `claude --resume <uuid>` in stream-json mode in that conversation's recorded cwd and return its session name

#### Scenario: Invalid kind is rejected
- **WHEN** `POST /api/sessions` is called with a `kind` value other than `terminal` or `chat`
- **THEN** the system SHALL respond with HTTP 400

## ADDED Requirements

### Requirement: Session transcript endpoint
The system SHALL expose `GET /api/sessions/{terminalId}/transcript` returning the named session's conversation transcript (the claude-recorded `.jsonl` history for its conversation uuid), for the chat UI to render history on open or reconnect without relying on a terminal-style snapshot. The endpoint SHALL resolve the session's conversation uuid from the live session store (or, if the session is not currently live, SHALL respond with HTTP 404) and locate the transcript the same way `hasTranscript`/`deleteTranscript` already do (glob `projects/*/<uuid>.jsonl` under the claude config dir).

#### Scenario: Fetch a live session's transcript
- **WHEN** `GET /api/sessions/{terminalId}/transcript` is called for a live session whose conversation has a recorded transcript
- **THEN** the system SHALL respond with HTTP 200 and the transcript content

#### Scenario: Transcript requested for an unknown session
- **WHEN** `GET /api/sessions/{terminalId}/transcript` is called for a `terminalId` that is not a live session
- **THEN** the system SHALL respond with HTTP 404

#### Scenario: Live session with no transcript yet
- **WHEN** `GET /api/sessions/{terminalId}/transcript` is called for a live session that has not yet exchanged any messages (no transcript file recorded)
- **THEN** the system SHALL respond with HTTP 200 and an empty transcript, not an error

### Requirement: Mode-switch endpoint
The system SHALL expose `POST /api/sessions/{terminalId}/mode` with a JSON body `{"kind": "terminal"|"chat"}` that kills the named live session and respawns its conversation as the requested kind, returning `SpawnResponse{session_name}` on success. The conversation's uuid SHALL NOT need to cross the wire for this operation — `DisplaySession.SessionID` is deliberately excluded from every other endpoint's response (`json:"-"`), so the backend SHALL resolve the uuid internally from the live session record rather than requiring the client to supply it. The backend SHALL wait (bounded) for the old child to actually leave sessiond's list before respawning, so the two processes never race on the same transcript file — the same ordering concern the history-delete endpoint already handles. The session index SHALL be unaffected (kind is a property of the live child, not the persisted conversation).

#### Scenario: Switch a terminal session to chat
- **WHEN** `POST /api/sessions/claude-abcd1234/mode` is called with `{"kind":"chat"}` for a live terminal session
- **THEN** the system SHALL kill that session, respawn its conversation as a chat session, and respond with HTTP 201 and the new session's name

#### Scenario: Switching to the kind already running is rejected
- **WHEN** `POST /api/sessions/{terminalId}/mode` is called with the kind the session is already running as
- **THEN** the system SHALL respond with HTTP 400 rather than needlessly killing and respawning

#### Scenario: Unknown session
- **WHEN** `POST /api/sessions/{terminalId}/mode` is called for a `terminalId` with no live session
- **THEN** the system SHALL respond with HTTP 400

#### Scenario: Invalid kind
- **WHEN** `POST /api/sessions/{terminalId}/mode` is called with a `kind` value other than `terminal` or `chat`
- **THEN** the system SHALL respond with HTTP 400
