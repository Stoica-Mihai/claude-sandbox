## ADDED Requirements

### Requirement: Delete a session from history
The system SHALL expose a `DELETE /api/sessions/history/{uuid}` endpoint that permanently and irreversibly deletes a recorded conversation, keyed by its claude conversation **uuid**. This endpoint is DISTINCT from the existing kill route `DELETE /api/sessions/{terminalId}` (keyed by the live dtach/socket name) and SHALL NOT replace or alter it. The handler SHALL call `SessionManager.DeleteHistory(uuid)`, then publish an SSE update via the broker, and SHALL respond with HTTP 204 on success or HTTP 404 when the uuid is not present in the dashboard session index.

`SessionManager.DeleteHistory(uuid)` SHALL perform the following in this exact order:
1. Membership check — if the uuid is not present in the dashboard session index, return an error (and the handler maps this to 404); no kill and no transcript deletion SHALL occur.
2. Live-kill — iterate `discoverSessions()` (NOT `ListSessions()`) and, if an entry's metadata `SessionID` equals the uuid, kill that live session first via the existing `Kill(sessionName)` path keyed by the dtach session name. If no live session matches, skip the kill.
3. Remove the index entry via `SessionIndex.remove(uuid)`.
4. Delete the transcript file(s) via `deleteTranscript(uuid)`.

Deletion SHALL remove BOTH the dashboard index entry in `dashboard-sessions.json` AND every transcript file matching `projects/*/<uuid>.jsonl` under `$CLAUDE_CONFIG_DIR`.

#### Scenario: Delete a dead history-only conversation
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid present in the index with no matching live session
- **THEN** the system SHALL skip the kill, remove the index entry, delete every matching `projects/*/<uuid>.jsonl` transcript file, publish an SSE update, and respond with HTTP 204

#### Scenario: Delete a conversation that is currently live
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid whose conversation is running as a live dtach session
- **THEN** the system SHALL first kill that live session via `Kill(sessionName)` resolved from `discoverSessions()` (the entry whose `SessionID` equals the uuid), then remove the index entry, delete the transcript file(s), publish an SSE update, and respond with HTTP 204

#### Scenario: Delete an unknown conversation
- **WHEN** `DELETE /api/sessions/history/{uuid}` is called for a uuid that is not present in the dashboard session index
- **THEN** the system SHALL NOT kill any session and SHALL NOT delete any transcript file, and SHALL respond with HTTP 404

#### Scenario: Kill route is unaffected
- **WHEN** `DELETE /api/sessions/{terminalId}` is called for a running session after the history-delete route exists
- **THEN** the system SHALL kill that session by its dtach name exactly as before, independent of the new history-delete route

### Requirement: Session index supports entry removal
The `SessionIndex` SHALL provide a `remove(uuid)` method that acquires the index mutex, deletes the entry for that uuid, and persists the index to disk via `save()`. Removal of a uuid that is not present SHALL be a no-op (no error, no spurious write requirement beyond the standard save).

#### Scenario: Remove deletes and persists
- **WHEN** `remove(uuid)` is called for a uuid present in the index
- **THEN** the entry SHALL be deleted from the in-memory map and the index SHALL be saved to disk so the removal survives reload

### Requirement: Transcript deletion helper
The system SHALL provide a `deleteTranscript(uuid)` helper that globs `filepath.Join(claudeConfigDir(), "projects", "*", uuid + ".jsonl")` (mirroring the existing `hasTranscript` glob) and removes each matching file. Absence of any match SHALL NOT be treated as an error.

#### Scenario: Removes matching transcript files
- **WHEN** `deleteTranscript(uuid)` is called and one or more `projects/*/<uuid>.jsonl` files exist under the claude config dir
- **THEN** each matching file SHALL be removed

#### Scenario: No transcript present
- **WHEN** `deleteTranscript(uuid)` is called and no matching transcript file exists
- **THEN** the helper SHALL complete without error
