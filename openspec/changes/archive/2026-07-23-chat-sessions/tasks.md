## 1. Groundwork

- [x] 1.1 Pin the claude CLI version in `Dockerfile.sessions` (replace the `install.sh | bash` "latest" install with an explicit pinned version — the one the stream-json vocabulary below is verified against)
- [x] 1.2 `shared/enums.go`: add the session-kind allowlist (`terminal`, `chat`) and a `SessionKindValues()` helper, mirroring the existing `ModelValues()`/`EffortValues()` pattern
- [x] 1.3 `shared/types.go`: add `Kind` to `SpawnRequest` (omitempty, defaults to terminal when absent) and `DisplaySession` (always present); add a small typed struct for the `conversation_reset`/`init` event shape the backend re-key tap needs (only the fields it reads — not a full event vocabulary)
- [x] 1.4 `shared/routes.go`: add `RouteSessionTranscript` (`/api/sessions/{terminalId}/transcript`) and its path builder, following the existing `SessionPath`/`SessionNamePath` pattern
- [x] 1.5 `sessiond/protocol/protocol.go`: add `Kind` to `Request` (control-socket SPAWN) and to `SessionInfo` (LIST response); document that an empty `Kind` means terminal

## 2. sessiond: chat session kind

- [x] 2.1 Define a minimal `liveSession` interface (`Kill()`, `Exited() <-chan struct{}`, `serve(net.Listener)`) satisfied by the existing `session` type; no behavior change to `session`/`sessiond/session.go`
- [x] 2.2 New chat actor (`sessiond/chatsession.go`): actor struct + command loop mirroring `session`'s shape (attach/detach/input/output/kill commands via a `cmds` channel) but holding a pipe-child (`exec.Cmd` with `StdinPipe`/`StdoutPipe`) instead of a PTY — no `termState`, no active/suspended viewer fields
- [x] 2.3 Chat actor: spawn `claude -p --session-id <uuid> --input-format stream-json --output-format stream-json --include-partial-messages --dangerously-skip-permissions` (or `--resume <uuid>`) in `cwd`; stdout read loop splits on newlines and broadcasts each complete line to every registered viewer (no suspension, no active-viewer concept)
- [x] 2.4 Chat actor: viewer input writes go to stdin in FIFO order (already implied by single-actor-goroutine serialization — no extra queue needed); a stdin write failure surfaces to that viewer via CONTROL error, mirroring the terminal actor's input-write-failure handling
- [x] 2.5 Chat actor: ATTACH handling ignores/accepts zero `cols`/`rows`; no SNAPSHOT frame sent in reply to a chat ATTACH — the viewer starts receiving live DATA immediately
- [x] 2.6 Chat actor: kill (SIGTERM → grace → SIGKILL on the process group, reusing `killGracePeriod`), teardown (CLOSE to viewers, socket unlink via the existing `onExit` hook shape), and exit-on-EOF (stdout closed / `cmd.Wait` returns) mirroring the terminal actor's teardown path
- [x] 2.7 `sessiond/registry.go`: generalize `sessions map[string]*session` to hold the `liveSession` interface; `spawn` dispatches to `newSession`/`begin` (terminal, `kind==""` or `"terminal"`) or the new chat constructor (`kind=="chat"`) based on `req.Kind`; `list`/`kill`/`shutdown` operate against the interface unchanged
- [x] 2.8 Unit tests for the chat actor: stdout-line broadcast to multiple viewers, stdin write ordering, kill/teardown, ATTACH with zero dims, no-snapshot-on-attach — `go test -race ./...` clean in `sessiond/`

## 3. backend: chat relay, index re-key, transcript endpoint

- [x] 3.1 `backend/lifecycle.go`/`session.go`: `Spawn`/`Resume` accept and validate `kind` (reject anything other than `terminal`/`chat` with 400), pass it through to sessiond's SPAWN op, and record it on the in-memory `sessionRecord` (not the persisted index — see design.md decision 1)
- [x] 3.2 `backend/sessionstate.go`: add `Kind` to `sessionRecord` and thread it into `display()` → `api.DisplaySession.Kind`
- [x] 3.3 `backend/handlers.go`: chat WS bridge variant — for a session whose kind is `chat`, forward `FrameData` lines as WS TextMessage (not BinaryMessage) and forward inbound WS text (user input) as `FrameData` to sessiond; skip the ATTACH-carries-dimensions requirement and the snapshot-wait for this kind; keep the terminal bridge path (`bridgeWSToFrames`/`bridgeFramesToWS`) untouched, branching by session kind before choosing which bridge function runs
- [x] 3.4 `backend/sessionindex.go`: add a `rekey(oldUUID, newUUID string) error` method that moves an entry to a new key in place (same cwd/created/name), atomic and mutex-guarded like the existing mutators
- [x] 3.5 Chat bridge: two-step re-key tap using `ChatConversationResetEvent`/`ChatSystemEvent` (1.3) — on `conversation_reset`, record its `session_id` (old uuid, NOT `new_conversation_id` — verified unreliable against the pinned engine, see design.md decision 7); on the next `system`/`init` event on the same connection, take its `session_id` as the new uuid and call `SessionIndex.rekey(old, new)`, updating the live `sessionStore` record's `SessionID` in place; log and continue (never fail the bridge) if no `init` follows or either shape is malformed
- [x] 3.6 New `GET /api/sessions/{terminalId}/transcript` handler: resolve the session's conversation uuid from the live store (404 if not live), locate its transcript via the existing `transcriptPaths` glob, and stream/serve its content (200, empty body if no transcript file exists yet)
- [x] 3.7 Register the transcript route in `backend/handlers.go`'s `NewServer` and mirror it in the frontend's passthrough proxy (it needs no transform, so it can ride the existing `/api/` catch-all — verify and note in a comment if so, or add an explicit mirror if the catch-all doesn't cover it)
- [x] 3.8 Backend tests: spawn/resume with `kind`, invalid-kind 400, chat bridge frame-direction test against a fake sessiond socket, `conversation_reset` re-key (index + live record), transcript endpoint (live/404/empty)
- [x] 3.9 (Discovered during implementation, not itemized above but required by the chat-ui mode-switch requirement: `SpawnRequest.SessionID`/conversation uuid is deliberately excluded from the wire per `json:"-"`, so a client-orchestrated kill-then-resume-by-uuid is impossible without exposing it. Added `POST /api/sessions/{terminalId}/mode` (`shared/routes.go` `RouteSessionMode`, `SessionManager.SwitchMode` in `lifecycle.go`, `handleModeSwitch` in `handlers.go`) — a single backend-orchestrated kill+resume-as-kind that resolves the uuid server-side, waiting for the old child to leave sessiond's list first (same ordering concern as `DeleteHistory`). Tested in `chat_handlers_test.go`.

## 4. frontend: spawn/resume mode UX

- [x] 4.1 `shared`-injected route/enum plumbing: extend `routesJSON`/`window.ROUTES` (frontend/handlers.go templateFuncs) with the transcript path builder; extend whatever enum injection covers session kind so `picker.js`/new chat modules read the allowlist from the shared contract, not a re-typed literal
- [x] 4.2 NEW SESSION modal (`frontend/web/templates/fragments/directory-picker.html` + `picker.js`): add the Terminal/Chat mode choice control; remember the last-used choice in `localStorage`; include `kind` in the spawn form submission
- [x] 4.3 Resume offers both surfaces via the same modal-wide Terminal/Chat toggle from 4.2 (not a separate control per history row — the toggle already applies to whichever action the primary button submits, new or resume, so a second per-row control would duplicate the same choice); `dirPickerSetSel`'s resume path already carries the current `kind` through the shared hidden field
- [x] 4.4 `store.js`/session data: ensure the session-kind field flows from `api.DisplaySession.Kind` through the embedded `#session-data` JSON into the client store, so views can branch on it

## 5. frontend: chat rendering surface

- [x] 5.1 New `chat.js` (manager, mirroring `terminal.js`'s `TerminalManager` shape: `create`/`destroy`/`get` keyed by terminal id) — no side effects at import, `init()` exported
- [x] 5.2 New `chat-render.js`: markdown rendering (vendor a small, dependency-free markdown renderer under `frontend/web/static/vendor/` — no CDN), streaming partial-text updates, collapsed-by-default thinking blocks, tool-step collapsible rows (diff rendering for Edit/Write, command+output excerpt for Bash), `conversation_reset` rendered as a system notice
- [x] 5.3 New `chat-input.js`: input bar wiring — send, queue-while-running (client just calls `socket.send` in submission order; the ordering guarantee is server-side per task 2.4), `/clear` as a plain input line (no dedicated button), image attach via the existing upload endpoint + file-path reference in the outbound JSON message (no inline base64 path exists in the code at all)
- [x] 5.4 Chat header: cwd/model display, live cost/token ticker updated from usage events, mode-switch and kill action buttons
- [x] 5.5 History-on-open: fetch `GET /api/sessions/{terminalId}/transcript` and render it before/alongside subscribing to the live tail; tail-first virtualized rendering with lazy-load-older-on-scroll for long transcripts
- [x] 5.6 `tabs.js`'s `openSession`: branch on the session's kind to create either a `TerminalManager` instance or a `chat.js` view instance in the single-surface slot; `updateSingleWelcome`/`cleanupKilledSession` generalized to be kind-agnostic
- [x] 5.7 Mode switch action (both directions): kill current session, spawn the other kind with `resume=<uuid>`, open the resulting surface — wire as a `data-action` handler per the actions.js delegation convention
- [x] 5.8 `app.css`: chat surface styling (message list, bubbles, tool-step rows, input bar, header ticker) — Futurism-conformant (square corners, 2px borders, `--accent`), added to `app.css` only; update the override ledger if anything diverges from the kit
- [x] 5.9 JS tests (`node --test`) for `chat.js`/`chat-render.js`/`chat-input.js`: streaming partial-text accumulation, tool-step row construction, queue-while-running ordering at the client boundary, image-attach path composition, mode-switch action wiring

## 6. Verify

- [x] 6.1 `GOWORK=off go build ./...` and `go test -race ./...` clean across `sessiond/`, `backend/`, `frontend/`, `shared/`
- [x] 6.2 `node --test frontend/web/static/js/*.test.mjs frontend/web/static/js/__tests__/*.test.mjs` clean, including the new chat tests
- [x] 6.3 Re-read `docs/chat-sessions-design.md` against the implementation: confirm no terminal-lane file changed behavior, confirm sessiond's chat actor never branches on event JSON content, confirm the backend bridge's only content inspection is the `conversation_reset`/`init` tap
- [x] 6.4 `openspec validate --strict` clean; every task above checked off and matched by a spec requirement
- [x] 6.5 Update `CLAUDE.md`'s architecture section to document the chat session kind end-to-end (sessiond chat actor, kind-aware SPAWN, WS bridge variant, transcript endpoint, session-index re-key tap, new frontend chat modules) alongside the existing terminal-kind description
