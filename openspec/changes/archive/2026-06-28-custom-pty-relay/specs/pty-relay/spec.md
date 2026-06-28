## ADDED Requirements

### Requirement: Bidirectional I/O via unix socket and pipe-pane
The system SHALL relay I/O between WebSocket viewers and tmux sessions using a unix socket per session. The relay SHALL create a unix socket listener at `/tmp/relay-<name>.sock`, then run `tmux pipe-pane -t <session> 'socat - UNIX-CONNECT:/tmp/relay-<name>.sock'` to connect tmux's pane I/O to the socket. Reading from the socket yields pane output. Writing to the socket sends input to the pane.

#### Scenario: Relay starts on session spawn
- **WHEN** a new tmux session is spawned
- **THEN** the system SHALL create a unix socket, run pipe-pane with socat, and start reading output into the ring buffer

#### Scenario: Relay starts on session discovery
- **WHEN** an existing tmux session is discovered on dashboard startup
- **THEN** the system SHALL set up the unix socket and pipe-pane for that session, even if no WebSocket viewer is connected

#### Scenario: socat connection drops
- **WHEN** the socat connection to the unix socket drops (e.g., socat exits)
- **THEN** the system SHALL re-establish pipe-pane and socat within 1 second

#### Scenario: Input is sent via unix socket
- **WHEN** a WebSocket BinaryMessage with user input arrives
- **THEN** the relay SHALL write the bytes directly to the unix socket, which delivers them to the pane's stdin via socat. No process is spawned per keystroke.

### Requirement: Alternate screen tracking with dual output routing
The relay SHALL track alternate screen state by detecting `\x1b[?1049h` (enter) and `\x1b[?1049l` (exit) sequences (and older `\x1b[?47h` / `\x1b[?47l` variants) in the output stream. The sequences themselves SHALL be stripped from output sent to WebSocket viewers so xterm.js remains in normal screen mode. Output routing depends on the current state:

- **Normal screen mode** (default): Output is written to both the ring buffer AND broadcast to WebSocket viewers. This captures conversation content (questions, answers, code).
- **Alternate screen mode**: Output is broadcast to WebSocket viewers only (for live TUI rendering) but NOT written to the ring buffer. This discards TUI chrome (banners, spinners, prompts) from history.

#### Scenario: Claude Code enters alternate screen
- **WHEN** Claude Code sends `\x1b[?1049h` (smcup)
- **THEN** the relay SHALL strip the sequence from viewer output, set the internal state to alternate-screen-on, and stop writing subsequent output to the ring buffer

#### Scenario: Claude Code exits alternate screen
- **WHEN** Claude Code sends `\x1b[?1049l` (rmcup)
- **THEN** the relay SHALL strip the sequence from viewer output, set the internal state to alternate-screen-off, and resume writing subsequent output to the ring buffer

#### Scenario: TUI renders during alternate screen
- **WHEN** Claude Code renders its TUI (banner, spinner, prompt) while in alternate screen mode
- **THEN** the output SHALL be forwarded to WebSocket viewers (TUI is visible live) but SHALL NOT be stored in the ring buffer (history stays clean)

#### Scenario: Conversation output in normal screen
- **WHEN** Claude Code writes conversation content (answers, code output) while in normal screen mode
- **THEN** the output SHALL be both forwarded to viewers AND stored in the ring buffer for future replay

### Requirement: Ring buffer per session
The system SHALL maintain a fixed-size circular byte buffer (default 1MB) per session that stores recent terminal output from normal screen mode only. The buffer SHALL be written to continuously from the unix socket reader (when not in alternate screen mode), regardless of whether any WebSocket viewer is connected.

#### Scenario: Buffer wraps around
- **WHEN** the ring buffer reaches capacity and new data arrives
- **THEN** the oldest data SHALL be overwritten, and the buffer SHALL always contain the most recent output up to its capacity

#### Scenario: Buffer is read for replay
- **WHEN** a new WebSocket viewer connects
- **THEN** the system SHALL return the ring buffer contents in chronological order (oldest first) for replay

### Requirement: Resize relay via tmux resize-window
The system SHALL relay terminal resize events to the tmux session by running `tmux resize-window -t <session> -x <cols> -y <rows>` when a WebSocket TextMessage with resize data is received.

#### Scenario: Browser window resized
- **WHEN** a resize JSON message with cols and rows is received
- **THEN** the system SHALL run `tmux resize-window` to update the pane dimensions

### Requirement: Clean reconnect with terminal reset and replay
When a WebSocket viewer connects, the system SHALL send a terminal reset sequence (`\x1bc`) followed by the ring buffer contents before starting live output streaming. This ensures the viewer starts with a clean terminal state and sees recent history.

#### Scenario: Viewer reconnects to active session
- **WHEN** a new WebSocket connection is made to an active session
- **THEN** the server SHALL send `\x1bc` (reset), then the ring buffer contents, then live output from the unix socket. The viewer SHALL see a clean terminal with recent history.

#### Scenario: First viewer connects to session with accumulated buffer
- **WHEN** a session has been running for 10 minutes with no viewer and a viewer connects
- **THEN** the viewer SHALL see up to 1MB of recent output replayed, followed by live streaming
