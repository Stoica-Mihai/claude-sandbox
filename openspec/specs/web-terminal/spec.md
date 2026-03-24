# web-terminal Specification

## Purpose
WebSocket-based terminal relay connecting browser xterm.js to Claude Code sessions via tmux attach.

## Requirements
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

### Requirement: Full Claude Code TUI support
The attach PTY SHALL be configured to fully support Claude Code's interactive features including slash commands with autocomplete, keyboard shortcuts, colored output, and the thinking/streaming display. The attach PTY SHALL be spawned with `pty.StartWithSize` using a large initial size (120x50) so Claude Code renders its banner and UI correctly before xterm.js connects and sends the actual terminal dimensions. Using a small default (e.g., 80x24) causes Claude Code to render for the small size, and the cursor position becomes incorrect after resize.

#### Scenario: Slash command autocomplete
- **WHEN** the user types `/` followed by a partial command in the web terminal
- **THEN** Claude Code's autocomplete SHALL appear and function identically to a native terminal

#### Scenario: Keyboard shortcuts
- **WHEN** the user presses Claude Code keyboard shortcuts (e.g., Escape to cancel, Ctrl+C to interrupt)
- **THEN** the shortcuts SHALL be forwarded to the PTY and behave identically to a native terminal

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
- **THEN** the client SHALL reset the backoff counter, send a resize message, and the server SHALL replay scrollback via `AddViewer`. Terminal resumes normal I/O.

#### Scenario: Maximum retry limit reached
- **WHEN** 10 consecutive attempts fail
- **THEN** the client SHALL stop retrying and display a permanent "Connection lost" message

#### Scenario: Normal session end does not trigger reconnect
- **WHEN** the WebSocket closes with code 1000
- **THEN** the client SHALL NOT reconnect and SHALL display "[Session ended]"
