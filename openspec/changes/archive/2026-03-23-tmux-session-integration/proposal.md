## Why

External Claude Code sessions (started from the CLI) appear in the dashboard sidebar but cannot be interacted with — they have no PTY the dashboard can attach to. Additionally, dashboard-spawned sessions do not survive dashboard restarts because the PTY is owned by the dashboard process. Switching to tmux as the universal session owner solves both problems: every session becomes attachable from any client (dashboard or CLI), and sessions persist independently of the dashboard lifecycle.

## What Changes

- All Claude Code sessions are spawned inside tmux sessions with predictable names (`claude-<hex>`)
- Session discovery switches from reading `~/.claude/sessions/*.json` to `tmux list-sessions`
- WebSocket terminal relay attaches to sessions via `tmux attach -t <name>` instead of holding a direct PTY
- The managed vs external session distinction is removed — all sessions are tmux sessions
- The `claude` shell alias is replaced with a function that wraps invocations in tmux
- **BREAKING**: Sessions started without tmux are no longer visible in the dashboard
- tmux is added as a container dependency in the Dockerfile
- A minimal `.tmux.conf` is added (256color, 50k scrollback, no status bar, `window-size latest` for multi-client resize)

## Capabilities

### New Capabilities
- `tmux-sessions`: Covers tmux-based session lifecycle — spawning claude inside tmux, discovering sessions via `tmux list-sessions`, killing sessions via `tmux kill-session`, and session persistence across dashboard restarts.

### Modified Capabilities
- `session-api`: Session discovery changes from `~/.claude/sessions/*.json` file scanning to `tmux list-sessions`. The managed/detected/external session model is replaced with a single unified tmux session model. Merge logic is removed.
- `web-terminal`: WebSocket attachment changes from connecting to a dashboard-owned PTY to spawning `tmux attach -t <name>` with an ephemeral PTY. Scrollback replay is handled by tmux instead of the dashboard's ring buffer. Multiple viewers are supported natively via concurrent tmux attach processes.
- `dashboard-ui`: The external session visual treatment (dimmed, warning dot, "external" label, non-clickable) is removed. All sessions get uniform styling and are clickable. Kill button appears on all sessions.

## Impact

- **Go backend** (`session.go`, `handlers.go`): `SessionManager` rewritten — `ManagedSession` replaced with `TmuxSession`, `discoverSessions()` calls tmux instead of reading files, `Spawn()` runs `tmux new-session`, `Kill()` runs `tmux kill-session`, WebSocket handler spawns `tmux attach` per connection.
- **Go data model**: `RingBuffer` becomes optional (tmux owns scrollback). `DetectedSession` and `DisplaySession.External`/`.Managed` fields removed.
- **Frontend** (`sessions.html`, `views.js`): Remove external session styling and click guard. Kill button on all sessions.
- **Dockerfile**: Add `tmux` to apt-get. Add `.tmux.conf`. Replace claude alias with tmux-wrapping function.
- **Dependencies**: tmux becomes a hard runtime requirement inside the container.
