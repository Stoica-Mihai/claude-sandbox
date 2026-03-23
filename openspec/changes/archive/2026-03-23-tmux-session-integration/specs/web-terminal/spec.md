## MODIFIED Requirements

### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint (served by the Go backend via `gorilla/websocket`) that connects a browser-based xterm.js terminal to a tmux session running Claude Code. When a WebSocket connection is made, the server SHALL spawn `tmux attach -t <sessionName>` with a PTY via `pty.StartWithSize` and relay bidirectional data between the xterm.js client and the attach PTY with no transformation, preserving all escape sequences, colors, and control characters. The attach PTY is ephemeral — it is created on WebSocket connect and destroyed on disconnect.

#### Scenario: Attach to a session
- **WHEN** a WebSocket connection is made to `/ws/terminal/:sessionName`
- **THEN** the server SHALL verify the tmux session exists via `tmux has-session -t <name>`, spawn `tmux attach -t <name>` with a PTY, and begin relaying data in both directions. tmux SHALL replay the current pane content to the client automatically.

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be encoded as binary (via `TextEncoder`) and sent as a WebSocket BinaryMessage, which the server SHALL write to the attach PTY stdin. Terminal input MUST NOT be sent as a WebSocket TextMessage, as the server reserves TextMessage for JSON control messages (e.g., resize).

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** tmux SHALL relay the output through the attach process and the server SHALL forward it to the WebSocket client immediately

#### Scenario: Invalid session name
- **WHEN** a WebSocket connection is attempted for a tmux session name that does not exist
- **THEN** the server SHALL return HTTP 404 before upgrading to WebSocket

#### Scenario: Multiple viewers on the same session
- **WHEN** two WebSocket connections attach to the same tmux session simultaneously
- **THEN** each SHALL get its own `tmux attach` process and PTY. Both viewers SHALL see the same terminal content. Input from either viewer SHALL be sent to the session.

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the attach PTY dimensions SHALL be updated so tmux propagates the resize to Claude Code's TUI.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON resize message with `cols` and `rows` values
- **THEN** the Go server SHALL call `pty.Setsize()` on the attach PTY file descriptor, and tmux SHALL propagate the new dimensions to the claude process

### Requirement: Session lifecycle over WebSocket
The system SHALL handle session end gracefully. When the Claude Code process exits inside tmux, the tmux session ends, the attach process exits, and the WebSocket client SHALL be notified. When the WebSocket disconnects, the attach process SHALL be terminated but the tmux session SHALL continue running.

#### Scenario: Claude Code process exits
- **WHEN** the claude process exits inside the tmux session (user typed `/exit`, process crashed, etc.)
- **THEN** the tmux session SHALL end, the attach process SHALL exit, the PTY read SHALL return EOF, and the server SHALL send a close frame to the WebSocket

#### Scenario: WebSocket disconnects
- **WHEN** the browser closes or the WebSocket connection drops
- **THEN** the server SHALL terminate the `tmux attach` process and close the PTY. The tmux session SHALL continue running and remain reattachable via a new WebSocket connection.

#### Scenario: Reattach to a session
- **WHEN** a new WebSocket connection is made to `/ws/terminal/:sessionName` for a still-running tmux session
- **THEN** the server SHALL spawn a new `tmux attach` process with a new PTY, and tmux SHALL replay the current pane content to the client
