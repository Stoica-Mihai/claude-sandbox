## MODIFIED Requirements

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
