# session-api Specification

## Purpose
HTTP API for managing Claude Code sessions — listing, spawning, killing, and browsing directories.
## Requirements
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

### Requirement: Session list includes display name
The `ListSessions` method SHALL enrich each `DisplaySession` with a `DisplayName string` field computed from the SessionManager's name map (custom name if set, otherwise `DirName`). Enrichment SHALL happen on every `ListSessions` call, not just on cache miss.

#### Scenario: Session with custom name
- **WHEN** a session has a custom name set via `SetSessionName`
- **THEN** `DisplaySession.DisplayName` SHALL be the custom name

#### Scenario: Session without custom name
- **WHEN** a session has no custom name
- **THEN** `DisplaySession.DisplayName` SHALL be `DirName` (directory basename)

### Requirement: Session rename endpoint
The system SHALL expose `PUT /api/sessions/{terminalId}/name` to set or clear a session's custom name. The name SHALL be stored in the session index keyed by the session's conversation uuid (resolved from the live session's metadata sidecar), so it persists and appears in both the sidebar and the resume list. Clearing the name SHALL remove it.

#### Scenario: Rename persists by conversation uuid
- **WHEN** a live session is renamed to "relay fixes"
- **THEN** the system SHALL set the name on that conversation's index entry, and the name SHALL appear in the sidebar and in that folder's resume list

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

### Requirement: Session history endpoint
The system SHALL expose `GET /api/sessions/history?cwd=<path>` returning the previous sessions recorded for that working directory, as a JSON array of `{uuid, created, name}` sorted by creation time descending. The list SHALL come from the dashboard session index, not from claude's transcript files.

#### Scenario: List a folder's previous sessions
- **WHEN** `GET /api/sessions/history?cwd=/workspace/cmux` is called and three sessions were created there
- **THEN** the system SHALL return those three entries (uuid, created, optional name), newest first

#### Scenario: Folder with no history
- **WHEN** `GET /api/sessions/history?cwd=/workspace/empty` is called and no sessions were created there
- **THEN** the system SHALL return an empty array

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

### Requirement: Session index supports entry removal
The `SessionIndex` SHALL provide a `remove(uuid)` method that acquires the index mutex, deletes the entry for that uuid, and persists the index to disk via `save()`. Removal of a uuid that is not present SHALL be a no-op (no error).

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

### Requirement: Create a new directory under /workspace
The system SHALL expose a `POST /api/directories` endpoint that creates a single new directory beneath a parent path inside `/workspace`, for use by the directory picker when starting a session in a new project. The JSON request body SHALL be `{ "path": <parent-relative-path>, "name": <new-folder-name>, "gitInit": <bool> }`, where `path` is the currently browsed directory relative to `/workspace` (empty string = the workspace root) and `name` is the folder to create.

The endpoint SHALL validate `name` against the regular expression `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` — a single path segment of 1–64 characters, beginning with an alphanumeric character (no leading dot or dash), containing only ASCII letters, digits, `.`, `_`, and `-`, and therefore no path separators and no `..`. This regex is the single authoritative validation gate for the name; the endpoint SHALL NOT apply any additional name-specific rejection beyond it.

The endpoint SHALL resolve and prefix-check the **parent** path using the same logic as `GET /api/directories`: it SHALL compute `filepath.Join(workspaceRoot, path)`, take `filepath.Abs`, and require the result to be `workspaceRoot` itself or a path beneath it. Before creating the directory, it SHALL perform an explicit parent-existence check (`os.Stat` on the resolved parent, requiring a directory) so that a missing parent is classified as a client error and never reaches the create call.

On a valid request the endpoint SHALL create the directory with `os.Mkdir` at mode `0o755` (not `os.MkdirAll` — the parent MUST already exist). When `gitInit` is true, it SHALL then run `git -C <new-dir> init`. The umask-adjusted on-disk mode is acceptable; the endpoint SHALL NOT issue a follow-up `os.Chmod`.

The endpoint SHALL respond as follows:
- **201 Created** on success (directory created; `git init` succeeded or was not requested).
- **201 Created with a `warning` field** when the directory was created but `gitInit` was requested and `git init` failed. The directory SHALL be kept (not rolled back), and the `warning` field SHALL carry a human-readable notice.
- **400 Bad Request** when the name fails the regex (`"Invalid name"`), when the resolved parent path is not under `/workspace` (`"invalid path"`), or when the parent path does not exist / is not a directory (`"directory not found"`). The `"invalid path"` and `"directory not found"` messages SHALL be byte-identical to those returned by `GET /api/directories`.
- **409 Conflict** when the target directory already exists (`os.Mkdir` returns an `os.ErrExist` error), with the message `"Folder already exists"`.
- **500 Internal Server Error** for any other `os.Mkdir` failure, with a plain message (`"failed to create directory"`).

#### Scenario: Create a folder at the workspace root
- **WHEN** a POST request is made to `/api/directories` with body `{"path":"","name":"relay-visualizer","gitInit":false}` and `/workspace/relay-visualizer` does not exist
- **THEN** the system SHALL create `/workspace/relay-visualizer` with `os.Mkdir` at mode `0o755` and respond with HTTP 201

#### Scenario: Create a folder inside a browsed subdirectory
- **WHEN** a POST request is made to `/api/directories` with body `{"path":"experiments","name":"new-idea","gitInit":true}` and `/workspace/experiments` exists
- **THEN** the system SHALL create `/workspace/experiments/new-idea`, run `git -C /workspace/experiments/new-idea init`, and respond with HTTP 201

#### Scenario: git init fails after the folder is created
- **WHEN** a POST request creates the directory successfully but the subsequent `git init` fails
- **THEN** the system SHALL keep the created directory (no rollback) and respond with HTTP 201 including a `warning` field describing the git-init failure

#### Scenario: Invalid folder name
- **WHEN** a POST request is made to `/api/directories` with a `name` that fails `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` (for example `".."`, `"a/b"`, `".hidden"`, an empty string, or a 65-character name)
- **THEN** the system SHALL reject the request before any filesystem call and respond with HTTP 400 and the error `"Invalid name"`

#### Scenario: Parent path escapes the workspace
- **WHEN** a POST request is made to `/api/directories` with a `path` whose resolved absolute form is not `workspaceRoot` and not beneath it (for example `path="../../etc"`)
- **THEN** the system SHALL respond with HTTP 400 and the error `"invalid path"`

#### Scenario: Parent directory does not exist
- **WHEN** a POST request is made to `/api/directories` with a regex-valid `name` and a `path` that resolves under `/workspace` but does not exist on disk (for example `path="nope-does-not-exist"`)
- **THEN** the explicit pre-create parent-existence check SHALL reject the request with HTTP 400 and the error `"directory not found"`, without attempting to create anything

#### Scenario: Target folder already exists
- **WHEN** a POST request is made to `/api/directories` for a `name` whose target directory already exists under the resolved parent
- **THEN** `os.Mkdir` SHALL return an `os.ErrExist` error and the system SHALL respond with HTTP 409 and the error `"Folder already exists"`

### Requirement: Frontend mirrors the create-directory route as a per-route proxy
The frontend service SHALL register `POST /api/directories` as an explicit per-route proxy to the backend, consistent with how every other `/api/*` route is mirrored in the frontend. The proxy SHALL forward the JSON request body and pass the backend's status code and response body through unchanged so the client observes the backend's authoritative `201`/`201-with-warning`/`400`/`409`/`500` result.

#### Scenario: Frontend forwards a create request to the backend
- **WHEN** the dashboard client sends `POST /api/directories` with a JSON body to the frontend
- **THEN** the frontend SHALL forward the request to the backend's `POST /api/directories` and relay the backend's status code and body back to the client without altering them

