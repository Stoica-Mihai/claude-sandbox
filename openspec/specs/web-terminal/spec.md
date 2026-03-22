# web-terminal Specification

## Purpose
TBD - created by archiving change web-dashboard. Update Purpose after archive.
## Requirements
### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint (served by the Go backend via `gorilla/websocket`) that connects a browser-based xterm.js terminal to a PTY running Claude Code. The WebSocket SHALL relay bidirectional data between the xterm.js client and the PTY with no transformation, preserving all escape sequences, colors, and control characters.

#### Scenario: Attach to a spawned session
- **WHEN** a WebSocket connection is made to `/ws/terminal/:terminalId`
- **THEN** the server SHALL attach the WebSocket to the corresponding PTY and begin relaying data in both directions

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be encoded as binary (via `TextEncoder`) and sent as a WebSocket BinaryMessage, which the server SHALL write to PTY stdin. Terminal input MUST NOT be sent as a WebSocket TextMessage, as the server reserves TextMessage for JSON control messages (e.g., resize).

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** the output SHALL be sent to the WebSocket client immediately

#### Scenario: Invalid terminal ID
- **WHEN** a WebSocket connection is attempted for a non-existent terminal ID
- **THEN** the server SHALL close the WebSocket with a 1008 (Policy Violation) code and an error reason

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the PTY dimensions SHALL be updated so Claude Code's TUI layout adapts correctly.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON resize message with `cols` and `rows` values
- **THEN** the Go server SHALL call `pty.Setsize()` on the corresponding PTY file descriptor

### Requirement: Session lifecycle over WebSocket
The system SHALL handle session end gracefully. When the Claude Code process exits, the WebSocket client SHALL be notified. When the WebSocket disconnects, the PTY process SHALL continue running (detached mode).

#### Scenario: Claude Code process exits
- **WHEN** the PTY process exits (user typed `/exit`, process crashed, etc.)
- **THEN** the server SHALL send a close frame to the WebSocket with exit code information

#### Scenario: WebSocket disconnects
- **WHEN** the browser closes or the WebSocket connection drops
- **THEN** the PTY process SHALL continue running and remain reattachable via a new WebSocket connection to the same terminal ID

#### Scenario: Reattach to a detached session
- **WHEN** a new WebSocket connection is made to `/ws/terminal/:terminalId` for a still-running detached PTY
- **THEN** the server SHALL attach the new WebSocket to the existing PTY and replay recent terminal output from the scrollback ring buffer

### Requirement: Full Claude Code TUI support
The PTY SHALL be configured to fully support Claude Code's interactive features including slash commands with autocomplete, keyboard shortcuts, colored output, and the thinking/streaming display. The PTY SHALL be spawned with `pty.StartWithSize` using a large initial size (120x50) so Claude Code renders its banner and UI correctly before xterm.js connects and sends the actual terminal dimensions. Using a small default (e.g., 80x24) causes Claude Code to render for the small size, and the cursor position becomes incorrect after resize.

#### Scenario: Slash command autocomplete
- **WHEN** the user types `/` followed by a partial command in the web terminal
- **THEN** Claude Code's autocomplete SHALL appear and function identically to a native terminal

#### Scenario: Keyboard shortcuts
- **WHEN** the user presses Claude Code keyboard shortcuts (e.g., Escape to cancel, Ctrl+C to interrupt)
- **THEN** the shortcuts SHALL be forwarded to the PTY and behave identically to a native terminal

