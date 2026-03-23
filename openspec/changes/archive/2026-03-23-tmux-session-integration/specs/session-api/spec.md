## MODIFIED Requirements

### Requirement: List active Claude Code sessions across all directories
The system SHALL expose an endpoint that returns all currently running Claude Code sessions inside the container, regardless of which working directory they were started in. Session discovery SHALL run `tmux list-sessions` and filter to sessions with the `claude-` name prefix. The endpoint SHALL return session name, working directory, creation time, and whether a dashboard WebSocket is currently attached.

#### Scenario: Sessions running in different directories
- **WHEN** a GET request is made to `/fragments/sessions` and tmux sessions are running in `/workspace/api-service`, `/workspace/frontend`, and `/workspace/infra`
- **THEN** the response SHALL include all three sessions, each showing its respective working directory

#### Scenario: No sessions are running
- **WHEN** a GET request is made to `/fragments/sessions` and no tmux sessions with the `claude-` prefix exist
- **THEN** the response SHALL render an empty state

### Requirement: Session discovery — global scope via tmux list-sessions
The system SHALL discover sessions by running `tmux list-sessions -F "#{session_name}|#{session_created}|#{pane_current_path}"` and filtering to sessions whose name starts with `claude-`. The `pane_current_path` field SHALL be used to display which directory the session belongs to. The system SHALL NOT read `~/.claude/sessions/*.json` files.

#### Scenario: Multiple tmux sessions exist
- **WHEN** the system runs `tmux list-sessions` and finds `claude-a1b2c3d4`, `claude-e5f6g7h8`, and `user-shell`
- **THEN** the system SHALL return only the two `claude-` prefixed sessions with their creation time and working directory

#### Scenario: tmux server not running
- **WHEN** `tmux list-sessions` fails with "no server running"
- **THEN** the system SHALL treat this as zero sessions and return an empty list

### Requirement: Spawn a new Claude Code session
The system SHALL expose an HTTP endpoint to create a new interactive Claude Code session in a specified directory. The session SHALL be spawned inside a tmux session via `tmux new-session -d -s <name> -c <cwd> -- claude --dangerously-skip-permissions`. The working directory MUST exist inside the container and MUST be under `/workspace`. The tmux session name SHALL be returned as the terminal ID for WebSocket attachment.

#### Scenario: Valid directory
- **WHEN** a POST request is made to `/api/sessions` with `cwd=/workspace/my-project`
- **THEN** the system SHALL create a tmux session running claude in the specified directory, publish an SSE update event, and return a response containing the session name (as `X-Terminal-Id` header) for WebSocket attachment

#### Scenario: Directory does not exist
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` that does not exist
- **THEN** the system SHALL respond with HTTP 400 and an error message

#### Scenario: Directory outside /workspace
- **WHEN** a POST request is made to `/api/sessions` with a `cwd` outside `/workspace`
- **THEN** the system SHALL respond with HTTP 400 and an error message indicating the directory must be under `/workspace`

### Requirement: Kill a Claude Code session
The system SHALL expose an endpoint to terminate a running session by its tmux session name. The system SHALL run `tmux kill-session -t <name>` to terminate the session. All sessions (whether spawned from the dashboard or CLI) SHALL be killable.

#### Scenario: Kill a session
- **WHEN** a DELETE request is made to `/api/sessions/:sessionName` for a running tmux session
- **THEN** the system SHALL run `tmux kill-session -t <name>`, publish an SSE update event, and respond with HTTP 200

#### Scenario: Kill a non-existent session
- **WHEN** a DELETE request is made to `/api/sessions/:sessionName` for a session name that does not exist in tmux
- **THEN** the system SHALL respond with HTTP 404

## REMOVED Requirements

### Requirement: Merge managed and detected sessions
**Reason**: Replaced by unified tmux session model. There is no longer a distinction between managed and detected sessions — all sessions are tmux sessions discovered via `tmux list-sessions`.
**Migration**: All session discovery goes through `tmux list-sessions` with `claude-` prefix filtering. The in-memory managed map and `~/.claude/sessions/*.json` file scanning are removed.

### Requirement: Session discovery — global scope via ~/.claude/sessions/
**Reason**: Replaced by tmux-based session discovery. The `~/.claude/sessions/*.json` files are no longer read.
**Migration**: Use `tmux list-sessions -F` for session discovery instead.
