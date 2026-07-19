# pty-relay Delta

## ADDED Requirements

### Requirement: Backend bridges WebSocket viewers to sessiond
The backend SHALL connect each terminal WebSocket to the session's sessiond socket as a stateless per-connection bridge: WS BinaryMessages forward as DATA frames, WS resize TextMessages forward as CONTROL frames, and inbound SNAPSHOT/DATA/CONTROL frames forward as the corresponding WS messages. The bridge SHALL send the protocol ATTACH (with the viewer's dimensions) upon the viewer's first resize message. The bridge SHALL hold no per-session state — no PTY handles, no emulator, no viewer registry — and SHALL map a session-ended CLOSE frame to WS close code 1000 and any other termination to an abnormal close so the client's reconnect logic engages.

#### Scenario: Byte round-trip
- **WHEN** a viewer types and claude replies
- **THEN** input travels WS-binary → DATA → PTY and output travels PTY → DATA → WS-binary with no transformation in the backend

#### Scenario: Session ends
- **WHEN** sessiond sends a session-ended CLOSE frame
- **THEN** the bridge SHALL close the WebSocket with code 1000 and the client SHALL display "[Session ended]" without reconnecting

#### Scenario: Backend cannot reach sessiond
- **WHEN** the session socket cannot be dialed for a live-listed session
- **THEN** the bridge SHALL close the WebSocket abnormally (not 1000) so the client retries with backoff

## REMOVED Requirements

### Requirement: Bidirectional I/O via a directly-owned dtach attach PTY
**Reason**: dtach and the attach PTY are removed; sessiond owns the session PTY directly and the backend no longer touches PTYs.
**Migration**: I/O ownership: session-host "sessiond owns claude sessions end-to-end"; backend side: the ADDED bridge requirement above. Attach-drop reconnect scenarios have no equivalent — there is no attach subprocess to drop.

### Requirement: Resize relay via pty.Setsize
**Reason**: The backend no longer holds a PTY to resize; resize is a protocol CONTROL op applied by sessiond to its own PTY.
**Migration**: Mechanism: session-host protocol requirement; policy (active viewer, no-viewer sizing): multi-viewer-resize capability, unchanged in behavior.

### Requirement: Alternate screen tracking with dual output routing
**Reason**: Already superseded in code by the vt emulator (ring buffer and alt-screen filtering were replaced when emulator snapshots landed); this delta records the removal formally.
**Migration**: Alt-screen state is owned by the emulator; replay behavior: session-host "Snapshot rendered at requested dimensions with mode restoration".

### Requirement: Ring buffer per session
**Reason**: Already superseded in code by the vt emulator's scrollback; formally removed.
**Migration**: session-host "Continuous per-session terminal state".

### Requirement: Clean reconnect with terminal reset and replay
**Reason**: Replay is now the emulator snapshot rendered at the joining viewer's dimensions, delivered as a protocol SNAPSHOT frame.
**Migration**: session-host snapshot requirement plus the ADDED bridge requirement.

### Requirement: Relay state is free of data races
**Reason**: The relay's shared mutable state moves into sessiond's per-session actor; the backend bridge is per-connection with no shared state.
**Migration**: The race-detector requirement lives on as the session-host "Race detector is clean" scenario.
