# web-terminal Specification

## Purpose
WebSocket-based terminal endpoint connecting a browser xterm.js terminal to a Claude Code dtach session. The browser connects to the frontend, which proxies the WebSocket to the backend; the backend registers the viewer with the session's relay. This spec covers the WebSocket endpoint and the viewer contract — relay internals are specified in `pty-relay` and session lifecycle in `dtach-sessions`.

## Requirements
### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint (served by the Go backend via `gorilla/websocket`) that connects a browser-based xterm.js terminal to a dtach session through the per-session relay. The browser connects to the frontend, which reverse-proxies the WebSocket to the backend. When a WebSocket connection is established, the backend SHALL register the viewer with the session's relay (see `pty-relay`), replay scrollback (terminal reset `\x1bc` + ring buffer) on attach, and begin delivering live output via broadcast. Terminal input SHALL be written to the relay's `dtach -a` attach PTY. No per-viewer attach process is spawned; one relay owns a single attach for all viewers.

#### Scenario: Attach to a session
- **WHEN** a WebSocket connection is made to `/ws/terminal/{terminalId}`
- **THEN** the backend SHALL verify the session's relay exists and is not stopped, upgrade to WebSocket, register the viewer via `AddViewer` (which sends a terminal reset and ring buffer replay), and enter the read loop for input and control messages

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be encoded as binary (via `TextEncoder`) and sent as a WebSocket BinaryMessage. The backend SHALL call `UnsuspendViewer`, then `ResizeToViewer`, then `SendInput` to write the data to the relay's attach PTY.

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** the relay SHALL read the output from the attach PTY, process alternate-screen tracking, broadcast cleaned output to all non-suspended viewers, and write normal-mode segments to the ring buffer

#### Scenario: Invalid session name
- **WHEN** a WebSocket connection is attempted for a session whose relay does not exist or is stopped
- **THEN** the backend SHALL return HTTP 404 before upgrading to WebSocket

#### Scenario: Multiple viewers on the same session
- **WHEN** two WebSocket connections attach to the same session simultaneously
- **THEN** both SHALL be registered as viewers on the same relay. Both SHALL receive broadcast output (unless suspended). Input from either viewer SHALL be written to the attach PTY. The active viewer's dimensions SHALL determine the PTY size.

### Requirement: WebSocket origin checking
The frontend SHALL guard WebSocket upgrades against Cross-Site WebSocket Hijacking via `checkWSOrigin`. An upgrade SHALL be permitted only when the request is same-origin (Origin host equals Host) or the Origin appears in the `ALLOWED_WS_ORIGINS` allowlist; otherwise the upgrade SHALL be rejected.

#### Scenario: Same-origin upgrade allowed
- **WHEN** a WebSocket upgrade arrives with an Origin host matching the request Host (or matching an entry in `ALLOWED_WS_ORIGINS`)
- **THEN** the frontend SHALL allow the upgrade and proxy it to the backend

#### Scenario: Cross-origin upgrade rejected
- **WHEN** a WebSocket upgrade arrives with an Origin that is neither same-origin nor in `ALLOWED_WS_ORIGINS`
- **THEN** the frontend SHALL reject the upgrade with HTTP 403

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the client SHALL send a JSON resize control message. The backend SHALL store the dimensions and resize the attach PTY only if the viewer is the active viewer.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON TextMessage with `{"type":"resize","cols":N,"rows":N}`
- **THEN** the backend SHALL call `Resize(conn, cols, rows)` which stores the dimensions and resizes the attach PTY via `pty.Setsize` only if this viewer is the active viewer

### Requirement: Session lifecycle over WebSocket
The system SHALL handle session end gracefully. When the Claude Code process exits, the dtach session ends, the relay's attach PTY reaches EOF, and the WebSocket client SHALL be notified. When the WebSocket disconnects, the viewer SHALL be removed but the dtach session SHALL continue running. Session lifecycle details are specified in `dtach-sessions`.

#### Scenario: Claude Code process exits
- **WHEN** the claude process exits inside the dtach session (user typed `/exit`, process crashed, etc.)
- **THEN** the dtach session SHALL end, the relay's attach PTY read SHALL return EOF, and the backend SHALL send a close frame to the WebSocket viewers

#### Scenario: WebSocket disconnects
- **WHEN** the browser closes or the WebSocket connection drops
- **THEN** the backend SHALL remove the viewer from the relay. The dtach session SHALL continue running and remain reattachable via a new WebSocket connection.

#### Scenario: Reattach to a session
- **WHEN** a new WebSocket connection is made to `/ws/terminal/{terminalId}` for a still-running dtach session
- **THEN** the backend SHALL register the viewer with the existing relay, which replays the ring buffer (terminal reset + scrollback) so the client sees current history

### Requirement: Full Claude Code TUI support
The relay's attach PTY SHALL be configured to fully support Claude Code's interactive features including slash commands with autocomplete, keyboard shortcuts, colored output, and the thinking/streaming display. dtach is used as a thin detach layer with no terminal emulation, keeping the raw byte stream intact so xterm.js renders the TUI correctly. PTY sizing follows the viewer-driven resize contract: a size is imposed only when a viewer is present (see `pty-relay`).

#### Scenario: Slash command autocomplete
- **WHEN** the user types `/` followed by a partial command in the web terminal
- **THEN** Claude Code's autocomplete SHALL appear and function identically to a native terminal

#### Scenario: Keyboard shortcuts
- **WHEN** the user presses Claude Code keyboard shortcuts (e.g., Escape to cancel, Ctrl+C to interrupt)
- **THEN** the shortcuts SHALL be forwarded to the attach PTY and behave identically to a native terminal

### Requirement: Client-side deactivation recovery
When the client receives a `{"type":"deactivated"}` text message, it SHALL set a `needsRefresh` flag. On the next user input (`term.onData`), if the flag is set, the client SHALL call `term.clear()` to remove garbled content before sending the keystroke. The keystroke triggers `ResizeToViewer` on the backend, which resizes the attach PTY and produces a clean redraw via broadcast.

#### Scenario: Client receives deactivation
- **WHEN** the WebSocket receives a text message with `{"type":"deactivated"}`
- **THEN** the client SHALL set `needsRefresh = true`

#### Scenario: User types after deactivation
- **WHEN** the user types in a terminal where `needsRefresh` is true
- **THEN** the client SHALL set `needsRefresh = false`, call `term.clear()`, and send the keystroke as a BinaryMessage

#### Scenario: No deactivation active
- **WHEN** the user types in a terminal where `needsRefresh` is false
- **THEN** the client SHALL send the keystroke normally without clearing the terminal

### Requirement: WebSocket auto-reconnect with exponential backoff
When the WebSocket connection to a terminal drops unexpectedly, the client SHALL automatically attempt to reconnect. The reconnection logic SHALL distinguish between a normal session end (close code 1000) and an unexpected connection loss (any other code).

#### Scenario: Unexpected close triggers reconnect
- **WHEN** the WebSocket `onclose` event fires with a code other than 1000 (normal closure)
- **THEN** the client SHALL initiate reconnection with exponential backoff starting at 1 second

#### Scenario: Exponential backoff timing
- **WHEN** reconnection attempts are made
- **THEN** delays SHALL follow: 1s, 2s, 4s, 8s, 16s, capped at 30s. Each attempt doubles the previous delay until the cap.

#### Scenario: Successful reconnection
- **WHEN** a reconnection WebSocket handshake succeeds
- **THEN** the client SHALL reset the backoff counter, send a resize message, and the backend SHALL replay scrollback via `AddViewer`. Terminal resumes normal I/O.

#### Scenario: Maximum retry limit reached
- **WHEN** 10 consecutive attempts fail
- **THEN** the client SHALL stop retrying and display a permanent "Connection lost" message

#### Scenario: Normal session end does not trigger reconnect
- **WHEN** the WebSocket closes with code 1000
- **THEN** the client SHALL NOT reconnect and SHALL display "[Session ended]"
