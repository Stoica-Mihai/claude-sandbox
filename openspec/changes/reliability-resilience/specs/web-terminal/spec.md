## MODIFIED Requirements

### Requirement: WebSocket auto-reconnect with exponential backoff
When the WebSocket connection to a terminal drops unexpectedly, the client SHALL automatically attempt to reconnect using exponential backoff. The reconnection logic SHALL distinguish between a normal session end (process exited) and an unexpected connection loss (network issue, server restart).

#### Scenario: Unexpected WebSocket close triggers reconnect
- **WHEN** the WebSocket `onclose` event fires with a code other than 1000 (normal closure) and the close reason does not contain "process exited"
- **THEN** the client SHALL initiate an automatic reconnection sequence using exponential backoff starting at 1 second

#### Scenario: Exponential backoff timing
- **WHEN** reconnection attempts are made
- **THEN** the delay between attempts SHALL follow the pattern: 1s, 2s, 4s, 8s, 16s, capped at 30s. Each subsequent attempt SHALL double the previous delay until the cap is reached. The delays SHALL remain at 30s for all subsequent attempts after the cap.

#### Scenario: Successful reconnection
- **WHEN** a reconnection WebSocket handshake succeeds
- **THEN** the client SHALL reset the backoff counter to zero, re-send a resize message with the current terminal dimensions, and the server SHALL replay the scrollback buffer (existing server-side behavior). The terminal SHALL resume normal bidirectional I/O. User input typed during the disconnection period SHALL NOT be buffered or replayed.

#### Scenario: Maximum retry limit reached
- **WHEN** 10 consecutive reconnection attempts fail without a successful connection
- **THEN** the client SHALL stop automatic reconnection and display a message indicating the connection was lost. The user SHALL be able to manually trigger a new reconnection sequence (e.g., by clicking the terminal area).

#### Scenario: Normal session end does not trigger reconnect
- **WHEN** the WebSocket closes with code 1000 (normal closure) and the close reason is "process exited"
- **THEN** the client SHALL NOT attempt reconnection and SHALL display "[Session ended]" as before

#### Scenario: Page visibility change during backoff
- **WHEN** the browser tab is hidden (e.g., user switches tabs) while a reconnection backoff timer is running
- **THEN** the reconnection timer SHALL continue running. When the tab becomes visible again and the WebSocket is still disconnected, the next reconnection attempt SHALL proceed at the current backoff interval without resetting.

#### Scenario: Manual session destroy during reconnect
- **WHEN** the user kills the session from the sidebar while a reconnection sequence is in progress
- **THEN** the reconnection sequence SHALL be cancelled immediately and the terminal SHALL display "[Session ended]" without further retry attempts

### Requirement: Scrollback replay on reconnect
The existing server-side scrollback replay (ring buffer contents sent on WebSocket attach) SHALL work transparently with the reconnection mechanism. No changes are needed to the server-side replay logic.

#### Scenario: Reconnect replays recent output
- **WHEN** the WebSocket reconnects after a temporary disconnection
- **THEN** the server SHALL replay the scrollback ring buffer contents to the new WebSocket connection (existing behavior), and the terminal SHALL display the replayed output so the user sees the current state of the session including any output produced while disconnected
