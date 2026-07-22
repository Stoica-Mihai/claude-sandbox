## Why

Every session today is an xterm.js terminal over a PTY. That surface carries real structural cost — snapshot repaint on attach, DEC-mode re-assertion, active-viewer resize arbitration, suspended-viewer freezes — and a poor phone experience (a software keyboard driving a raw terminal). `docs/chat-sessions-design.md` verifies, against the pinned claude engine in stream-json mode, that a structured chat surface is buildable directly in Go with no SDK/Node dependency: typed JSON events over stdio, no PTY, no emulator. Chat and terminal become two first-class ways to drive the same conversation, so a session started on a phone in chat mode can be picked up at a desk in terminal mode and back again, with no decision yet about which one ultimately wins (design doc §11, deferred).

## What Changes

- Pin the claude engine version in `Dockerfile.sessions` (currently `install.sh` grabs latest at build time) so the stream-json event vocabulary this change depends on cannot silently drift.
- sessiond gains a second session kind (`chat`) alongside the existing PTY-based `terminal` kind: a pipe-child actor that spawns `claude -p --session-id <uuid> --input-format stream-json --output-format stream-json --include-partial-messages --dangerously-skip-permissions`, relays each stdout JSON line to viewers, and writes queued input messages to stdin. It is deliberately dumber than the terminal actor — no emulator, no snapshot, no DEC-mode tracking, no active-viewer size arbitration, no JSON interpretation. ATTACH carries no dimensions for chat sessions; every viewer sees every event (pure broadcast, no suspension).
- The SPAWN control op gains a `kind` field (`terminal` default, or `chat`); LIST/KILL/close semantics are unchanged and kind-agnostic. Exactly one live child exists per conversation uuid regardless of kind.
- Mode switch works in both directions: kill the current child, respawn the other kind with `--resume <uuid>`. The session index (`uuid → {cwd, created, name}`) is unchanged — kind is a property of the live child, not the stored conversation.
- Backend gains a WS bridge variant for chat sessions: event lines become WS text frames; user input (including image-attach-via-path and `/clear`) becomes JSON messages written to stdin. The bridge inspects only `conversation_reset`/`init` events to re-key (or record lineage for) the session index entry when `/clear` mints a new conversation id; every other event passes through unparsed.
- New `GET /api/sessions/{uuid}/transcript` endpoint serves a conversation's JSONL transcript so the chat UI can render history on open/reconnect instead of requiring a live snapshot.
- New chat UI: markdown message list with streaming partial text, collapsed-by-default thinking blocks, one collapsible row per tool call (diffs for Edit/Write, command+output excerpt for Bash), an input bar with send / queue-while-running / image attach (via the existing upload volume, file-path reference — never inline base64) and a required `/clear` command, a header with cwd/model/live cost-token ticker, and mode-switch + kill actions. Agent questions arrive as plain text (headless mode has no `AskUserQuestion` menu events) and are answered in the input bar.
- The NEW SESSION modal gains a mode choice (Terminal | Chat, no global default — remembers the last used choice) that applies to both starting a new session and resuming a previous one, so any recorded conversation can be resumed in either surface regardless of which kind it last ran as.
- Excluded from this change (parked, design doc P4): supervisor JSON-answer integration, fleet/semantic activity cards fed from the chat event stream, dictation polish. No terminal-lane behavior changes — xterm.js, the PTY actor, and the existing WS terminal bridge are untouched.

## Capabilities

### New Capabilities
- `chat-session-host`: sessiond's chat-kind session actor — pipe-child spawn/kill/attach, stdout-line broadcast to viewers, stdin writes from queued input, no emulator/snapshot/DEC-mode/active-viewer machinery, pinned claude engine version.
- `chat-relay`: the backend's WS bridge variant for chat sessions (event lines ↔ WS text frames, JSON input ↔ stdin) and its `conversation_reset`/`init` stream tap that re-keys the session index when `/clear` mints a new conversation id.
- `chat-ui`: the frontend chat rendering surface (markdown/streaming/tool-step rows/cost header/input bar/mode-switch controls) and the session-kind spawn/resume UX (Terminal | Chat choice in the NEW SESSION modal and the resume list).

### Modified Capabilities
- `session-host`: the SPAWN request/response and the registry's dispatch gain a `kind` (terminal|chat), routing to the PTY actor or the new chat actor while keeping LIST/KILL/close and the one-live-child-per-uuid invariant kind-agnostic.
- `session-api`: `SpawnRequest`/`DisplaySession` wire types gain `kind`; a new transcript endpoint (`GET /api/sessions/{terminalId}/transcript`) serves the conversation's JSONL history.

## Impact

- **Infra**: `Dockerfile.sessions` (pin claude version instead of `curl .../install.sh | bash` grabbing latest).
- **shared/**: `shared/types.go` (session kind enum, `SpawnRequest.Kind`, `DisplaySession.Kind`, chat event/control envelope types), `shared/routes.go` (transcript route), `shared/enums.go` (kind allowlist) — single source for backend + frontend.
- **sessiond/**: new chat actor variant (`sessiond/chatsession.go` or similar), `sessiond/registry.go` (kind-aware spawn dispatch), `sessiond/protocol/protocol.go` (Request.Kind, chat frame shapes if any beyond existing DATA/CONTROL).
- **backend/**: `backend/handlers.go` (chat WS bridge variant, transcript handler), `backend/session.go`/`sessionindex.go` (kind plumbing, conversation_reset re-key), `backend/lifecycle.go` (spawn/resume kind parameter).
- **frontend/**: new chat UI module(s) under `frontend/web/static/js/`, `frontend/web/templates/` (mode choice in the NEW SESSION modal, resume list, chat message templates/rendering), `frontend/web/static/css/app.css` (chat surface styling, Futurism-conformant).
- **Docker**: no compose/volume changes expected (chat sessions reuse the existing sockets and uploads volume); a version pin is a Dockerfile-only change.
- No changes to `web-terminal`, `pty-relay`, `multi-viewer-resize`, `dtach-sessions`, or `dashboard-ui`'s terminal-specific requirements — the terminal lane is untouched.
