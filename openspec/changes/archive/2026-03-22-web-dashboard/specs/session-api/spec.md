## ADDED Requirements

### Requirement: List active Claude Code sessions across all directories
The system SHALL expose an endpoint that returns all currently running Claude Code sessions inside the container, regardless of which working directory they were started in. Session discovery SHALL read `~/.claude/sessions/*.json` — this is a global directory where Claude Code registers all active sessions with their `pid`, `sessionId`, `cwd`, and `startedAt` fields. The endpoint SHALL cross-reference PIDs with live processes to determine liveness. The system SHALL also include managed sessions (spawned via the dashboard) that may not yet have registered a session file.

#### Scenario: Sessions running in different directories
- **WHEN** a GET request is made to `/fragments/sessions` and sessions are running in `/workspace/api-service`, `/workspace/frontend`, and `/workspace/infra`
- **THEN** the response SHALL include all three sessions, each showing its respective `cwd`, regardless of which directory the dashboard server was started from

#### Scenario: No sessions are running
- **WHEN** a GET request is made to `/fragments/sessions` and no Claude Code processes are active
- **THEN** the response SHALL render an empty state

#### Scenario: Stale session file exists
- **WHEN** a session JSON file exists in `~/.claude/sessions/` but the referenced PID is no longer running
- **THEN** the session SHALL be included with `alive` set to `false` so the UI can display it as dead/stale

#### Scenario: Managed session not yet registered
- **WHEN** the dashboard spawns a new session via PTY but Claude Code has not yet written its `~/.claude/sessions/<pid>.json` file
- **THEN** the session SHALL still appear in the list as a managed session (tracked in the dashboard's in-memory map) with its `cwd` from the spawn request

### Requirement: Session discovery — global scope via ~/.claude/sessions/
The system SHALL discover sessions by reading all `*.json` files in `~/.claude/sessions/`. Each file is named `<pid>.json` and contains:
```json
{"pid": 1842, "sessionId": "uuid", "cwd": "/workspace/api-service", "startedAt": 1774179864188}
```
The `cwd` field SHALL be used to display which directory the session belongs to. This directory is global — Claude Code writes session files here for every active session regardless of project scope. The system SHALL NOT attempt to read project-scoped session data from `~/.claude/projects/` as those are historical transcripts, not active session indicators.

#### Scenario: Multiple sessions in ~/.claude/sessions/
- **WHEN** the system reads `~/.claude/sessions/` and finds `1842.json`, `2103.json`, `987.json`
- **THEN** the system SHALL parse each file's `pid`, `sessionId`, `cwd`, and `startedAt` fields and check each PID for liveness via `syscall.Kill(pid, 0)`

#### Scenario: Session file is malformed
- **WHEN** a session file cannot be parsed as valid JSON
- **THEN** the system SHALL skip that file and log a warning

### Requirement: Merge managed and detected sessions
The system SHALL maintain two session sources and merge them for display:
1. **Managed sessions**: PTYs spawned through the dashboard, tracked in-memory with full terminal access (terminalId, PTY file descriptor, scrollback buffer, WebSocket attachment)
2. **Detected sessions**: Sessions found in `~/.claude/sessions/*.json` that were started via `docker exec` or other means, displayed as read-only with no terminal attachment

When a managed session also appears in `~/.claude/sessions/` (matched by PID), the system SHALL merge them into a single entry with both the managed terminal access and the Claude-provided session metadata (sessionId).

#### Scenario: Dashboard-spawned session registers with Claude
- **WHEN** the dashboard spawns a session (PID 1842) and Claude Code writes `~/.claude/sessions/1842.json`
- **THEN** the system SHALL merge the entries: the session appears once with both terminal access (managed) and the sessionId from Claude's file

#### Scenario: External session detected
- **WHEN** a session file exists for PID 445 but PID 445 was not spawned by the dashboard
- **THEN** the session SHALL appear in the list with an "external" badge and no terminal attachment option

### Requirement: Spawn a new Claude Code session
The system SHALL expose an HTTP endpoint to create a new interactive Claude Code session in a specified directory. The session SHALL be spawned inside a PTY via `pty.StartWithSize` with an initial size of 120x50, so Claude Code renders its startup UI correctly before the browser terminal connects. The `TERM` environment variable SHALL be set to `xterm-256color`. The working directory MUST exist inside the container and MUST be under `/workspace`.

#### Scenario: Valid directory
- **WHEN** a POST request is made to `/api/sessions` with `cwd=/workspace/my-project`
- **THEN** the system SHALL spawn a new `claude --dangerously-skip-permissions` process in a PTY with the specified working directory, add it to the managed session map, publish an SSE update event, and return a response containing the `terminalId` for WebSocket attachment

#### Scenario: Directory does not exist
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` that does not exist
- **THEN** the system SHALL respond with HTTP 400 and an error message

#### Scenario: Directory outside /workspace
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` outside `/workspace`
- **THEN** the system SHALL respond with HTTP 400 and an error message indicating the directory must be under `/workspace`

### Requirement: Kill a Claude Code session
The system SHALL expose an endpoint to terminate a running session by its terminal ID. The system SHALL send SIGTERM to the process and clean up the PTY. Only managed sessions (spawned by the dashboard) can be killed. External sessions SHALL not be killable from the dashboard.

#### Scenario: Kill a managed session
- **WHEN** a DELETE request is made to `/api/sessions/:terminalId` for a managed session
- **THEN** the system SHALL terminate the process, clean up the PTY and scrollback buffer, remove it from the managed map, publish an SSE update event, and respond with HTTP 200

#### Scenario: Kill a non-existent session
- **WHEN** a DELETE request is made to `/api/sessions/:terminalId` for an unknown terminal ID
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
