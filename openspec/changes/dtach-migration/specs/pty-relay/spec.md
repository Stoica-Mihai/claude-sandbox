# Spec: pty-relay

**Spec Path:** `specs/pty-relay/spec.md`
**Change Type:** MODIFIED

> Builds on the `custom-pty-relay` change. Only the transport and resize
> requirements change. The "Alternate screen tracking with dual output routing",
> "Ring buffer per session", and "Clean reconnect with terminal reset and replay"
> requirements are **unchanged** in behavior (the reconnect mechanism re-execs
> `dtach -a` instead of `socat`, but the observable reset-and-replay contract is
> identical).

---

## MODIFIED Requirements

### Requirement: Bidirectional I/O via a directly-owned dtach attach PTY
The relay SHALL connect WebSocket viewers to a session by owning a single
`dtach -a <socket> -E -z` attach process per session, started with a pseudo-
terminal the relay owns (`pty.Start`). Reading the PTY master yields pane output;
writing the PTY master sends input to the pane. The relay SHALL NOT use
`tmux pipe-pane`, `socat`, or a unix-socket listener. There SHALL be exactly one
attach process per session regardless of the number of connected viewers.

#### Scenario: Relay starts on session spawn
- **WHEN** a new session is spawned
- **THEN** the relay SHALL start a `dtach -a` attach under an owned PTY and begin reading output into the ring buffer

#### Scenario: Relay starts on session discovery
- **WHEN** an existing session is discovered on dashboard startup
- **THEN** the relay SHALL start a `dtach -a` attach for that session even if no WebSocket viewer is connected

#### Scenario: Input is sent via the attach PTY
- **WHEN** a WebSocket BinaryMessage with user input arrives
- **THEN** the relay SHALL write the bytes directly to the attach PTY master, which delivers them to the session. No process is spawned per keystroke.

#### Scenario: Attach process drops while master is alive
- **WHEN** the relay's attach PTY returns EOF but the session socket still exists
- **THEN** the relay SHALL re-exec `dtach -a` and swap the PTY under the relay mutex, then restart a single read loop guarded by a generation counter

#### Scenario: Master exits
- **WHEN** the relay's attach PTY returns EOF and the session socket no longer exists
- **THEN** the relay SHALL stop and close all viewer connections

### Requirement: Resize relay via pty.Setsize
The relay SHALL resize a session by calling `pty.Setsize` on the owned attach
PTY, which dtach forwards to the inner program as `SIGWINCH`. The relay SHALL NOT
shell out to `tmux resize-window`. The relay SHALL impose a size only when at
least one browser viewer is present; a session with no viewers SHALL keep its
current size. When viewers are present and the attach is (re)started, the relay
SHALL re-apply the last viewer dimensions, since dtach does not auto-adopt a
fresh client's size. The per-viewer "active typist wins" selection of which
dimensions to apply is unchanged.

#### Scenario: Active viewer resizes
- **WHEN** the active viewer reports new terminal dimensions
- **THEN** the relay SHALL call `pty.Setsize` with those dimensions and the inner program SHALL receive `SIGWINCH`

#### Scenario: Size applied on viewer connect
- **WHEN** a viewer connects or reconnects to a session
- **THEN** the relay SHALL apply that viewer's dimensions so the session adopts them rather than retaining a stale size

#### Scenario: No viewer present
- **WHEN** the relay attaches to or reconnects to a session that has no browser viewers
- **THEN** the relay SHALL NOT impose a size

### Requirement: Relay state is free of data races
The relay's mutable fields shared across goroutines SHALL be synchronized. The
attach PTY handle SHALL only be reassigned under the relay mutex. Activity
timestamps (`lastInputAt`, `lastResizeAt`) SHALL be accessed via atomics. The
alternate-screen flag read outside the read loop SHALL be accessed under the
relay mutex or via an atomic. `go test -race ./...` SHALL pass.

#### Scenario: Reconnect under concurrent input
- **WHEN** the relay re-execs its attach while a viewer is sending input
- **THEN** access to the PTY handle SHALL be serialized so no read or write targets a closed or half-swapped handle

#### Scenario: Race detector is clean
- **WHEN** the test suite is run with `-race`
- **THEN** no data races SHALL be reported in the relay
