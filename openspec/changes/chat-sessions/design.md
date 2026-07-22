## Context

`docs/chat-sessions-design.md` is the authoritative, probe-verified design (dated 2026-07-21, engine 2.1.215): streaming deltas, thinking events, `Stop` hook firing, `/clear`+`/model` dispatch, resume, and image-via-file-path have all been verified empirically against the pinned binary. This document translates that design into this repository's concrete architecture (`sessiond` actor/registry, `backend` bridge/index, `shared` wire contract, `frontend` native-ES-module JS) and records the implementation decisions the design doc leaves to the change itself.

The existing terminal path is the working reference for "how sessiond hosts a session": `session` in `sessiond/session.go` is an actor goroutine owning a PTY, an emulator, and a `viewers map[net.Conn]*viewer`, driven by a `cmds chan sessCmd`. `registry.spawn` in `sessiond/registry.go` starts the child, opens its socket, and registers it. The backend's `handleWebSocket`/`bridgeWSToFrames`/`bridgeFramesToWS` in `backend/handlers.go` hold no session state — they translate WS messages to `protocol` frames and back. `shared/` (`claude-sandbox-api`) is the single wire-contract module both backend and frontend import via a local replace directive. None of this changes for terminal sessions.

## Goals / Non-Goals

**Goals:**
- Ship design doc phases P1–P3: pinned engine version, sessiond chat kind, SPAWN `kind`, chat WS bridge variant + index re-key tap, transcript endpoint, and a real chat UI (markdown, streaming, tool steps, image attach, mode switch both directions).
- Reuse the actor/registry/protocol scaffolding sessiond already has for the terminal kind — a chat session is a second, deliberately simpler actor variant, not a parallel subsystem.
- Keep sessiond dumb (bytes/lines in, bytes/lines out) and the backend's interpretation limited to the one thing it needs (`conversation_reset`/`init` re-keying) — full event parsing is a frontend concern, per the design doc's protocol responsibility split.
- Zero behavior change to the terminal lane.

**Non-Goals (parked, P4):**
- Supervisor JSON-answer integration, semantic activity cards, fleet view.
- Deciding whether chat replaces the terminal as default/only surface (design doc §11) — both lanes ship first-class, no deprecation language anywhere.
- Dictation polish (progressive enhancement, not core to this change).
- Nested subagent (Task tool) rendering — flat rows per design doc decision #2.

## Decisions

### 1. Session kind lives on the live child, not the conversation
The session index (`backend/sessionindex.go`, `dashboard-sessions.json`) keeps its existing shape (`uuid → {cwd, created, name}`) with no `kind` field added to persisted entries. Kind is decided at spawn/resume time (a request parameter) and tracked only in the in-memory `sessionRecord` (backend) and the sessiond `session`/new chat-session struct (sessiond). This matches the design doc's "mode switch = kill + resume" model exactly: switching lanes never touches the index, only which binary/flags the next spawn uses.

**Alternative considered:** persist last-used kind per conversation in the index, so a plain resume (no explicit kind) reopens in the same lane. Rejected for this change — the design doc's decision #1 leans "remember last used [modal choice], no global default," which is a frontend-local (localStorage) UX preference, not a durable per-conversation server fact. Revisit only if usage data shows users expect resume-without-choosing to preserve lane.

### 2. sessiond: a second actor type, not a mode flag on `session`
Rather than adding `kind`-conditional branches throughout the existing `session` struct (which owns PTY-specific fields — `ptmx`, `term *termState`, `active net.Conn` resize arbitration), the chat kind gets its own actor type (`chatSession`) implementing the same external surface the registry needs (`Kill()`, `Exited()`, a way to `serve(ln)` a listener, `send`-style command handling for attach/detach/input/output). This keeps the terminal actor's invariants (documented at length in `sessiond/session.go`'s comments — pollable PTY fds, write deadlines, active-viewer suspension) untouched and un-risked, and keeps the chat actor small: no emulator, no snapshot, no per-viewer size, no active/suspended distinction — every registered viewer is always live and gets every line.

**Alternative considered:** one `session` struct with a `kind` field and PTY fields becoming `*optional`. Rejected — it would force every PTY-specific method (`applyResize`, `activateViewer`, snapshot rendering) to add kind guards, directly triggering this environment's repeat-touch-refactor rule the moment a chat bug required "just one more" branch in actor code that terminal sessions also depend on. A parallel, minimal type is the smaller and safer surface.

**Shared scaffolding:** the registry (`sessions map[string]*session`-shaped registry) is generalized to hold either kind behind a small interface (`liveSession`: `Kill()`, `Exited() <-chan struct{}`, `serve(net.Listener)`), so `registry.spawn` dispatches on `req.Kind` to `newSession(...)` or `newChatSession(...)` but list/kill/shutdown logic is written once against the interface.

### 3. Chat actor process model: `exec.Cmd` with `StdinPipe`/`StdoutPipe`, not a PTY
The design doc is explicit: `-p --input-format stream-json --output-format stream-json --include-partial-messages` is a plain-pipe headless mode, not a TUI — no `pty.Start` involved. The chat actor holds `cmd.StdinPipe()` (write queued input messages) and reads `cmd.StdoutPipe()` line-by-line (each line is one JSON event, broadcast verbatim to viewers as a `FrameData`-equivalent — see decision 5 on protocol reuse). `cmd.Wait()` in a goroutine feeds the same `cmdEnd`-style teardown path the terminal actor uses, so kill/exit semantics (`CloseEnded`/`CloseKilled`, LIST removal, socket cleanup) are identical from the registry's point of view.

### 4. Input writes are queued and serialized, matching the verified queue-while-running behavior
The design doc verifies the engine accepts multiple queued stdin messages and processes them in order (open decision #3, resolved). The chat actor's command loop only ever has one writer to stdin (itself, from queued `cmdInput`-equivalent messages), so "queueing" is naturally just "the actor hasn't gotten around to writing the next line yet" — no separate queue data structure is needed beyond the actor's existing `cmds` channel, which already serializes everything through one goroutine.

### 5. Protocol reuse: existing frame types, not a new frame vocabulary
sessiond's frame types (`FrameData`, `FrameControl`, `FrameClose`, `FrameAttach`, `FrameRequest`/`FrameResponse`) are reused unchanged for chat sessions:
- `FrameData` carries one JSON event line (UTF-8 bytes) each direction — engine→viewer for stdout lines, viewer→engine for one JSON input message. This mirrors the terminal kind's "DATA is raw bytes, no interpretation" contract exactly; only the payload's meaning differs (an opaque JSON line vs. opaque terminal bytes), and sessiond does not need to know the difference to relay it.
- `FrameAttach` still opens a viewer stream, but for chat sessions `cols`/`rows` are not required — the existing `Attach{Cols,Rows}` struct is reused with both left at their zero value (sessiond skips the size validation branch it takes for terminal ATTACH); no snapshot is sent in reply (no `FrameSnapshot` for chat — see decision 6).
- `FrameClose`/kill/list are unchanged.
- `Request` (control socket) gains `Kind string` (`"terminal"`/`"chat"`, empty defaults to `"terminal"` for backward compatibility with any in-flight terminal-only client expectations).

**Alternative considered:** a distinct frame type for chat events (e.g. `FrameEvent`). Rejected — it would require the backend bridge to special-case frame types per session kind for no benefit; reusing `FrameData` keeps the bridge's job ("`FrameData` → WS message of the kind kind implies") a one-line branch on the *session's* kind (known from the spawn/list response), not on frame type.

### 6. No snapshot machinery for chat; history comes from the transcript
Terminal sessions rely on the emulator's `Snapshot()` because a PTY's live state (cursor position, alt-screen, scrollback) cannot be reconstructed from bytes alone without replaying them through a terminal emulator. A chat session's "state" is just the ordered list of JSON events already durably persisted by claude itself as the conversation's `.jsonl` transcript (same file `hasTranscript`/`deleteTranscript` in `backend/paths.go` already glob for). So: on ATTACH, sessiond chat sessions send nothing but live events from that point forward — no snapshot frame at all. The new `GET /api/sessions/{terminalId}/transcript` endpoint (session-api delta) serves the parsed-once transcript JSONL for the frontend to render before it starts consuming the live tail, exactly per design doc §7. This is a deliberate protocol asymmetry between the two kinds that the frontend already has the seam for (it decides, from the session's `kind`, whether to expect a `FrameSnapshot` before live data).

### 7. Session-index re-key tap lives in the backend bridge, not sessiond
Per design doc §14 decision #4, `/clear` inside a chat session emits a `conversation_reset` event, then the stream re-inits under a new uuid, and the transcript persists under the new id. sessiond must not parse this (decision 2's "dumb relay" invariant) — it is caught in the backend's chat WS bridge (`chat-relay` capability), which already sees every line as it forwards `FrameData` → WS text.

**Verified against the pinned engine (2.1.215, probed 2026-07-22) — the re-key is a two-step tap, not a single-event read:**
- `conversation_reset`'s own `session_id` field is the OLD uuid (confirmed identical to the preceding turns' `session_id`).
- `conversation_reset` also carries a `new_conversation_id` field, but it does **not** reliably identify the uuid the conversation actually continues under — empirically, the transcript file that gets written afterward uses a different uuid than `new_conversation_id`. This field is a wire artifact, not the re-key target; it is kept only for diagnostic logging.
- The actual new uuid is the `session_id` of the very next `system` event whose `subtype` is `"init"` — this event reliably arrives right after the reset (after any hook-lifecycle system events, which carry no session id change) and its `session_id` matches the transcript file that gets written for the continued conversation.

So the bridge's tap is stateful across two events: on `conversation_reset`, record the old uuid and enter an "awaiting re-key" state for that connection; on the next `system`/`init` event, read its `session_id` as the new uuid and call `SessionIndex.rekey(old, new)` (rename the entry in place — same cwd/created/name, new key — never delete+recreate, which would lose the custom name and reset `created`). The live `sessionRecord` in the backend's `sessionStore` is updated the same way so `ListSessions`/kill/rename keyed by the *old* uuid don't silently 404 after a `/clear`.

**Alternative considered:** have sessiond watch for `conversation_reset` since it already sees every line. Rejected — this is exactly the line sessiond must not cross per the design doc's protocol responsibility split; the backend is the layer that owns session-index semantics, and putting reset-detection there keeps sessiond's "no JSON interpretation" invariant total, not "mostly".

### 8. Frontend: a second rendering module, no shared code with `terminal.js` beyond `SessionSocket`
`SessionSocket` (`session-socket.js`) is transport-agnostic already — it exposes `onData`/`onControl`/`onStatus` callbacks over one WS connection and does not assume DATA payloads are terminal bytes. The chat UI reuses `SessionSocket` unmodified: `onData` receives one JSON event line at a time (parsed from the WS binary/text payload — chat sessions send events as WS text frames per decision 5b below, so `onData`'s `string` branch is exercised instead of the `ArrayBuffer` ones terminal sessions use). A new `chat.js` (plus small companions mirroring the `terminal-*.js` split: `chat-render.js` for markdown/tool-step rendering, `chat-input.js` for the input bar) owns everything downstream: parsing the event vocabulary, building the message list DOM, and composing outbound JSON messages. `tabs.js`'s single-terminal-view pattern (one shown surface at a time, sidebar as switcher) is generalized minimally: `openSession` gains a kind branch that creates either a `TerminalManager` instance or a chat view instance in the same `#singleTerminal`-equivalent slot, keyed off the session's `kind` from the store.

**5b. Chat frames travel as WS text, not binary.** Terminal DATA is binary (`websocket.BinaryMessage`) because it's an opaque byte stream that may not be valid UTF-8 mid-escape-sequence. A chat session's `FrameData` payload is always one complete JSON line (UTF-8 by construction), so the backend bridge's chat variant writes it as `websocket.TextMessage`, and `SessionSocket.onmessage`'s existing text branch (already used for CONTROL messages) receives it — the frontend distinguishes "this is a chat event" from "this is a resize/deactivated control message" by shape (`JSON.parse` then check for a recognized envelope field), not by frame type, mirroring how sessiond distinguishes them by socket/session kind rather than frame type.

### 9. Spawn/resume UX: mode is a request parameter, remembered client-side only
The NEW SESSION modal's Terminal|Chat toggle sets `SpawnRequest.Kind` for whichever action the primary button submits — starting new or resuming a previous session — rather than a second, per-history-row control; the toggle is the single place kind is chosen, applying uniformly to both. The frontend remembers the last-used choice in `localStorage` (mirroring the existing theme/accent pattern of "instant local paint, no server round-trip for a personal UI preference") — this is explicitly NOT synced via `/api/ui-prefs` because it is a per-action choice, not a persistent app setting, matching design doc open-question #1's lean ("remember last used, no global default").

## Risks / Trade-offs

- **[Risk]** Engine version drift changes the stream-json event vocabulary (e.g. `conversation_reset` shape, `Stop` hook behavior) → **Mitigation**: this change pins the version in `Dockerfile.sessions` as its first task; a future deliberate bump requires re-running the P1 spike's probes before merging, per the design doc's own risk table.
- **[Risk]** The backend's `conversation_reset` re-key tap silently mis-parses an unexpected event shape and leaves the index pointing at a dead uuid → **Mitigation**: re-key is best-effort and logged; a failure to re-key does not crash the bridge or the session — worst case the old index entry stops resolving to the live session (same failure mode as any other reconciliation gap the existing 5s poll already tolerates) until the next full LIST/history refresh.
- **[Risk]** Two independent actor implementations in sessiond (terminal, chat) double the surface `go test -race` must keep clean → **Mitigation**: the chat actor is intentionally small (no emulator, no resize arbitration — the two most complex, race-prone parts of the terminal actor), and the registry-level interface keeps tests for spawn/list/kill/shutdown shared rather than duplicated per kind.
- **[Risk]** Long transcripts make the new transcript endpoint slow or memory-heavy on open → **Mitigation**: per design doc §7, the frontend virtualizes and tail-renders; the endpoint itself can stream the file rather than buffering it fully (implementation detail, not a wire-contract change).
- **[Risk]** Image attach must never send inline base64 (verified silently dropped by the engine on 2.1.215) → **Mitigation**: the chat input bar's only image path is upload-then-reference-by-file-path, reusing the existing `POST /api/sessions/{terminalId}/upload` endpoint unchanged; this is enforced by not building an inline-base64 code path at all, not by a runtime check.

## Migration Plan

No data migration — the session index schema is unchanged (decision 1). Rollout is additive and backward compatible: `SpawnRequest.Kind` defaults to `"terminal"` when absent, so any client or persisted state predating this change continues to behave exactly as before. Deploy order (matches the task waves): `shared/` wire contract → `sessiond` (chat actor + kind-aware registry, plus the version pin) → `backend` (chat bridge + transcript endpoint + re-key tap) → `frontend` (chat UI + spawn/resume mode UX). Each layer is independently testable against the layer below with the old behavior unchanged when `kind` is absent/`"terminal"`. Rollback is a revert; no running-session state is impacted since chat sessions are new, additive processes.

## Open Questions

Carried forward from `docs/chat-sessions-design.md` §14 (not blocking, tracked for implementation-time judgment calls, resolved with the doc's stated lean unless evidence says otherwise during build):
- Spawn-modal default selection: remember last used client-side, no global/server default (decision 9).
- Subagent (Task tool) event rendering: flat rows in v1, no nesting.
- `conversation_reset`/`init` JSON field names for the re-key tap are now confirmed (decision 7, verified 2026-07-22 against the pinned 2.1.215 engine) and encoded as `ChatConversationResetEvent`/`ChatSystemEvent` in `shared/types.go` — no longer open.
