## Why

The dashboard has several reliability gaps that degrade the user experience during normal operation:

1. **WebSocket drops are silent and unrecoverable.** When the browser loses the WebSocket connection to a terminal (network hiccup, container restart, laptop sleep/wake), the terminal shows "[Session ended]" and the user must manually reload the page and re-open the session. There is no visual indicator that a reconnection is being attempted and no automatic retry.

2. **No container health check.** Docker and docker-compose have no way to determine whether the dashboard server inside the container is actually healthy and serving requests. If the Go process hangs or stops accepting connections, `docker ps` still shows the container as "Up" with no indication of a problem. This prevents automated restart via `restart: unless-stopped` from working correctly.

3. **Stale session files accumulate.** Claude Code writes session files to `~/.claude/sessions/<pid>.json` when a session starts but does not clean them up when the process exits. The dashboard's `discoverSessions()` reads all of these files on every request, returning dead sessions with `alive: false`. Over time, stale files accumulate, cluttering the session list with ghost entries and adding unnecessary filesystem reads.

## What Changes

- Add WebSocket reconnection logic in `terminal.js` with exponential backoff (1s, 2s, 4s, 8s, capped at 30s) and a visible "Reconnecting..." indicator in the terminal. On successful reconnect, scrollback is replayed from the server's ring buffer.
- Add a `GET /healthz` health check endpoint to the Go server and configure a `healthcheck` block in `docker-compose.yml` so Docker can detect when the dashboard is unresponsive.
- Add stale session cleanup in `session.go` that removes `~/.claude/sessions/<pid>.json` files when the referenced PID is no longer alive, triggered during `ListSessions()`.

## Capabilities

### Modified Capabilities
- `dashboard-ui`: WebSocket reconnection indicator (visual "Reconnecting..." overlay with retry count) displayed in the terminal pane when the connection drops
- `session-api`: Stale session file cleanup during session discovery; new `/healthz` health check endpoint
- `web-terminal`: WebSocket auto-reconnect with exponential backoff, max retry limit, and scrollback replay on successful reconnection

## Impact

- **`dashboard/web/static/js/terminal.js`**: Add reconnection logic to the WebSocket lifecycle in `TerminalManager.create()`. Replace the current `ws.onclose` handler (which just prints "[Session ended]") with a retry loop using exponential backoff. Add a "Reconnecting..." message to the terminal on each attempt. On successful reconnect, the server replays scrollback automatically via the existing `handleWebSocket` replay logic.
- **`dashboard/handlers.go`**: Add a `handleHealthz` handler registered at `GET /healthz` that returns HTTP 200 with a JSON body indicating server status. No authentication required.
- **`dashboard/session.go`**: Modify `discoverSessions()` to delete session files whose PID is no longer alive, removing stale entries from disk rather than returning them as dead sessions.
- **`docker-compose.yml`**: Add a `healthcheck` block using `curl` or `wget` to probe `GET /healthz` with an appropriate interval, timeout, and retry count.
- **No new dependencies.** All changes use existing libraries (gorilla/websocket, xterm.js, standard Go net/http).
