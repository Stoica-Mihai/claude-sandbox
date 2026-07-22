# chat-relay Specification

## Purpose
Backend-side bridge between chat WebSocket viewers and a sessiond chat session, mirroring `pty-relay`'s stateless-bridge role for the terminal kind. Covers the WS bridge variant for chat sessions and the one place the backend is allowed to look inside an event line: detecting `conversation_reset`/`init` to keep the session index correct across a `/clear`.

## Requirements

### Requirement: WebSocket bridge variant for chat sessions
For a session whose kind is `chat`, the backend SHALL bridge each chat WebSocket connection to the session's sessiond socket as follows: each `FrameData` line received from sessiond SHALL be forwarded as a WebSocket TextMessage (not BinaryMessage — chat event lines are complete UTF-8 JSON, unlike terminal PTY byte chunks); each WebSocket message representing a user input (a JSON envelope from the client) SHALL be forwarded to sessiond as a `FrameData` line for the actor to write to the child's stdin. The bridge SHALL NOT send an ATTACH with meaningful `cols`/`rows` for chat sessions (per `chat-session-host`, dimensions are not required) and SHALL NOT expect or forward a snapshot frame.

#### Scenario: Event line reaches the browser as text
- **WHEN** sessiond broadcasts one chat event line to the bridge
- **THEN** the bridge SHALL forward it to the browser as a WebSocket TextMessage, unmodified

#### Scenario: User input reaches the engine
- **WHEN** the browser sends a chat input message over the WebSocket
- **THEN** the bridge SHALL forward it to sessiond as a `FrameData` frame for the chat actor to write to the child's stdin

#### Scenario: No snapshot expected
- **WHEN** a chat WebSocket first attaches
- **THEN** the bridge SHALL NOT wait for or forward a snapshot frame before delivering live events

### Requirement: Session index re-key is a two-step tap across conversation_reset and the following system/init event
The chat bridge SHALL inspect every line it forwards only for `conversation_reset` and `system` (subtype `init`) event shapes (not the full event vocabulary). Verified against the pinned engine (2.1.215): a `conversation_reset` event's own `session_id` field is the OLD conversation uuid; its `new_conversation_id` field SHALL NOT be used as the re-key target — it does not reliably match the uuid the conversation actually continues under. On observing `conversation_reset`, the bridge SHALL record the old uuid and await the next `system`/`init` event on the same connection; that event's `session_id` SHALL be treated as the new uuid. On obtaining both, the backend SHALL update both the persisted session index (`SessionIndex`) and the in-memory live session record (`sessionStore`) to reference the new uuid in place — preserving the entry's `cwd`, `created`, and custom `name` — rather than deleting the old entry and creating an unrelated new one. All other event lines SHALL pass through the bridge unparsed.

#### Scenario: /clear re-keys the index in place
- **WHEN** a live chat session's client issues `/clear`, the engine emits `conversation_reset` (old uuid in `session_id`), and the following `system`/`init` event carries a new `session_id`
- **THEN** the backend SHALL update the session index entry to that new uuid while preserving its recorded `cwd`, `created` timestamp, and any custom name

#### Scenario: new_conversation_id is not used as the re-key target
- **WHEN** a `conversation_reset` event's `new_conversation_id` field differs from the `session_id` of the following `system`/`init` event
- **THEN** the backend SHALL re-key to the `system`/`init` event's `session_id`, not to `new_conversation_id`

#### Scenario: Live session record follows the re-key
- **WHEN** the session index is re-keyed after a `conversation_reset` + `init` pair
- **THEN** the in-memory live session record for that session name SHALL also be updated to the new conversation uuid, so `ListSessions`, kill, and rename continue to resolve correctly

#### Scenario: Unrelated events are not parsed for index effects
- **WHEN** the bridge forwards an event that is neither `conversation_reset` nor a `system`/`init` event
- **THEN** the backend SHALL NOT attempt any session-index mutation based on that event's content

#### Scenario: Re-key failure does not crash the bridge
- **WHEN** a `conversation_reset` is observed but no `system`/`init` event follows before the connection ends (or either event does not carry the expected fields)
- **THEN** the bridge SHALL log the anomaly and continue operating normally, and SHALL NOT terminate the WebSocket connection or the underlying session
