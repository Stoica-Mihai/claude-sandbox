# web-terminal Delta

## MODIFIED Requirements

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
