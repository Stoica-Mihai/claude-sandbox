## MODIFIED Requirements

### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint (served by the Go backend via `gorilla/websocket`) that connects a browser-based xterm.js terminal to a tmux session via the relay's pipe-pane + socat unix socket. When a WebSocket connection is made, the server SHALL register the viewer with the session's relay, replay the ring buffer for scrollback history, and begin delivering live output via broadcast. Terminal input SHALL be sent to the tmux pane via the relay's socat connection. No `tmux attach` process is spawned per viewer.

#### Scenario: Attach to a session
- **WHEN** a WebSocket connection is made to `/ws/terminal/:sessionName`
- **THEN** the server SHALL verify the session's relay exists and is not stopped, upgrade to WebSocket, register the viewer via `AddViewer` (which sends a terminal reset and ring buffer replay), and enter the read loop for input and control messages

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be encoded as binary (via `TextEncoder`) and sent as a WebSocket BinaryMessage. The server SHALL call `UnsuspendViewer`, then `ResizeToViewer`, then `SendInput` to forward the data to the tmux pane via the relay's socat connection.

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** the relay SHALL read the output from the socat connection, process alternate screen tracking, broadcast cleaned output to all non-suspended viewers, and write normal-mode segments to the ring buffer

#### Scenario: Invalid session name
- **WHEN** a WebSocket connection is attempted for a session whose relay does not exist or is stopped
- **THEN** the server SHALL return HTTP 404 before upgrading to WebSocket

#### Scenario: Multiple viewers on the same session
- **WHEN** two WebSocket connections attach to the same tmux session simultaneously
- **THEN** both SHALL be registered as viewers on the same relay. Both SHALL receive broadcast output (unless suspended). Input from either viewer SHALL be sent to the pane. The active viewer's dimensions SHALL determine the tmux window size.

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the client SHALL send a JSON resize control message. The server SHALL store the dimensions and resize tmux only if the viewer is the active viewer.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON TextMessage with `{"type":"resize","cols":N,"rows":N}`
- **THEN** the server SHALL call `Resize(conn, cols, rows)` which stores the dimensions and resizes tmux only if this viewer is the active viewer

## ADDED Requirements

### Requirement: Client-side deactivation recovery
When the client receives a `{"type":"deactivated"}` text message, it SHALL set a `needsRefresh` flag. On the next user input (`term.onData`), if the flag is set, the client SHALL call `term.clear()` to remove garbled content before sending the keystroke. The keystroke triggers `ResizeToViewer` on the server, which resizes tmux and produces a clean redraw via broadcast.

#### Scenario: Client receives deactivation
- **WHEN** the WebSocket receives a text message with `{"type":"deactivated"}`
- **THEN** the client SHALL set `needsRefresh = true`

#### Scenario: User types after deactivation
- **WHEN** the user types in a terminal where `needsRefresh` is true
- **THEN** the client SHALL set `needsRefresh = false`, call `term.clear()`, and send the keystroke as a BinaryMessage

#### Scenario: No deactivation active
- **WHEN** the user types in a terminal where `needsRefresh` is false
- **THEN** the client SHALL send the keystroke normally without clearing the terminal
