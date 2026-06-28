# multi-viewer-resize Specification

## Purpose
Per-viewer terminal dimension tracking and active-viewer-based session resizing (via `pty.Setsize` on the relay's owned `dtach -a` attach PTY) for multi-viewer relay sessions.

## Requirements
### Requirement: Per-viewer dimension tracking
The relay SHALL track each connected viewer's terminal dimensions (cols, rows) independently. Dimensions SHALL be stored when the viewer sends a resize control message and SHALL be removed when the viewer disconnects.

#### Scenario: Viewer reports dimensions on connect
- **WHEN** a viewer's WebSocket connects and sends an initial resize message with cols and rows
- **THEN** the relay SHALL store those dimensions for that viewer

#### Scenario: Viewer reports dimensions on window resize
- **WHEN** a connected viewer sends a resize control message with updated cols and rows
- **THEN** the relay SHALL update the stored dimensions for that viewer

#### Scenario: Viewer disconnects
- **WHEN** a viewer's WebSocket connection closes
- **THEN** the relay SHALL remove the stored dimensions for that viewer

### Requirement: Active-viewer resize on input
The relay SHALL track which viewer is the "active" viewer (the one whose terminal dimensions the attach PTY currently matches). When a viewer sends terminal input and is not the active viewer, the relay SHALL resize the session by calling `pty.Setsize` on the owned `dtach -a` attach PTY (which dtach forwards to the session as SIGWINCH) to that viewer's stored dimensions before forwarding the input. This mimics tmux "window-size latest" behavior, but the mechanism is `pty.Setsize`, not tmux. The relay SHALL impose a size only when at least one viewer is present; a session with no viewers SHALL keep its current size.

#### Scenario: First viewer becomes active
- **WHEN** the first viewer connects and sends a resize message
- **THEN** the relay SHALL set that viewer as the active viewer and resize the attach PTY (via `pty.Setsize`) to their dimensions

#### Scenario: Non-active viewer sends input
- **WHEN** a viewer that is not the active viewer sends a BinaryMessage (terminal input)
- **THEN** the relay SHALL set that viewer as the active viewer, resize the attach PTY (via `pty.Setsize`) to that viewer's stored dimensions, and forward the input to the attach PTY

#### Scenario: Active viewer sends input
- **WHEN** the active viewer sends a BinaryMessage (terminal input)
- **THEN** the relay SHALL forward the input without resizing the attach PTY

#### Scenario: Non-active viewer sends resize message
- **WHEN** a viewer that is not the active viewer sends a resize control message (TextMessage)
- **THEN** the relay SHALL store the dimensions but SHALL NOT resize the attach PTY and SHALL NOT change the active viewer

### Requirement: Viewer suspension
When the active viewer changes, the relay SHALL suspend all non-active viewers. Suspended viewers SHALL NOT receive broadcast output data. Suspended viewers SHALL receive control messages (text frames).

#### Scenario: Active viewer changes
- **WHEN** a non-active viewer sends input and becomes the active viewer
- **THEN** the relay SHALL mark all other viewers as suspended

#### Scenario: Broadcast skips suspended viewers
- **WHEN** the relay broadcasts output from the attach PTY
- **THEN** suspended viewers SHALL NOT receive the broadcast data

#### Scenario: Suspended viewer receives deactivation notification
- **WHEN** a viewer is suspended
- **THEN** the relay SHALL send a `{"type":"deactivated"}` WebSocket TextMessage to that viewer

#### Scenario: Viewer unsuspends on input
- **WHEN** a suspended viewer sends a BinaryMessage (terminal input)
- **THEN** the relay SHALL unsuspend that viewer before processing the input

### Requirement: Per-connection write serialization
All WebSocket writes to a single viewer connection SHALL be serialized via a per-connection mutex. This prevents concurrent write corruption between the broadcast goroutine and control message writes.

#### Scenario: Concurrent broadcast and control message
- **WHEN** the broadcast goroutine and the handleWebSocket goroutine both attempt to write to the same viewer connection
- **THEN** writes SHALL be serialized by the per-connection mutex so only one write occurs at a time
