## MODIFIED Requirements

### Requirement: WebSocket reconnection indicator
When the WebSocket connection to a terminal session drops unexpectedly (not due to process exit), the terminal pane SHALL display a visible reconnection indicator so the user knows the system is attempting to restore the connection. The indicator SHALL disappear on successful reconnection or after all retries are exhausted.

#### Scenario: Connection drops unexpectedly
- **WHEN** the WebSocket connection closes with a code other than normal closure (1000) or going away (1001) while the PTY process is still running
- **THEN** the terminal SHALL display a "[Reconnecting... (attempt 1)]" message in dim gray text and begin the automatic reconnection sequence

#### Scenario: Reconnection attempt in progress
- **WHEN** a reconnection attempt is made
- **THEN** the terminal SHALL display "[Reconnecting... (attempt N)]" showing the current attempt number, so the user can see that retries are progressing

#### Scenario: Reconnection succeeds
- **WHEN** the WebSocket successfully reconnects to the same terminal ID
- **THEN** the terminal SHALL display "[Reconnected]" in green text, the server SHALL replay the scrollback buffer (existing behavior), and normal terminal interaction SHALL resume without the user needing to take any action

#### Scenario: All reconnection attempts exhausted
- **WHEN** the maximum number of reconnection attempts is reached without a successful connection
- **THEN** the terminal SHALL display "[Connection lost. Click to retry.]" in red text and stop automatic retries. The user SHALL be able to click the terminal area to trigger a fresh reconnection sequence.

#### Scenario: Process exit vs connection drop
- **WHEN** the WebSocket closes with a normal closure code (1000) and the close reason indicates "process exited"
- **THEN** the terminal SHALL display "[Session ended]" (existing behavior) and SHALL NOT attempt reconnection, because the PTY process has terminated and there is nothing to reconnect to
