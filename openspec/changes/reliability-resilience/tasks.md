## 1. WebSocket Reconnection Logic

- [ ] 1.1 Add reconnection state tracking to `TerminalManager.create()` in `terminal.js`: store retry count, backoff delay, reconnect timer ID, and a `shouldReconnect` flag per terminal instance
- [ ] 1.2 Replace the current `ws.onclose` handler with reconnection-aware logic: if close code is 1000 with reason "process exited", show "[Session ended]" (existing behavior); otherwise, start the reconnection sequence
- [ ] 1.3 Implement `reconnect(terminalId)` function with exponential backoff: create a new WebSocket, wait 1s/2s/4s/8s/16s/30s (capped) between attempts, increment retry count, stop after 10 consecutive failures
- [ ] 1.4 On successful reconnect: reset backoff state, re-wire `ws.onopen`/`onmessage`/`onclose`/`onerror` handlers, send resize message with current terminal dimensions, update the instance's `ws` reference
- [ ] 1.5 On `TerminalManager.destroy(terminalId)`: set `shouldReconnect = false` and clear any pending reconnection timer to prevent reconnect attempts for killed sessions

## 2. Reconnection UI Indicator

- [ ] 2.1 Write "[Reconnecting... (attempt N)]" to the terminal buffer in dim gray (`\x1b[90m`) on each reconnection attempt
- [ ] 2.2 Write "[Reconnected]" in green (`\x1b[32m`) on successful reconnection
- [ ] 2.3 Write "[Connection lost. Click to retry.]" in red (`\x1b[31m`) after all retries are exhausted
- [ ] 2.4 Add a click handler on the terminal container that triggers a fresh reconnection sequence when retries are exhausted (reset retry count and backoff, start from 1s)

## 3. Health Check Endpoint

- [ ] 3.1 Add `handleHealthz` handler in `handlers.go` that returns HTTP 200 with `{"status": "ok"}` and `Content-Type: application/json`
- [ ] 3.2 Register `GET /healthz` route in `NewServer()` route registration block
- [ ] 3.3 Add `healthcheck` block to `docker-compose.yml`: `test: ["CMD", "curl", "-f", "http://localhost:8080/healthz"]`, interval 10s, timeout 5s, retries 3, start_period 15s

## 4. Stale Session Cleanup

- [ ] 4.1 Modify `discoverSessions()` in `session.go` to delete session files where `isProcessAlive(pid)` returns false, using `os.Remove()` on the file path
- [ ] 4.2 Log stale file deletions at debug level (`slog.Debug`) with the file name and PID
- [ ] 4.3 On deletion failure, log a warning (`slog.Warn`) and include the session in the returned list with `Alive: false` as a fallback
- [ ] 4.4 Exclude successfully cleaned-up sessions from the returned `[]DetectedSession` slice (do not append them)

## 5. Testing and Verification

- [ ] 5.1 Verify the Go code builds cleanly with the new `/healthz` handler and modified `discoverSessions()`
- [ ] 5.2 Test WebSocket reconnection by stopping and restarting the dashboard server while a terminal is open — verify the terminal shows reconnection messages and resumes after the server comes back
- [ ] 5.3 Test that killing a session from the sidebar does NOT trigger reconnection attempts (the "[Session ended]" message appears instead)
- [ ] 5.4 Test the health check endpoint: `curl http://localhost:8080/healthz` returns `{"status": "ok"}`
- [ ] 5.5 Test docker-compose health check: `docker inspect claude_workspace` shows healthy status after startup
- [ ] 5.6 Create a stale session file manually (`echo '{"pid":99999}' > ~/.claude/sessions/99999.json`), trigger a session list refresh, and verify the file is deleted
- [ ] 5.7 Verify that active session files for running processes are NOT deleted during cleanup
