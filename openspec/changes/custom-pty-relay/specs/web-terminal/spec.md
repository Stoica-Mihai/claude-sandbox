## MODIFIED Requirements

### Requirement: WebSocket-based terminal connection
The system SHALL provide a WebSocket endpoint that connects a browser-based xterm.js terminal to a tmux session running Claude Code. When a WebSocket connection is made, the server SHALL register the connection with the session's PTY relay (NOT spawn `tmux attach`). Output is streamed from the relay's ring buffer (replay) and live pipe-pane reader. Input is relayed via `tmux send-keys`. The WebSocket connection does not own a PTY — the relay manages all I/O.

#### Scenario: Attach to a session
- **WHEN** a WebSocket connection is made to `/ws/terminal/:sessionName`
- **THEN** the server SHALL verify the tmux session exists, register the WebSocket with the relay, send a terminal reset (`\x1bc`), replay the ring buffer, and begin streaming live output. No `tmux attach` process is spawned.

#### Scenario: Terminal input
- **WHEN** the user types in the xterm.js terminal
- **THEN** the keystrokes SHALL be encoded as binary and sent as a WebSocket BinaryMessage, which the server SHALL write directly to the relay's unix socket (delivered to the pane via socat)

#### Scenario: Terminal output
- **WHEN** the Claude Code process writes to stdout/stderr
- **THEN** the output SHALL flow through tmux's pipe-pane via socat to the relay's unix socket. The relay tracks alternate screen state, strips the sequences from viewer output, routes normal-mode output to the ring buffer, and forwards all output to connected WebSocket viewers immediately.

#### Scenario: Invalid session name
- **WHEN** a WebSocket connection is attempted for a tmux session name that does not exist
- **THEN** the server SHALL return HTTP 404 before upgrading to WebSocket

#### Scenario: Multiple viewers on the same session
- **WHEN** two WebSocket connections attach to the same session simultaneously
- **THEN** both SHALL be registered with the same relay. Both receive identical output. Input from either is sent via `tmux send-keys`.

### Requirement: Terminal resize support
The system SHALL support dynamic terminal resizing. When the browser window or terminal pane is resized, the tmux window dimensions SHALL be updated via `tmux resize-window`.

#### Scenario: Browser resize
- **WHEN** the xterm.js client sends a JSON resize message with `cols` and `rows` values
- **THEN** the server SHALL run `tmux resize-window -t <session> -x <cols> -y <rows>` to update the pane dimensions

### Requirement: Session lifecycle over WebSocket
The system SHALL handle session end gracefully. When the tmux session exits, the relay detects it (pipe-pane EOF) and notifies all connected WebSocket viewers. When a WebSocket disconnects, it is simply unregistered from the relay — no process cleanup needed.

#### Scenario: Claude Code process exits
- **WHEN** the claude process exits and the tmux session ends
- **THEN** the relay SHALL detect the pipe-pane EOF, send a WebSocket close frame to all viewers, and clean up the ring buffer

#### Scenario: WebSocket disconnects
- **WHEN** the browser closes or the WebSocket connection drops
- **THEN** the WebSocket SHALL be unregistered from the relay. No process is killed. The relay continues capturing output for future viewers.

#### Scenario: Reattach to a session
- **WHEN** a new WebSocket connection is made to a still-running session
- **THEN** the server SHALL send terminal reset + ring buffer replay + live streaming. The viewer sees recent history and current state.

### Requirement: Native text selection and copy
xterm.js SHALL be configured with `copyOnSelect: true` and `rightClickSelectsWord: true`. Since tmux mouse reporting is disabled, xterm.js handles all mouse events natively — text selection, right-click context menu, and mouse wheel scrolling all work without toggles or workarounds.

#### Scenario: User selects text
- **WHEN** the user clicks and drags to select text in the terminal
- **THEN** xterm.js SHALL highlight the selection and automatically copy it to the clipboard

#### Scenario: User scrolls with mouse wheel
- **WHEN** the user scrolls the mouse wheel in the terminal
- **THEN** xterm.js SHALL scroll through its own scrollback buffer (populated by the ring buffer replay and live output)
