## Why

The current tmux-based WebSocket relay has an inherent conflict: tmux's `mouse on` setting captures all mouse events for scrollback navigation, preventing xterm.js from handling native text selection and copy. Disabling tmux mouse mode restores selection but removes mouse wheel scrolling. The `smcup@:rmcup@` workaround for scrollback causes garbled reattach and banner duplication.

The root cause is that tmux's terminal emulation layer sits between Claude Code and xterm.js, injecting alternate screen management and mouse reporting that conflict with browser-native interactions. By building a custom relay that bypasses `tmux attach` for the viewer path, we can let xterm.js handle mouse events natively while tmux continues to own session persistence.

## What Changes

- The WebSocket handler stops using `tmux attach` as the relay mechanism
- A new server-side relay reads/writes directly to the tmux pane via `tmux pipe-pane` + socat over a unix socket (bidirectional, zero process spawning per keystroke)
- A server-side ring buffer captures all terminal output per session, replaying it into xterm.js on connect
- tmux `mouse off` is set — no more mouse reporting conflicts
- The relay tracks alternate screen state: strips sequences from viewer output (xterm.js stays in normal mode), routes TUI output to viewers only (not ring buffer), routes conversation output to both viewers and ring buffer
- On reattach, xterm.js is cleared before ring buffer replay — history is clean (conversation content only, no TUI artifacts)
- Native mouse wheel scrolling works via xterm.js's own scrollback
- Native text selection + `copyOnSelect` works without any toggle
- **BREAKING**: The `tmux attach`-based viewer path is removed

## Capabilities

### New Capabilities
- `pty-relay`: Server-side relay that connects WebSocket viewers to tmux sessions without using `tmux attach`. Includes ring buffer for scrollback capture, replay on reconnect, and direct I/O to the tmux pane.

### Modified Capabilities
- `web-terminal`: WebSocket handler changes from spawning `tmux attach` per connection to using the new PTY relay. Scrollback replay comes from the ring buffer instead of tmux's pane replay. Mouse reporting is not used.
- `tmux-sessions`: tmux configuration changes — `mouse off`, `smcup@:rmcup@` re-enabled. tmux still owns session persistence and CLI access. The dashboard viewer path bypasses `tmux attach`.

## Impact

- **Go backend** (`handlers.go`, new `relay.go`): WebSocket handler rewritten to use the relay instead of `tmux attach`. New relay module manages per-session output capture and ring buffer.
- **Go backend** (`session.go`): SessionManager gains relay lifecycle management — start relay when session is spawned/discovered, stop when session exits.
- **tmux.conf**: `mouse off`, add `smcup@:rmcup@` back.
- **Frontend** (`terminal.js`): xterm.js gets `copyOnSelect: true`. Clear terminal buffer on reconnect before replay. No mouse reporting interference — selection and scroll work natively.
- **Dependencies**: No new external dependencies. Ring buffer is a Go struct (similar to the previously deleted `ringbuffer.go`).
