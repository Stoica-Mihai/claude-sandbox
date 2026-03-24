## Context

The custom PTY relay (`relay.go`) replaced per-viewer `tmux attach` to give xterm.js native mouse control (selection, scroll) and provide a ring buffer for clean reconnection. The relay uses `tmux pipe-pane -IO` + socat over a unix socket for bidirectional I/O, broadcasting output to all connected WebSocket viewers. This works well for a single viewer but produces garbled display when multiple viewers have different terminal dimensions, because tmux renders at one window size and all viewers receive the same escape sequences.

## Goals / Non-Goals

**Goals:**
- Resize tmux to the active viewer's dimensions when they type (mimics `window-size latest`)
- Prevent non-active viewers from receiving garbled output at the wrong terminal width
- Recover cleanly when the user switches devices (clear + resize + redraw)
- Serialize WebSocket writes per connection to prevent gorilla/websocket frame corruption

**Non-Goals:**
- Keeping non-active viewers in sync with live output (fundamental limitation: one tmux pane = one render size)
- Per-viewer terminal rendering (would require per-viewer `tmux attach`, which conflicts with the relay's purpose)
- Automatic display recovery without user input (no reliable cross-platform trigger)

## Decisions

### 1. Active-viewer tracking via `lastResizer`

Track which viewer last triggered a resize. Only `ResizeToViewer` (input-triggered) changes the active viewer. `Resize` (client resize events) stores dimensions but does not change who is active.

**Rationale:** Prevents cascading resize loops. If `Resize` changed the active viewer, a tmux redraw could trigger a client resize event on the non-active viewer, which would resize tmux back, causing an infinite loop.

**Alternative considered:** Resize tmux on every client resize message. Rejected because client resize events fire on viewport changes (keyboard open/close on mobile), which would cause rapid tmux resizes even without user intent.

### 2. Suspend non-active viewers instead of sending garbled output

When `ResizeToViewer` fires, all non-active viewers are marked `suspended`. `broadcast` skips suspended viewers. Their display freezes at the last correctly-rendered state.

**Rationale:** Three alternatives were tried and rejected:
- **Server-side refresh** (reset + ring buffer replay to non-active viewers): caused concurrent write corruption between `broadcast` and `refreshOtherViewers` goroutines, plus the ring buffer contains mixed-width content.
- **Client-side refresh on focus**: unreliable trigger on mobile browsers; ring buffer replay is still garbled at wrong width.
- **No suspension** (let garbled output through): produces visually broken display that persists until user types.

Suspension is the only approach that avoids garbled display without concurrent write issues.

### 3. `atomic.Bool` for the suspended flag

The `suspended` field uses `sync/atomic.Bool` rather than being protected by the viewer's `writeMu` or the relay's `mu`.

**Rationale:** `broadcast` checks `suspended` on every output chunk for every viewer. Using the write mutex would require locking even for suspended viewers (defeating the purpose of skipping them). An atomic bool allows a lock-free check in the hot path.

### 4. Client-side `term.clear()` on resume

When the server sends `{"type":"deactivated"}`, the client sets a `needsRefresh` flag. On the next `term.onData` (user input), the client calls `term.clear()` before sending the keystroke.

**Rationale:** The server cannot send correctly-rendered content to the viewer (ring buffer has mixed-width data). The client must clear its own terminal before the tmux redraw arrives. Clearing on input (not on receiving the deactivation) avoids an unnecessary visual flash when the user isn't interacting.

### 5. Per-connection write mutex

Each viewer has a `writeMu sync.Mutex` that serializes all WebSocket writes (broadcast output, deactivation messages, close frames).

**Rationale:** gorilla/websocket does not support concurrent writes. Without serialization, `broadcast` (readLoop goroutine) and `ResizeToViewer` (handleWebSocket goroutine) can write to the same connection simultaneously, corrupting WebSocket frames and killing the connection.

## Risks / Trade-offs

- **Non-active viewer sees stale content** → Acceptable trade-off. The user confirmed they don't intend to use both devices simultaneously. Display recovers on the first keystroke.
- **Brief blank screen on resume** → `term.clear()` wipes the display before the tmux redraw arrives. The gap is one network round-trip (~1-50ms on LAN). Acceptable.
- **`broadcast` holds `writeMu` per viewer** → Serializes output delivery across viewers. For 2-3 viewers on localhost, latency is negligible. Would need rethinking for many viewers or high-latency connections.
- **`Resize` ignores non-active viewers** → If the active viewer disconnects and the non-active viewer sends a window resize (not input), tmux stays at the old size until they type. Edge case; `RemoveViewer` clears `lastResizer`, so the next `Resize` call from any viewer will set them as active.
