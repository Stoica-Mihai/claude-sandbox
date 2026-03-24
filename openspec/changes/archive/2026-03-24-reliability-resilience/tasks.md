## 1. WebSocket auto-reconnect

- [x] 1.1 In `terminal.js` `TerminalManager.create()`, refactor the WebSocket setup into a `connectWebSocket(terminalId, term, containerEl)` function that can be called for both initial connection and reconnection
- [x] 1.2 In `ws.onclose`, check close code: if 1000 (normal), show "[Session ended]" as before. Otherwise, start reconnection sequence.
- [x] 1.3 Implement exponential backoff: 1s initial, double each attempt, cap at 30s, max 10 retries. Use `setTimeout` for delays.
- [x] 1.4 On each attempt, write `\r\n\x1b[90m[Reconnecting... (attempt N)]\x1b[0m` to the terminal
- [x] 1.5 On successful reconnect: reset backoff counter, write `\r\n\x1b[32m[Reconnected]\x1b[0m`, send resize message. Server replays scrollback via AddViewer automatically.
- [x] 1.6 After 10 failed attempts, write `\r\n\x1b[31m[Connection lost]\x1b[0m` and stop retrying
- [x] 1.7 Store the reconnect timer ID so it can be cancelled if `TerminalManager.destroy(terminalId)` is called (prevents reconnecting to a killed session)

## 2. Health check endpoint

- [x] 2.1 Add `handleHealthz` handler in `handlers.go`: return HTTP 200 with `{"status":"ok"}` and `Content-Type: application/json`
- [x] 2.2 Register `GET /healthz` route in `NewServer`
- [x] 2.3 Add `healthcheck` block to `docker-compose.yml`: `curl -f http://localhost:${DASHBOARD_PORT:-8080}/healthz`, interval 10s, timeout 5s, retries 3, start_period 15s

## 3. Verification

- [x] 3.1 `go build ./...` and `go vet ./...` pass
- [x] 3.2 Docker image builds and dashboard starts
- [x] 3.3 `curl http://localhost:8080/healthz` returns `{"status":"ok"}`
- [x] 3.4 `docker inspect --format='{{.State.Health.Status}}' claude_workspace` shows "healthy"
- [x] 3.5 Open a terminal, disconnect network briefly (or restart container), verify terminal shows "Reconnecting..." and recovers
- [x] 3.6 Kill a session, verify terminal shows "[Session ended]" with no reconnection attempt
