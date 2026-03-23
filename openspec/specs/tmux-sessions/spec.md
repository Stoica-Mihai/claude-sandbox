# tmux-sessions Specification

## Purpose
tmux-based session lifecycle management — spawning Claude Code inside tmux, discovering sessions via `tmux list-sessions`, killing sessions, periodic polling for changes, session persistence across dashboard restarts, and CLI tmux wrapping.

## Requirements
### Requirement: Spawn Claude Code sessions inside tmux
The system SHALL spawn all Claude Code sessions inside tmux sessions using `tmux new-session -d -s <name> -c <cwd> -- claude --dangerously-skip-permissions`. The tmux session name SHALL follow the pattern `claude-<hex>` where `<hex>` is 8 random hexadecimal characters. The working directory MUST exist and MUST be under `/workspace`. If `tmux new-session` fails with a duplicate session name, the system SHALL retry with a new random name up to 3 times before returning an error.

#### Scenario: Spawn a new session
- **WHEN** a spawn request is made with `cwd=/workspace/my-project`
- **THEN** the system SHALL run `tmux new-session -d -s claude-<hex> -c /workspace/my-project -- claude --dangerously-skip-permissions`, and the tmux session SHALL appear in `tmux list-sessions` output

#### Scenario: Spawn with duplicate session name
- **WHEN** `tmux new-session` fails because a session with the generated name already exists
- **THEN** the system SHALL generate a new random name and retry, up to 3 attempts

#### Scenario: tmux server not running
- **WHEN** no tmux server is running and a spawn request is made
- **THEN** `tmux new-session` SHALL automatically start the tmux server and create the session

### Requirement: Discover sessions via tmux list-sessions
The system SHALL discover all Claude Code sessions by running `tmux list-sessions -F "#{session_name}|#{session_created}|#{pane_current_path}"` and filtering results to sessions whose name starts with `claude-`. The system SHALL NOT read `~/.claude/sessions/*.json` files for session discovery.

#### Scenario: Multiple sessions running
- **WHEN** `tmux list-sessions` returns sessions `claude-a1b2c3d4`, `claude-e5f6g7h8`, and `my-other-session`
- **THEN** the system SHALL return only the two `claude-` prefixed sessions with their creation time and working directory

#### Scenario: No tmux server running
- **WHEN** `tmux list-sessions` fails with exit code 1 ("no server running")
- **THEN** the system SHALL treat this as zero sessions and return an empty list without logging an error

#### Scenario: Session list caching
- **WHEN** multiple session list requests arrive within 2 seconds
- **THEN** the system SHALL return cached results from the first `tmux list-sessions` call. The cache SHALL be invalidated when a session is spawned or killed.

### Requirement: Kill sessions via tmux kill-session
The system SHALL terminate sessions by running `tmux kill-session -t <name>`. This SHALL terminate the claude process running inside the tmux session and destroy the tmux session. Any WebSocket connections attached to the session SHALL receive a close frame.

#### Scenario: Kill a running session
- **WHEN** a kill request is made for session `claude-a1b2c3d4`
- **THEN** the system SHALL run `tmux kill-session -t claude-a1b2c3d4`, the tmux session SHALL no longer appear in `tmux list-sessions`, and an SSE update event SHALL be published

#### Scenario: Kill a non-existent session
- **WHEN** a kill request is made for a session name that does not exist in tmux
- **THEN** the system SHALL respond with HTTP 404

### Requirement: Periodic session list polling
The system SHALL run a background goroutine that polls `tmux list-sessions` every 5 seconds and compares the result with the cached session list. If the list has changed (a session was added externally or exited on its own), the system SHALL update the cache and publish an SSE event to trigger a sidebar refresh in all connected clients. This ensures the dashboard detects session exits that happen without dashboard interaction (e.g., user typing `/exit` in claude, or CLI sessions ending).

#### Scenario: Session exits without dashboard interaction
- **WHEN** a user types `/exit` in a Claude Code session (from CLI or dashboard) and no dashboard action triggers a refresh
- **THEN** the polling goroutine SHALL detect the session's disappearance from `tmux list-sessions` within 5 seconds and publish an SSE update event

#### Scenario: Session created from CLI
- **WHEN** a user runs `claude` from the CLI, creating a new tmux session
- **THEN** the polling goroutine SHALL detect the new session within 5 seconds and publish an SSE update event

### Requirement: Session persistence across dashboard restarts
The system SHALL discover and list all running tmux sessions on startup. Sessions created before a dashboard restart SHALL be fully interactive — users SHALL be able to attach to them via WebSocket and see the current terminal state.

#### Scenario: Dashboard restarts with running sessions
- **WHEN** the dashboard process restarts and tmux sessions `claude-a1b2c3d4` and `claude-e5f6g7h8` are still running
- **THEN** both sessions SHALL appear in the session list immediately and SHALL be attachable via WebSocket

### Requirement: CLI sessions are automatically tmux-managed
The container SHALL provide a `claude` shell function (in `.bashrc`) that wraps every `claude` invocation in a tmux session. The function SHALL create a tmux session with a random `claude-<hex>` name, start claude with `--dangerously-skip-permissions` and any passed arguments, and attach the user's terminal interactively. The function SHALL unset the `TMUX` environment variable before attaching to avoid "sessions should be nested with care" warnings when the user is already inside a tmux session on the host. The function SHALL generate random hex using `od -An -tx1 -N4 /dev/urandom | tr -d ' \n'` (coreutils, no extra dependencies). The user SHALL be able to detach with the standard tmux detach key (`Ctrl+B d`).

#### Scenario: CLI user runs claude
- **WHEN** a user runs `claude` from the container shell
- **THEN** a new tmux session SHALL be created and the user SHALL be attached interactively. The session SHALL appear in the dashboard.

#### Scenario: CLI user runs claude with arguments
- **WHEN** a user runs `claude "fix the bug in main.go"`
- **THEN** the arguments SHALL be passed through to the claude command inside the tmux session

#### Scenario: CLI user detaches
- **WHEN** a user presses `Ctrl+B d` while in a tmux-attached claude session
- **THEN** the user SHALL return to their shell, the tmux session SHALL continue running, and the session SHALL remain visible and attachable in the dashboard

### Requirement: tmux configuration
The container SHALL include a `.tmux.conf` at `/home/claude/.tmux.conf` with: `default-terminal` set to `xterm-256color`, `history-limit` set to `50000`, `mouse` set to `off`, `status` set to `off`, and `window-size` set to `latest`. The status bar SHALL be disabled because the dashboard provides its own session UI. The `window-size latest` setting SHALL ensure that when multiple clients attach with different terminal sizes, tmux uses the most recently active client's dimensions rather than constraining to the smallest.

#### Scenario: Terminal colors work correctly
- **WHEN** Claude Code runs inside a tmux session
- **THEN** 256-color output SHALL render correctly because `default-terminal` is set to `xterm-256color`

#### Scenario: Scrollback is available
- **WHEN** a tmux session has been running with extensive output
- **THEN** up to 50000 lines of scrollback SHALL be retained by tmux
