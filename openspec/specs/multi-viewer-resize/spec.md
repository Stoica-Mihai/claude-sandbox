# multi-viewer-resize Specification

## Purpose
Per-viewer terminal dimension tracking and active-viewer-based session resizing (via `pty.Setsize` on sessiond's directly-owned session PTY) for multi-viewer sessions.

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
sessiond SHALL track which viewer is the "active" viewer (the one whose terminal dimensions the session PTY currently matches). When a viewer sends terminal input and is not the active viewer, sessiond SHALL resize the session PTY to that viewer's stored dimensions (delivering SIGWINCH to the program) before forwarding the input. This mimics tmux "window-size latest" behavior. sessiond SHALL impose a size only when at least one viewer is present; a session with no viewers SHALL keep its current size. The active-viewer slot SHALL only be assigned to a connection currently registered in the viewer set — a resize or input frame from an already-evicted connection SHALL NOT become the active viewer or change the PTY size.

#### Scenario: First viewer becomes active
- **WHEN** the first viewer attaches with its dimensions
- **THEN** sessiond SHALL set that viewer as the active viewer and resize the session PTY to those dimensions

#### Scenario: Non-active viewer sends input
- **WHEN** a viewer that is not the active viewer sends terminal input
- **THEN** sessiond SHALL set that viewer as the active viewer, resize the session PTY to its stored dimensions, and forward the input

#### Scenario: Active viewer sends input
- **WHEN** the active viewer sends terminal input
- **THEN** sessiond SHALL forward the input without resizing the PTY

#### Scenario: Non-active viewer sends resize message
- **WHEN** a viewer that is not the active viewer sends a resize control message
- **THEN** sessiond SHALL store the dimensions but SHALL NOT resize the PTY and SHALL NOT change the active viewer

#### Scenario: Evicted connection cannot become active
- **WHEN** a resize frame from a viewer arrives after sessiond evicted that viewer
- **THEN** sessiond SHALL ignore it for active-viewer selection and PTY sizing

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
All writes to a single viewer SHALL be serialized through that viewer's bounded outbound queue, drained by one writer per connection. Broadcast data and control messages for a viewer SHALL flow through the same queue so no two writes interleave. A viewer whose queue is full SHALL be evicted rather than blocking the session actor.

#### Scenario: Concurrent broadcast and control message
- **WHEN** output broadcast and a `deactivated` control message target the same viewer
- **THEN** both SHALL be enqueued on that viewer's queue and written sequentially by its single writer

#### Scenario: Full queue evicts the viewer
- **WHEN** a viewer's outbound queue is full when a message is offered
- **THEN** sessiond SHALL evict that viewer instead of blocking
