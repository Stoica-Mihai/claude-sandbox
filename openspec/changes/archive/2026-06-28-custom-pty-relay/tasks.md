## 1. tmux Configuration Changes

- [x] 1.1 Set `mouse off` in `tmux.conf`
- [x] 1.2 Add `set -ga terminal-overrides ',xterm-256color:smcup@:rmcup@'` to `tmux.conf`

## 2. Ring Buffer

- [x] 2.1 Create `dashboard/ringbuffer.go` — fixed-size circular byte buffer with `Write([]byte)`, `Bytes() []byte`, and `Reset()` methods, protected by sync.Mutex
- [x] 2.2 Add configurable capacity constant (default 1MB)

## 3. PTY Relay Module

- [x] 3.1 Create `dashboard/relay.go` with `Relay` struct: session name, ring buffer, unix socket listener, socat connection, connected WebSocket viewers list (sync.RWMutex-protected)
- [x] 3.2 Implement `Start()` — create unix socket at `/tmp/relay-<name>.sock`, listen for connection, run `tmux pipe-pane -t <session> 'socat - UNIX-CONNECT:/tmp/relay-<name>.sock'`, accept socat connection
- [x] 3.3 Implement output reader goroutine — read from socat connection, track alternate screen state, strip sequences from viewer output, route normal-mode output to ring buffer, broadcast all output to viewers
- [x] 3.4 Implement `Stop()` — run `tmux pipe-pane -t <session>` (no arg = stop), close socat connection, close unix socket, remove socket file, clean up goroutine
- [x] 3.5 Implement `AddViewer(conn)` — register WebSocket, send terminal reset `\x1bc`, replay ring buffer, add to broadcast list
- [x] 3.6 Implement `RemoveViewer(conn)` — unregister WebSocket from broadcast list
- [x] 3.7 Implement `SendInput(data []byte)` — write bytes directly to the socat connection (delivered to pane stdin)
- [x] 3.8 Implement `Resize(cols, rows)` — run `tmux resize-window -t <session> -x <cols> -y <rows>`
- [x] 3.9 Handle socat connection drop — detect EOF/error on socat connection, re-establish pipe-pane + socat, reconnect within 1 second

## 4. Alternate Screen Tracking

- [x] 4.1 Implement byte scanner that detects `\x1b[?1049h`, `\x1b[?1049l`, `\x1b[?47h`, `\x1b[?47l` in output chunks — strips them from viewer output, toggles `inAlternateScreen` boolean
- [x] 4.2 Handle edge case: sequence split across two read chunks (partial match at end of buffer)
- [x] 4.3 When `inAlternateScreen` is true, broadcast to viewers but skip ring buffer writes
- [x] 4.4 When `inAlternateScreen` is false, broadcast to viewers AND write to ring buffer

## 5. Relay Manager Integration

- [x] 5.1 Add relay map to `SessionManager` — `map[string]*Relay`, one per discovered session
- [x] 5.2 Start relay when session is spawned via `Spawn()`
- [x] 5.3 Start relay for sessions discovered on startup and by the poller
- [x] 5.4 Stop relay when session is killed via `Kill()` or exits (detected by socat EOF)
- [x] 5.5 On poller detecting new session, start relay; on session gone, stop relay

## 6. WebSocket Handler Rewrite

- [x] 6.1 Rewrite `handleWebSocket` — remove `tmux attach` + PTY + sync.Once code, replace with: verify session → get relay → upgrade WebSocket → `relay.AddViewer(conn)` → read loop for input/resize → on disconnect `relay.RemoveViewer(conn)`
- [x] 6.2 Input handling: WebSocket BinaryMessage → `relay.SendInput(data)`
- [x] 6.3 Resize handling: WebSocket TextMessage with resize JSON → `relay.Resize(cols, rows)`

## 7. Frontend Changes

- [x] 7.1 Ensure `copyOnSelect: true` and `rightClickSelectsWord: true` are set in terminal.js
- [x] 7.2 Remove mobile scroll mode toggle (`mobileToggleScrollMode`, `mobileScrollActive`) and Page Up/Down override from views.js (xterm.js native scroll works)
- [x] 7.3 Remove scroll mode button from mobile control bar in layout.html

## 8. Cleanup

- [x] 8.1 Remove `creack/pty` import from handlers.go
- [x] 8.2 Check if `creack/pty` is used anywhere else — if not, remove from go.mod
- [x] 8.3 Update CLAUDE.md to reflect relay architecture

## 9. Verification

- [x] 9.1 `go build` and `go vet` pass
- [x] 9.2 Docker image builds successfully
- [x] 9.3 Test: spawn session from dashboard, terminal is interactive
- [x] 9.4 Test: text selection works with mouse drag (no Shift needed) and auto-copies
- [x] 9.5 Test: mouse wheel scrolls through xterm.js scrollback on desktop
- [x] 9.6 Test: touch scroll works on mobile through xterm.js scrollback
- [x] 9.7 Test: close tab and reopen — clean reconnect with history replay, no garbled content, no duplicate banners or TUI artifacts in scrollback
- [x] 9.8 Test: CLI `tmux attach` still works for CLI users
- [x] 9.9 Test: session exits cleanly, viewer gets close notification
- [x] 9.10 Test: multiple viewers on same session both see output and can send input
- [x] 9.11 Test: rapid typing and paste — no latency or dropped characters
