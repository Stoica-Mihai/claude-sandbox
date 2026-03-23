## 1. Dockerfile & Container Setup

- [x] 1.1 Add `tmux` to apt-get install in Dockerfile
- [x] 1.2 Add `.tmux.conf` with `default-terminal xterm-256color`, `history-limit 50000`, `mouse off`, `status off`, `window-size latest` — copy to `/home/claude/.tmux.conf`
- [x] 1.3 Replace the `claude` alias in `.bashrc` with a shell function that wraps claude in `tmux new-session` (use `od -An -tx1 -N4 /dev/urandom | tr -d ' \n'` for hex generation, unset `$TMUX` before attach to avoid nesting warnings)

## 2. Session Manager Rewrite

- [x] 2.1 Replace `ManagedSession` struct with `TmuxSession` struct (Name, CWD, CreatedAt, Attached)
- [x] 2.2 Replace `DisplaySession` — remove External/Managed fields, add Name and Attached fields
- [x] 2.3 Implement `Spawn()` using `tmux new-session -d -s <name> -c <cwd> -- claude --dangerously-skip-permissions` with retry on name collision
- [x] 2.4 Implement `Kill()` using `tmux kill-session -t <name>`
- [x] 2.5 Implement `ListSessions()` using `tmux list-sessions -F` with `claude-` prefix filtering
- [x] 2.6 Add session list caching (2-second TTL, invalidated on spawn/kill)
- [x] 2.7 Add periodic polling goroutine (every 5s) that compares `tmux list-sessions` with cached list and calls `broker.Publish()` on change
- [x] 2.8 Remove `discoverSessions()` file-based discovery, `DetectedSession` struct, and managed PID merge logic
- [x] 2.9 Remove `RingBuffer` usage from session management (delete `ringbuffer.go` if no longer used)

## 3. WebSocket Handler Changes

- [x] 3.1 Update `handleWebSocket` to verify session exists via `tmux has-session -t <name>` before upgrading
- [x] 3.2 Update `handleWebSocket` to spawn `tmux attach -t <name>` with `pty.StartWithSize` as the relay PTY
- [x] 3.3 Handle attach PTY lifecycle: read loop for PTY→WebSocket, write from WebSocket→PTY, cleanup on disconnect
- [x] 3.4 On WebSocket disconnect, kill the `tmux attach` process and close the PTY (tmux session continues)
- [x] 3.5 On tmux session exit (attach process EOF), send WebSocket close frame

## 4. HTTP Handler Updates

- [x] 4.1 Update `handleSpawn` to use new `Spawn()` and return tmux session name as `X-Terminal-Id`
- [x] 4.2 Update `handleKill` to use new `Kill()` with tmux session name
- [x] 4.3 Update `handleSessionsFragment` to pass new `DisplaySession` model to template

## 5. Frontend Changes

- [x] 5.1 Update `sessions.html` — remove external/managed branching, uniform status indicators, kill button on all sessions
- [x] 5.2 Update `sessions.html` — use tmux session name as `data-terminal-id`
- [x] 5.3 Update `views.js` — remove the `opacity-60` click guard that blocks external session clicks

## 6. Cleanup

- [x] 6.1 Remove `ringbuffer.go` if no longer referenced
- [x] 6.2 Remove `sessionFileData` struct and JSON parsing code
- [x] 6.3 Update `Shutdown()` to close active WebSocket connections and kill attach processes only (do NOT kill tmux sessions — they must survive dashboard restarts)

## 7. Verification

- [x] 7.1 Build the Go binary (`go build`) and verify no compile errors
- [x] 7.2 Build the Docker image (`make up`) and verify tmux is available in the container
- [x] 7.3 Test spawning a session from the dashboard and interacting via xterm.js
- [x] 7.4 Test spawning a session from CLI (`claude` command) and attaching from the dashboard
- [x] 7.5 Test killing a session from the dashboard (both dashboard-spawned and CLI-spawned)
- [x] 7.6 Test dashboard restart — verify existing tmux sessions appear and are attachable
