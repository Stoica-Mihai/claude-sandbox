# session-api Delta

## MODIFIED Requirements

### Requirement: List active Claude Code sessions across all directories
The system SHALL expose a `GET /api/sessions` endpoint that returns all currently running Claude Code sessions inside the sandbox, regardless of which working directory they were started in. The backend SHALL serve the list from its in-memory store, reconciled against sessiond's LIST op (event-driven on spawn/kill plus the periodic poll). The endpoint SHALL return session name, working directory, creation time, and display name.

#### Scenario: Sessions running in different directories
- **WHEN** a GET request is made to `/api/sessions` and sessions are running in `/workspace/api-service`, `/workspace/frontend`, and `/workspace/infra`
- **THEN** the response SHALL include all three sessions, each showing its respective working directory

#### Scenario: No sessions are running
- **WHEN** a GET request is made to `/api/sessions` and no sessions exist
- **THEN** the response SHALL render an empty state

### Requirement: Spawn a new Claude Code session
The system SHALL expose `POST /api/sessions` to create a session. The JSON body SHALL include `cwd` (a directory under `/workspace`) and MAY include `resume` (a conversation uuid). When `resume` is absent the system SHALL start a new conversation (`claude --session-id <new-uuid>`). When `resume` is present the system SHALL reopen that conversation (`claude --resume <uuid>`) in its recorded cwd. The backend SHALL validate the cwd and uuid, delegate the spawn to sessiond, record the index entry, and return the session name for WebSocket attachment.

#### Scenario: Start a new session
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux"}`
- **THEN** the system SHALL spawn a new conversation via sessiond and return its session name

#### Scenario: Resume a previous session
- **WHEN** `POST /api/sessions` is called with `{"cwd":"/workspace/cmux","resume":"<uuid>"}`
- **THEN** the system SHALL spawn a session running `claude --resume <uuid>` in that conversation's recorded cwd and return its session name

### Requirement: Kill a Claude Code session
The system SHALL expose a `DELETE /api/sessions/{terminalId}` endpoint to terminate a running session by its session name. The backend SHALL delegate the kill to sessiond, which signals the session's process group. All running `claude-` prefixed sessions SHALL be killable.

#### Scenario: Kill a session
- **WHEN** a DELETE request is made to `/api/sessions/{terminalId}` for a running session
- **THEN** the system SHALL kill the session via sessiond, publish an SSE update event, and respond with success

#### Scenario: Kill a non-existent session
- **WHEN** a DELETE request is made to `/api/sessions/{terminalId}` for a session name that does not exist
- **THEN** the system SHALL respond with HTTP 404

### Requirement: Delete a session from history
The system SHALL expose a `DELETE /api/sessions/history/{uuid}` endpoint that permanently and irreversibly deletes a recorded conversation, keyed by its claude conversation **uuid**. This endpoint is DISTINCT from the kill route `DELETE /api/sessions/{terminalId}` (keyed by the live session name) and SHALL NOT replace or alter it. The handler SHALL call `SessionManager.DeleteHistory(uuid)`, then publish an SSE update via the broker, and SHALL respond with HTTP 204 on success or HTTP 404 when the uuid is not present in the dashboard session index. The frontend's proxy SHALL forward the route to the backend and pass its status through (204/404).

`SessionManager.DeleteHistory(uuid)` SHALL perform the following in this exact order:
1. Membership check — if the uuid is not present in the dashboard session index, return an error (and the handler maps this to 404); no kill and no transcript deletion SHALL occur.
2. Live-kill — if the backend's session store holds a live session whose conversation uuid equals the target, kill it first via the existing kill path keyed by session name. If no live session matches, skip the kill.
3. Remove the index entry via `SessionIndex.remove(uuid)`.
4. Delete the transcript file(s) via `deleteTranscript(uuid)`.

Deletion SHALL remove BOTH the dashboard index entry in `dashboard-sessions.json` AND every transcript file matching `projects/*/<uuid>.jsonl` under `$CLAUDE_CONFIG_DIR`.

#### Scenario: Delete a dead history-only conversation
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid present in the index with no matching live session
- **THEN** the system SHALL skip the kill, remove the index entry, delete every matching `projects/*/<uuid>.jsonl` transcript file, publish an SSE update, and respond with HTTP 204

#### Scenario: Delete a conversation that is currently live
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid whose conversation is running as a live session
- **THEN** the system SHALL first kill that live session, then remove the index entry, delete the transcript file(s), publish an SSE update, and respond with HTTP 204

#### Scenario: Delete an unknown conversation
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid that is not present in the dashboard session index
- **THEN** the system SHALL NOT kill any session and SHALL NOT delete any transcript file, and SHALL respond with HTTP 404

#### Scenario: Kill route is unaffected
- **WHEN** `DELETE /api/sessions/{terminalId}` is called for a running session after the history-delete route exists
- **THEN** the system SHALL kill that session by its name exactly as before, independent of the history-delete route

## REMOVED Requirements

### Requirement: Session discovery — global scope via metadata sidecars
**Reason**: Metadata/PID sidecars no longer exist; sessiond answers LIST from live child-process handles.
**Migration**: Backend-side reconciliation: dtach-sessions "Periodic session list polling" (modified); sessiond-side: session-host "Clean-slate boot".

### Requirement: Relay tracks last activity timestamp
**Reason**: Stale spec — the current relay implements no `lastActivity` tracking; nothing consumes it. Removed rather than ported.
**Migration**: None. If activity indicators are wanted later, they belong in the session-host protocol as a new requirement.
