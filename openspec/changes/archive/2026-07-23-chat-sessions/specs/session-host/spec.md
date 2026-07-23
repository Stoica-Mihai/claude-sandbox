# session-host Delta

## MODIFIED Requirements

### Requirement: Unix-socket protocol
sessiond SHALL listen on a control socket (`control.sock`) for request/response ops — SPAWN, LIST, KILL — and on one socket per session for attach streams. Frames SHALL be length-prefixed (`type u8`, `len u32`, payload) with types DATA (raw PTY bytes for terminal sessions, opaque JSON event/input lines for chat sessions — both directions), CONTROL (JSON control messages), SNAPSHOT (rendered replay, terminal sessions only), and CLOSE. The ATTACH handshake SHALL carry the viewer's initial dimensions for terminal sessions; for chat sessions dimensions are not required (see `chat-session-host`). A terminal ATTACH SHALL be answered with a SNAPSHOT frame followed by live DATA; a chat ATTACH SHALL be answered with live DATA only, no SNAPSHOT. The CONTROL JSON shapes SHALL mirror the WebSocket text-message contract (`resize`, `deactivated`) byte-compatibly so the backend bridge translates without interpretation. The SPAWN request SHALL carry a `kind` field (`terminal` or `chat`); an absent or empty `kind` SHALL be treated as `terminal` for backward compatibility.

#### Scenario: Attach handshake (terminal)
- **WHEN** a client connects to a terminal session socket and sends ATTACH with `{cols, rows}`
- **THEN** sessiond SHALL register it as a viewer, reply with one SNAPSHOT frame, and stream subsequent output as DATA frames

#### Scenario: Attach handshake (chat)
- **WHEN** a client connects to a chat session socket and sends ATTACH
- **THEN** sessiond SHALL register it as a viewer and stream subsequent events as DATA frames, with no SNAPSHOT frame

#### Scenario: List sessions
- **WHEN** a LIST op arrives on the control socket
- **THEN** sessiond SHALL reply with every live session's name, cwd, creation time, and conversation uuid from its in-memory registry, regardless of kind

#### Scenario: SPAWN without a kind defaults to terminal
- **WHEN** a SPAWN op arrives with no `kind` field (or an empty one)
- **THEN** sessiond SHALL spawn a terminal (PTY) session, exactly as before this capability existed

## ADDED Requirements

### Requirement: Kind-aware session dispatch
The registry SHALL dispatch a SPAWN op to the PTY-based terminal actor or the pipe-based chat actor based on the request's `kind` field. LIST, KILL, and shutdown SHALL operate uniformly across both kinds through a shared minimal interface (kill, exit-notification, listener serving) — a caller of LIST or KILL SHALL NOT need to know or care which kind a session is.

#### Scenario: Spawn dispatches by kind
- **WHEN** a SPAWN op arrives with `kind=chat`
- **THEN** the registry SHALL start the chat actor (see `chat-session-host`) rather than the PTY actor

#### Scenario: Kill is kind-agnostic
- **WHEN** a KILL op arrives for a session name
- **THEN** the registry SHALL terminate it the same way regardless of whether it is a terminal or chat session

#### Scenario: One live child per conversation uuid across kinds
- **WHEN** a conversation uuid has a live terminal session and a SPAWN for the same uuid with `kind=chat` arrives without the terminal session first being killed
- **THEN** the registry's behavior SHALL NOT result in two simultaneously live children for the same uuid — a caller performing a mode switch is expected to kill the existing child first
