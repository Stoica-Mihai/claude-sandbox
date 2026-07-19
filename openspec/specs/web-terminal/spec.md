# web-terminal Specification

## Purpose
WebSocket-based terminal endpoint connecting a browser xterm.js terminal to a Claude Code session hosted by sessiond. The browser connects to the frontend, which proxies the WebSocket to the backend; the backend bridges the viewer to the session's sessiond socket. This spec covers the WebSocket endpoint and the viewer contract — the bridge is specified in `pty-relay`, session hosting in `session-host`, and backend-side lifecycle in `dtach-sessions`.

## Requirements
### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint (served by the Go backend via `gorilla/websocket`) that connects a browser-based xterm.js terminal to a claude session hosted by sessiond. The browser connects to the frontend, which reverse-proxies the WebSocket to the backend; the backend bridges the connection to the session's sessiond socket (see `pty-relay`). On the viewer's first resize message the bridge SHALL attach with those dimensions, and the viewer SHALL receive a rendered emulator snapshot (reset, scrollback, screen, cursor, terminal modes) followed by live output. Terminal input SHALL be forwarded as DATA frames to sessiond, which writes it to the session PTY. Each WebSocket connection is one sessiond viewer; no attach subprocess exists.

#### Scenario: Attach to a session
- **WHEN** a WebSocket connection is made to `/ws/terminal/{terminalId}`
- **THEN** the backend SHALL verify the session exists and is live, upgrade to WebSocket, bridge to the session's sessiond socket, and deliver the snapshot upon the client's first resize message

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be sent as a WebSocket BinaryMessage, forwarded by the bridge as a DATA frame, and written by sessiond to the session PTY

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** sessiond SHALL feed the bytes to the session emulator and broadcast them to all non-suspended viewers, and the bridge SHALL deliver them as WebSocket BinaryMessages

#### Scenario: Invalid session name
- **WHEN** a WebSocket connection is attempted for a session that does not exist or has ended
- **THEN** the backend SHALL return HTTP 404 before upgrading to WebSocket

#### Scenario: Multiple viewers on the same session
- **WHEN** two WebSocket connections attach to the same session simultaneously
- **THEN** both SHALL be registered as sessiond viewers. Both SHALL receive broadcast output (unless suspended). Input from either viewer SHALL reach the session PTY. The active viewer's dimensions SHALL determine the PTY size.

### Requirement: WebSocket origin checking
The frontend SHALL guard WebSocket upgrades against Cross-Site WebSocket Hijacking via `checkWSOrigin`. An upgrade SHALL be permitted only when the request is same-origin (Origin host equals Host) or the Origin appears in the `ALLOWED_WS_ORIGINS` allowlist; otherwise the upgrade SHALL be rejected.

#### Scenario: Same-origin upgrade allowed
- **WHEN** a WebSocket upgrade arrives with an Origin host matching the request Host (or matching an entry in `ALLOWED_WS_ORIGINS`)
- **THEN** the frontend SHALL allow the upgrade and proxy it to the backend

#### Scenario: Cross-origin upgrade rejected
- **WHEN** a WebSocket upgrade arrives with an Origin that is neither same-origin nor in `ALLOWED_WS_ORIGINS`
- **THEN** the frontend SHALL reject the upgrade with HTTP 403

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the client SHALL send a JSON resize control message. The bridge SHALL forward it as a CONTROL frame; sessiond SHALL store the dimensions and resize the session PTY only if the viewer is the active viewer.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON TextMessage with `{"type":"resize","cols":N,"rows":N}`
- **THEN** sessiond SHALL store the dimensions for that viewer and resize the session PTY only if that viewer is the active viewer

### Requirement: Session lifecycle over WebSocket
The system SHALL handle session end gracefully. When the Claude Code process exits, sessiond SHALL close every attach connection with a session-ended CLOSE, and the bridge SHALL close each WebSocket with code 1000. When a WebSocket disconnects, only that viewer detaches — the session SHALL continue running and remain reattachable. A backend restart SHALL NOT end sessions: reconnecting viewers SHALL reattach through the new backend and receive an exact snapshot with scrollback intact.

#### Scenario: Claude Code process exits
- **WHEN** the claude process exits inside a session (user typed `/exit`, process crashed, etc.)
- **THEN** sessiond SHALL end the session and the backend SHALL send a close frame (code 1000) to that session's WebSocket viewers

#### Scenario: WebSocket disconnects
- **WHEN** the browser closes or the WebSocket connection drops
- **THEN** the backend SHALL detach that viewer from sessiond. The session SHALL continue running and remain reattachable via a new WebSocket connection.

#### Scenario: Reattach to a session
- **WHEN** a new WebSocket connection is made to `/ws/terminal/{terminalId}` for a still-running session
- **THEN** the viewer SHALL receive the emulator snapshot rendered at its dimensions, including accumulated scrollback

#### Scenario: Backend restarts while sessions run
- **WHEN** the backend container is rebuilt or restarted while sessions are running
- **THEN** the sessions SHALL keep running in the sessions container, the clients SHALL reconnect with backoff, and each SHALL repaint from an exact snapshot with scrollback intact

### Requirement: Full Claude Code TUI support
The session PTY SHALL fully support Claude Code's interactive features including slash commands with autocomplete, keyboard shortcuts, colored output, and the thinking/streaming display. sessiond owns the PTY directly with no intermediate layer, keeping the raw byte stream intact so xterm.js renders the TUI correctly. PTY sizing follows the viewer-driven resize contract: a size is imposed only when a viewer is present (see `multi-viewer-resize`).

#### Scenario: Slash command autocomplete
- **WHEN** the user types `/` followed by a partial command in the web terminal
- **THEN** Claude Code's autocomplete SHALL appear and function identically to a native terminal

#### Scenario: Keyboard shortcuts
- **WHEN** the user presses Claude Code keyboard shortcuts (e.g., Escape to cancel, Ctrl+C to interrupt)
- **THEN** the shortcuts SHALL be forwarded to the session PTY and behave identically to a native terminal

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
