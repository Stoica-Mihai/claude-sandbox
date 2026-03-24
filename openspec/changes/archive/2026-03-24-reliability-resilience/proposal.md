## Why

The dashboard has two reliability gaps:

1. **WebSocket drops are silent and unrecoverable.** When the browser loses the WebSocket connection to a terminal (network hiccup, container restart, laptop sleep/wake), the terminal shows "[Session ended]" and the user must manually reload the page and re-open the session. There is no reconnection attempt and no visual indicator.

2. **No container health check.** Docker has no way to determine whether the dashboard server is healthy. If the Go process hangs, `docker ps` still shows "Up." The `restart: unless-stopped` policy only triggers on process exit, not on hangs.

## What Changes

- Add WebSocket reconnection logic in `terminal.js` with exponential backoff (1s, 2s, 4s, 8s, capped at 30s) and a visible "Reconnecting..." indicator in the terminal. Only reconnect on abnormal close (code != 1000). On successful reconnect, scrollback is replayed from the server's ring buffer via `AddViewer`.
- Add a `GET /healthz` health check endpoint to the Go server and configure a `healthcheck` block in `docker-compose.yml` so Docker can detect when the dashboard is unresponsive.

## Capabilities

### New Capabilities

### Modified Capabilities
- `web-terminal`: WebSocket auto-reconnect with exponential backoff, max retry cap, and scrollback replay on successful reconnection
- `session-api`: New `/healthz` health check endpoint

## Impact

- **`dashboard/web/static/js/terminal.js`**: Replace `ws.onclose` handler with reconnection logic. On abnormal close, write "Reconnecting..." to terminal, attempt reconnection with exponential backoff. On normal close (code 1000), show "[Session ended]" as before. On successful reconnect, the server replays scrollback via `AddViewer`.
- **`dashboard/handlers.go`**: Add `handleHealthz` handler at `GET /healthz` returning HTTP 200 with `{"status":"ok"}`.
- **`docker-compose.yml`**: Add `healthcheck` block using `curl -f http://localhost:${DASHBOARD_PORT:-8080}/healthz`.
- **No new dependencies.**
