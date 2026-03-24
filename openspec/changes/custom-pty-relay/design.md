## Context

The dashboard currently relays terminal I/O by spawning an ephemeral `tmux attach -t <name>` process per WebSocket connection. tmux's terminal emulation layer injects mouse reporting and alternate screen management that conflicts with xterm.js's native capabilities (text selection, scrollback, copy). Previous attempts to work around this (`smcup@:rmcup@`, `mouse on/off` toggles) each solve one problem while creating another.

tmux remains necessary for session persistence (the PTY survives dashboard restarts) and CLI access (`tmux attach` from shell). But the dashboard's viewer path doesn't need tmux's terminal emulation — it just needs raw byte relay between the tmux pane and the WebSocket.

## Goals / Non-Goals

**Goals:**
- Native text selection and copy in xterm.js (no Shift required, no toggles)
- Native mouse wheel scrolling through xterm.js scrollback (desktop and mobile touch scroll)
- Clean reconnect with scrollback replay (no garbled content)
- Maintain session persistence via tmux
- Maintain CLI access via `tmux attach`

**Non-Goals:**
- Replacing tmux for session persistence
- Building a full terminal emulator (VT100 parser)
- Infinite/persistent scrollback (ring buffer is fixed-size, in-memory)
- Search within scrollback (future enhancement)

## Decisions

### Decision 1: Bidirectional I/O via `pipe-pane` + socat unix socket

Instead of `tmux attach` (which injects its own terminal emulation), the relay uses tmux's `pipe-pane` with socat to bridge a unix socket:

```
tmux pipe-pane -t <session> 'socat - UNIX-CONNECT:/tmp/relay-<name>.sock'
```

The Go server listens on `/tmp/relay-<name>.sock`. When pipe-pane starts socat, it connects to the socket. The server reads from the socket (pane output) and writes to the socket (pane input) — bidirectional, zero process spawning per keystroke.

**Alternatives considered:**
- `tmux send-keys` for input — spawns a process per keystroke (~1-2ms latency each), poor for rapid typing/paste
- Named pipes (FIFOs) for I/O — requires two pipes, more filesystem overhead, unidirectional
- `tmux control mode (-CC)` — requires parsing tmux's structured protocol, high complexity
- Reading the tmux PTY fd directly — fragile, requires knowing the fd

**Rationale:** socat is already in the container. The unix socket gives true bidirectional streaming with no per-message overhead. pipe-pane captures raw program output before tmux processes it for display.

### Decision 2: Alternate screen tracking with dual buffers

`pipe-pane` captures raw output from Claude Code, which includes alternate screen sequences (`\x1b[?1049h` / `\x1b[?1049l`). Instead of stripping them (which causes TUI artifacts in scrollback), the relay **tracks the alternate screen state** and routes output to different destinations:

- **Normal screen mode** (alternate screen OFF): Output is written to the ring buffer AND broadcast to WebSocket viewers. This is the actual conversation content — questions, answers, code output.
- **Alternate screen mode** (alternate screen ON): Output is broadcast to WebSocket viewers only (for live TUI rendering) but **NOT written to the ring buffer**. This is TUI chrome — banners, spinners, prompts, thinking indicators.

The alternate screen sequences themselves (`\x1b[?1049h/l`) are stripped from the WebSocket output so xterm.js stays in normal mode. But the relay uses them internally as a toggle to decide where output goes.

**Important:** The `smcup@:rmcup@` tmux override does NOT affect pipe-pane output — it only affects `tmux attach` clients. Tracking must happen in the Go relay code.

**Result:**
- xterm.js viewport: TUI renders correctly (all output is forwarded live)
- xterm.js scrollback: clean — only conversation content, no TUI artifacts or duplicate banners
- Ring buffer: clean — only conversation content for replay on reconnect
- Mouse wheel scroll: works (xterm.js is always in normal mode)
- Text selection: works (no mouse reporting)

**Implementation:** A single boolean flag (`inAlternateScreen`) toggled by detecting `\x1b[?1049h` (set true) and `\x1b[?1049l` (set false). When true, skip ring buffer writes. ~20 lines of Go.

### Decision 3: Ring buffer per session, started on session discovery

Each tmux session gets a dedicated ring buffer (configurable size, default 1MB) that captures all output from the moment the session is discovered. The buffer runs continuously, not just when a WebSocket is connected.

**Rationale**: Reconnecting viewers get history from before they connected, not just from the current WebSocket session. The buffer captures output even when no viewer is attached.

### Decision 4: Clear + replay on WebSocket connect

When a WebSocket connects, the server:
1. Sends `\x1bc` (terminal reset) to clear xterm.js state
2. Replays the ring buffer contents
3. Starts live streaming from the unix socket

**Rationale**: The terminal reset prevents garbled content from mixing old state with replayed content. The ring buffer replay gives context. Live streaming continues from there.

### Decision 5: Disable tmux mouse, keep smcup@:rmcup@ for CLI users

tmux.conf changes to:
- `set -g mouse off` — no mouse reporting, xterm.js handles all mouse events natively
- `set -ga terminal-overrides ',xterm-256color:smcup@:rmcup@'` — for CLI `tmux attach` users, alternate screen is also disabled so they get consistent behavior

Dashboard viewers don't need the tmux override (relay strips sequences directly), but CLI users benefit from it.

### Decision 6: Resize via `tmux resize-window`

Terminal resize events are relayed by running `tmux resize-window -t <session> -x <cols> -y <rows>`. This spawns a process per resize event, but resize events are already debounced by the frontend (150ms) and the command is fast (~1ms).

## Risks / Trade-offs

- **`pipe-pane` lifecycle** → pipe-pane must be set up per session and re-established if socat exits (e.g., session restarts). The relay manager must monitor connection health and reconnect.
- **Ring buffer memory** → 1MB per session. With 10 sessions, that's 10MB. Acceptable.
- **socat dependency** → Already in the container's Dockerfile apt-get list. Not a new dependency.
- **`tmux resize-window` spawns a process** → Fast (~1ms), debounced by frontend. Acceptable.
- **CLI users see banner duplication** → The `smcup@:rmcup@` override affects `tmux attach` for CLI users. CLI terminals have their own scrollback and selection though, so this is acceptable.
- **Alternate screen detection edge cases** → If Claude Code uses non-standard alternate screen sequences, the relay might mistrack the state. Mitigated by only tracking the standard `\x1b[?1049h/l` and `\x1b[?47h/l` sequences which Claude Code uses.
