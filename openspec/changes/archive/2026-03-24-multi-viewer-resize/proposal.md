## Why

The custom PTY relay broadcasts all tmux pane output to every connected WebSocket viewer. When viewers have different terminal dimensions (e.g., mobile at 40x20 and desktop at 120x40), only the viewer whose size matches the current tmux window sees correctly rendered output — the other viewer receives escape sequences with cursor positioning for the wrong width, producing garbled display. There is also no mechanism to resize the tmux window when the user switches devices, so the terminal stays stuck at whichever size was set last.

## What Changes

- Per-viewer size tracking: each WebSocket viewer reports its terminal dimensions on connect and resize, stored server-side
- Active-viewer resize on input: when a viewer sends terminal input and is not the current active viewer, the relay resizes the tmux window to that viewer's dimensions before forwarding the keystroke (mimics tmux `window-size latest`)
- Viewer suspension: non-active viewers are suspended so broadcast skips them, freezing their display at the last correctly-rendered state instead of sending garbled output
- Deactivation notification: suspended viewers receive a `{"type":"deactivated"}` WebSocket text message so the client knows to clear the terminal on next user input
- Client-side clear on resume: when the user types on a deactivated terminal, the client clears the xterm.js buffer before sending the keystroke, producing a clean slate for the tmux redraw at the correct dimensions
- Per-connection write mutex: all WebSocket writes to a given connection are serialized to prevent gorilla/websocket concurrent-write corruption between broadcast and control messages

## Capabilities

### New Capabilities
- `multi-viewer-resize`: Server-side per-viewer dimension tracking, active-viewer switching with tmux resize, viewer suspension/unsuspension, deactivation signaling, and client-side display recovery

### Modified Capabilities
- `web-terminal`: WebSocket handler gains UnsuspendViewer → ResizeToViewer → SendInput sequence on binary input; text messages now parsed as control messages (resize or deactivated); client-side needsRefresh flag and term.clear() on input

## Impact

- **`dashboard/relay.go`**: `viewer` struct gains `writeMu`, `size`, and `suspended` fields. New methods: `Resize`, `ResizeToViewer`, `UnsuspendViewer`. `broadcast` skips suspended viewers and uses per-connection write mutex. `Relay` struct gains `viewers map[*websocket.Conn]*viewer` and `lastResizer` field.
- **`dashboard/handlers.go`**: `handleWebSocket` rewritten — BinaryMessage path calls `UnsuspendViewer` → `ResizeToViewer` → `SendInput`. `resizeMessage` renamed to `controlMessage`.
- **`dashboard/web/static/js/terminal.js`**: `ws.onmessage` parses text messages as JSON control messages. `needsRefresh` flag set on `"deactivated"`, `term.clear()` called on next `term.onData` when flag is set.
- **No new dependencies.** Uses `sync/atomic` from the standard library for the suspended flag.
