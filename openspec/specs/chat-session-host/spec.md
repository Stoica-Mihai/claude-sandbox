# chat-session-host Specification

## Purpose
sessiond's second session kind: a pipe-child actor that hosts a claude conversation over stream-json (no PTY, no terminal emulator) and relays JSON event lines to viewers exactly as opaquely as the terminal kind relays PTY bytes. Covers the pinned engine version this kind depends on, the chat actor's process and viewer model, and its deliberate simplicity relative to the terminal actor (`session-host`).

## Requirements

### Requirement: Pinned claude engine version
The `sessions` container image SHALL install a specific, pinned version of the claude CLI rather than whatever `install.sh` resolves as latest at build time. The pinned version SHALL be the one the stream-json event vocabulary this capability depends on was verified against. Bumping the pinned version SHALL be a deliberate, reviewed change, not an incidental side effect of a routine image rebuild.

#### Scenario: Image build installs the pinned version
- **WHEN** `Dockerfile.sessions` is built
- **THEN** the resulting image SHALL contain the pinned claude CLI version, not whatever version `install.sh` would otherwise resolve as latest

#### Scenario: Version bump is explicit
- **WHEN** a maintainer wants a newer claude engine
- **THEN** they SHALL edit the version pin in `Dockerfile.sessions` explicitly, and the change SHALL be reviewable as a diff to that pin

### Requirement: Chat session actor spawns a pipe-child, not a PTY
sessiond SHALL support a `chat` session kind whose actor spawns `claude -p --session-id <uuid> --input-format stream-json --output-format stream-json --include-partial-messages --dangerously-skip-permissions` (or with `--resume <uuid>` for resumes) as a plain child process connected via stdin/stdout pipes — no `pty.Start`, no `TERM` environment variable requirement tied to terminal rendering. The working directory and auth/config environment SHALL match the terminal kind's spawn (same `cwd`, same `$CLAUDE_CONFIG_DIR`).

#### Scenario: Spawn a new chat conversation
- **WHEN** a SPAWN op arrives with `kind=chat`, a `cwd`, and a conversation uuid
- **THEN** sessiond SHALL start the pipe-child claude process in stream-json mode in that directory and reply with the generated session name

#### Scenario: Resume a chat conversation
- **WHEN** a SPAWN op arrives with `kind=chat`, `resume=true`, and an existing conversation uuid
- **THEN** sessiond SHALL start the pipe-child with `--resume <uuid>` in the conversation's recorded working directory

### Requirement: Chat actor relays stdout lines and stdin writes without interpreting them
The chat session actor SHALL read the child's stdout line-by-line and broadcast each complete line verbatim to every registered, non-detached viewer, and SHALL write each viewer-submitted input line to the child's stdin in the order received. The actor SHALL NOT parse, validate, or branch on the JSON content of any line — it is a dumb relay, exactly as the terminal actor's PTY byte relay does not interpret PTY output.

#### Scenario: One stdout line reaches every viewer
- **WHEN** the claude process writes one complete JSON event line to stdout
- **THEN** every registered viewer SHALL receive that line, and no viewer SHALL receive a partial or merged line

#### Scenario: Queued input is written in order
- **WHEN** a viewer submits two input messages while the engine is still processing the first
- **THEN** the actor SHALL write them to stdin in the order they were submitted, without reordering or dropping either

#### Scenario: Actor does not branch on event content
- **WHEN** any stdout line is broadcast
- **THEN** sessiond SHALL NOT have parsed the line's JSON to decide viewer-visible behavior (e.g. it SHALL NOT special-case `conversation_reset` or any other event type)

### Requirement: No terminal emulator, snapshot, or DEC-mode state for chat sessions
The chat session actor SHALL NOT own a terminal emulator, SHALL NOT render or send a snapshot frame on attach, and SHALL NOT track or re-assert terminal modes. A joining viewer SHALL receive only events broadcast from the moment it attaches onward; prior history is out of sessiond's scope for this kind (see `chat-relay`'s transcript endpoint for history).

#### Scenario: Attach receives no snapshot
- **WHEN** a viewer attaches to a live chat session
- **THEN** sessiond SHALL NOT send a snapshot frame, and the viewer SHALL begin receiving only events broadcast after that point

### Requirement: ATTACH carries no dimensions for chat sessions
The ATTACH handshake for a chat session stream SHALL NOT require `cols`/`rows` to be non-zero; sessiond SHALL accept the attach regardless of the dimension values carried (they are meaningless for a non-PTY child).

#### Scenario: Attach with zero dimensions succeeds
- **WHEN** a viewer sends ATTACH with `cols=0, rows=0` to a chat session's socket
- **THEN** sessiond SHALL register the viewer and begin relaying events, not reject the attach

### Requirement: Multi-viewer chat sessions are pure broadcast — no active-viewer suspension
Every registered viewer of a chat session SHALL receive every broadcast event; there SHALL be no active/suspended viewer distinction, no PTY-size-driven resize arbitration, and no `deactivated` control messages for chat sessions.

#### Scenario: Two viewers on the same chat session both see all events
- **WHEN** two viewers are attached to the same chat session and the engine emits three events
- **THEN** both viewers SHALL receive all three events, and neither SHALL be suspended or notified of deactivation

### Requirement: Kill, exit, and registry lifecycle are shared with the terminal kind
A chat session SHALL support the same control-socket lifecycle as a terminal session: it SHALL appear in LIST results (including its conversation uuid, cwd, and creation time), SHALL be terminable via KILL (SIGTERM to the process group, escalating to SIGKILL after the existing grace period), and SHALL be removed from the registry and have its socket cleaned up when its child process exits, exactly as the terminal kind is today. Exactly one live child (of either kind) SHALL exist per conversation uuid at any time.

#### Scenario: Kill a chat session
- **WHEN** a KILL op arrives for a live chat session
- **THEN** sessiond SHALL signal the process group SIGTERM, escalate to SIGKILL after the grace period if needed, close all attach connections with a session-ended close, and remove the session from its registry

#### Scenario: Chat session appears in LIST
- **WHEN** a LIST op arrives while a chat session is live
- **THEN** the response SHALL include that session's name, cwd, creation time, and conversation uuid alongside any live terminal sessions

#### Scenario: One live child per conversation
- **WHEN** a conversation uuid already has a live child (terminal or chat) and a new SPAWN for the same uuid arrives
- **THEN** sessiond SHALL NOT run two children for the same uuid simultaneously — the caller is expected to kill the existing child before spawning the other kind (mode switch), per the `chat-relay`/spawn-UX capabilities' orchestration
