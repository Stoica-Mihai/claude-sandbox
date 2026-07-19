# pty-relay Specification

## Purpose
Backend-side bridge between terminal WebSocket viewers and sessiond. Each WebSocket connection is a stateless per-connection bridge to the session's sessiond socket, translating WS messages to protocol frames and back. The backend holds no PTYs, no emulators, and no viewer registry — session I/O, terminal state, and viewer fan-out are owned by sessiond (see `session-host`).

## Requirements

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
