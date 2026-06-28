# pty-relay Specification

## Purpose
Server-side relay connecting WebSocket viewers to a dtach session via a directly-owned `dtach -a` attach PTY. Covers bidirectional I/O, resize, alternate-screen routing, the per-session ring buffer, clean reconnect, and race-free shared state. One relay per session, one attach, N viewers.

## Requirements

### Requirement: Bidirectional I/O via a directly-owned dtach attach PTY
The relay SHALL connect WebSocket viewers to a session by owning a single `dtach -a <socket> -E -z -r none` attach process per session, started with a pseudo-terminal the relay owns (`pty.Start`). Reading the PTY master yields pane output; writing the PTY master sends input to the pane. The relay SHALL NOT use `tmux pipe-pane`, `socat`, or a unix-socket listener. There SHALL be exactly one attach process per session regardless of the number of connected viewers.

#### Scenario: Relay starts on session spawn
- **WHEN** a new session is spawned
- **THEN** the relay SHALL start a `dtach -a` attach under an owned PTY and begin reading output into the ring buffer

#### Scenario: Relay starts on session discovery
- **WHEN** an existing session is discovered on startup
- **THEN** the relay SHALL start a `dtach -a` attach for that session even if no WebSocket viewer is connected

#### Scenario: Input is sent via the attach PTY
- **WHEN** a WebSocket BinaryMessage with user input arrives
- **THEN** the relay SHALL write the bytes directly to the attach PTY master. No process is spawned per keystroke.

#### Scenario: Attach process drops while master is alive
- **WHEN** the relay's attach PTY returns EOF but the session is still alive
- **THEN** the relay SHALL re-exec `dtach -a`, swap the PTY under the relay mutex, and restart a single read loop guarded by a generation counter

#### Scenario: Master exits
- **WHEN** the relay's attach PTY returns EOF and the session is no longer alive
- **THEN** the relay SHALL stop and close all viewer connections

### Requirement: Resize relay via pty.Setsize
The relay SHALL resize a session by calling `pty.Setsize` on the owned attach PTY, which dtach forwards to the inner program as `SIGWINCH`. The relay SHALL NOT shell out to `tmux resize-window`. The relay SHALL impose a size only when at least one browser viewer is present; a session with no viewers SHALL keep its current size. When viewers are present and the attach is (re)started, the relay SHALL re-apply the last viewer dimensions, since dtach does not auto-adopt a fresh client's size. The per-viewer "active typist wins" selection of which dimensions to apply is unchanged.

#### Scenario: Active viewer resizes
- **WHEN** the active viewer reports new terminal dimensions
- **THEN** the relay SHALL call `pty.Setsize` with those dimensions and the inner program SHALL receive `SIGWINCH`

#### Scenario: Size applied on viewer connect
- **WHEN** a viewer connects or reconnects to a session
- **THEN** the relay SHALL apply that viewer's dimensions so the session adopts them rather than retaining a stale size

#### Scenario: No viewer present
- **WHEN** the relay attaches to or reconnects to a session that has no browser viewers
- **THEN** the relay SHALL NOT impose a size

### Requirement: Alternate screen tracking with dual output routing
The relay SHALL track alternate screen state by detecting `\x1b[?1049h` (enter) and `\x1b[?1049l` (exit) sequences (and older `\x1b[?47h` / `\x1b[?47l` variants) in the output stream. Output routing depends on the current state:

- **Normal screen mode** (default): output is written to both the ring buffer AND broadcast to viewers. This captures conversation content.
- **Alternate screen mode**: output is broadcast to viewers only (for live TUI rendering) but NOT written to the ring buffer. This keeps TUI chrome (banners, spinners, prompts) out of replay history.

The raw stream (including the alt-screen sequences) is broadcast to viewers so xterm.js manages the alternate screen itself; only the ring buffer is filtered.

#### Scenario: Claude Code enters alternate screen
- **WHEN** Claude Code sends `\x1b[?1049h`
- **THEN** the relay SHALL set its internal state to alternate-screen-on and stop writing subsequent output to the ring buffer

#### Scenario: Claude Code exits alternate screen
- **WHEN** Claude Code sends `\x1b[?1049l`
- **THEN** the relay SHALL set its internal state to alternate-screen-off and resume writing subsequent output to the ring buffer

#### Scenario: Sequence split across read chunks
- **WHEN** an alternate-screen sequence is split across two PTY read chunks
- **THEN** the relay SHALL buffer the partial prefix and complete the match on the next chunk before toggling state

### Requirement: Ring buffer per session
The system SHALL maintain a fixed-size circular byte buffer (default 1MB) per session that stores recent normal-screen output. The buffer SHALL be written continuously from the attach PTY reader (when not in alternate screen mode), regardless of whether any viewer is connected.

#### Scenario: Buffer wraps around
- **WHEN** the ring buffer reaches capacity and new data arrives
- **THEN** the oldest data SHALL be overwritten, and the buffer SHALL always contain the most recent output up to its capacity

#### Scenario: Buffer is read for replay
- **WHEN** a new viewer connects
- **THEN** the system SHALL return the ring buffer contents in chronological order for replay

### Requirement: Clean reconnect with terminal reset and replay
When a viewer connects, the relay SHALL send a terminal reset (`\x1bc`) followed by the ring buffer contents before live streaming, so the viewer starts clean with recent history.

#### Scenario: Viewer connects to active session
- **WHEN** a new WebSocket connection is made to an active session
- **THEN** the relay SHALL send `\x1bc`, then the ring buffer contents, then live output

### Requirement: Relay state is free of data races
The relay's mutable fields shared across goroutines SHALL be synchronized. The attach PTY handle SHALL only be reassigned under the relay mutex (or via an atomic pointer). Activity timestamps SHALL be accessed via atomics. The alternate-screen flag read outside the read loop SHALL be accessed via an atomic. `go test -race ./...` SHALL pass.

#### Scenario: Reconnect under concurrent input
- **WHEN** the relay re-execs its attach while a viewer is sending input
- **THEN** access to the PTY handle SHALL be serialized so no read or write targets a closed or half-swapped handle

#### Scenario: Race detector is clean
- **WHEN** the test suite is run with `-race`
- **THEN** no data races SHALL be reported in the relay
