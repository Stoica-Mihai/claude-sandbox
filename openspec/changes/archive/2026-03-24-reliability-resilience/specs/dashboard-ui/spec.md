## MODIFIED Requirements

### Requirement: WebSocket reconnection indicator
When the WebSocket connection drops unexpectedly (close code != 1000), the terminal SHALL display reconnection status messages inline using `term.write()`.

#### Scenario: Connection drops
- **WHEN** the WebSocket closes with a code other than 1000
- **THEN** the terminal SHALL display "[Reconnecting... (attempt 1)]" in dim gray text

#### Scenario: Reconnection attempt in progress
- **WHEN** each subsequent reconnection attempt is made
- **THEN** the terminal SHALL display "[Reconnecting... (attempt N)]" showing the attempt number

#### Scenario: Reconnection succeeds
- **WHEN** the WebSocket successfully reconnects
- **THEN** the terminal SHALL display "[Reconnected]" in green text and resume normal interaction

#### Scenario: All retries exhausted
- **WHEN** 10 consecutive attempts fail
- **THEN** the terminal SHALL display "[Connection lost]" in red text and stop retrying
