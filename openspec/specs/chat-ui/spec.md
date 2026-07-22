# chat-ui Specification

## Purpose
The frontend chat rendering surface — a structured, mobile-friendly alternative to the xterm.js terminal for the same underlying claude conversation — plus the spawn/resume UX that lets a user pick which surface a session uses. Chat and terminal are both first-class; this capability never disables, hides, or demotes the terminal lane.

## Requirements

### Requirement: Spawn mode choice — Terminal or Chat
The NEW SESSION modal SHALL present a choice between `Terminal` and `Chat` when starting a new session. There SHALL be no server-side or global default; the client SHALL remember the last-used choice locally (e.g. `localStorage`) and pre-select it on the next modal open, without round-tripping to the server. The selected kind SHALL be sent as `kind` on the spawn request.

#### Scenario: First-ever spawn has no remembered choice
- **WHEN** the user opens the NEW SESSION modal for the first time (no stored preference)
- **THEN** the modal SHALL show the mode choice with no pre-selected default forced by the server

#### Scenario: Last-used choice is remembered
- **WHEN** the user spawns a session in Chat mode, then later reopens the NEW SESSION modal
- **THEN** the modal SHALL pre-select Chat

#### Scenario: Chosen kind is sent on spawn
- **WHEN** the user selects Chat and launches
- **THEN** the spawn request SHALL carry `kind=chat`

### Requirement: Resume offers both surfaces
Resuming a previous session SHALL be possible in either surface, independent of which kind the conversation last ran as. This SHALL be exposed through the same modal-wide Terminal/Chat toggle used for starting a new session (§ "Spawn mode choice"), rather than a duplicate per-row control — selecting a previous session and submitting SHALL send the `resume` uuid together with whichever `kind` the toggle currently holds.

#### Scenario: Resume a conversation in the other lane
- **WHEN** a conversation last ran as a terminal session and the user sets the toggle to Chat, then selects that conversation from the previous-sessions list and confirms
- **THEN** the resume request SHALL carry `resume=<uuid>` and `kind=chat`

### Requirement: Chat message list renders markdown with streaming partial text
The chat surface SHALL render assistant and user turns as a scrollable message list with markdown formatting (code blocks, lists, emphasis, links). Partial/streaming assistant text SHALL be rendered incrementally as stream events arrive, not only once a turn completes. Thinking blocks SHALL be collapsed by default with an affordance to expand them.

#### Scenario: Streaming text appears incrementally
- **WHEN** the engine streams an assistant turn as a sequence of partial-message events
- **THEN** the message list SHALL update progressively as each partial arrives, not wait for the turn to finish

#### Scenario: Thinking is collapsed by default
- **WHEN** a turn includes a thinking block
- **THEN** the chat UI SHALL render it collapsed, with an explicit affordance to expand it

### Requirement: Tool calls render as collapsible step rows
Each `tool_use`/`tool_result` pair SHALL render as one collapsible row in the message list. `Edit` and `Write` tool calls SHALL render their content as a diff. `Bash` tool calls SHALL render the command plus an excerpt of its output. Other tool calls SHALL render at minimum the tool name and a collapsed detail view.

#### Scenario: Edit tool call renders a diff
- **WHEN** the engine emits an `Edit` tool_use/tool_result pair
- **THEN** the chat UI SHALL render one collapsible row showing the edit as a diff

#### Scenario: Bash tool call renders command and output excerpt
- **WHEN** the engine emits a `Bash` tool_use/tool_result pair
- **THEN** the chat UI SHALL render one collapsible row showing the command and an excerpt of its output

### Requirement: Input bar supports send, queue-while-running, and a required /clear command
The chat input bar SHALL let the user send a message at any time, including while a previous turn is still running — such input SHALL be queued and delivered to the engine in submission order (per `chat-session-host`'s ordering guarantee), not rejected or dropped. Typing `/clear` and submitting SHALL be a required, fully supported action that passes through to the engine's native `/clear` dispatch (dropping conversation context while keeping the same cwd); there SHALL be no dedicated `/clear` button — command-only.

#### Scenario: Sending while a turn is in progress
- **WHEN** the user submits a message while the assistant is still responding to a previous one
- **THEN** the client SHALL queue the input and it SHALL be delivered to the engine, processed after the current turn

#### Scenario: /clear drops context, keeps cwd
- **WHEN** the user types `/clear` and submits
- **THEN** the client SHALL send it to the engine as a normal input line, and the resulting `conversation_reset` SHALL be rendered as a system notice in the message list

### Requirement: Image attach via upload path, never inline base64
The input bar SHALL support attaching an image by uploading it through the existing image-upload endpoint and referencing the returned file path in the outbound message, so the engine reads the file from disk. The client SHALL NOT construct or send an inline base64 image content block under any circumstance.

#### Scenario: Attaching an image sends a file-path reference
- **WHEN** the user attaches an image and sends the message
- **THEN** the client SHALL first upload the image via the existing upload endpoint and SHALL compose the outbound message referencing the returned path, not embedding image bytes

### Requirement: Header shows cwd, model, live cost/token ticker, and mode/kill actions
The chat surface SHALL show a header with the session's cwd and model, a live-updating cost/token indicator driven by usage events on the stream, and action buttons for mode switch and kill.

#### Scenario: Usage events update the ticker
- **WHEN** a usage event arrives on the stream
- **THEN** the header's cost/token indicator SHALL update to reflect it

### Requirement: History renders from the transcript on open or reconnect
On opening or reconnecting to a chat session, the client SHALL fetch and render the conversation's transcript (via the transcript endpoint) before or alongside subscribing to the live event tail, so a locked/reopened phone or a dropped connection loses no visible history and requires no terminal-style snapshot replay. Long transcripts SHALL render virtualized/tail-first, lazy-loading older turns on scroll.

#### Scenario: Reopening after the phone was locked
- **WHEN** the user reopens the dashboard after the connection dropped while the phone was locked
- **THEN** the chat surface SHALL render the transcript history and then resume following live events, with nothing visibly lost

#### Scenario: Long transcript renders the tail first
- **WHEN** a conversation's transcript is very long
- **THEN** the client SHALL render the most recent turns first and lazy-load older ones as the user scrolls up

### Requirement: Agent questions render as plain text, answered in the input bar
Because headless stream-json mode has no `AskUserQuestion` menu events, question-shaped assistant turns SHALL render as ordinary plain-text messages; the user SHALL answer by typing in the input bar like any other turn. The chat UI SHALL NOT attempt to render a TUI-style selection menu for questions in this change.

#### Scenario: Agent asks a question
- **WHEN** the assistant's turn is a question to the user
- **THEN** it SHALL render as a normal markdown message, and the user SHALL reply via the input bar

### Requirement: Mode switch works in both directions from the chat surface
The chat surface SHALL offer a mode-switch action that kills the current chat child and respawns the same conversation uuid as a terminal session (opening the terminal surface for it), and the terminal surface SHALL symmetrically offer switching the same conversation to chat. The session index entry SHALL be unaffected by a mode switch.

#### Scenario: Switch from chat to terminal
- **WHEN** the user triggers mode switch from an open chat session
- **THEN** the client SHALL kill the chat session, spawn a terminal session with `resume=<uuid>`, and open the terminal surface for it

#### Scenario: Switch from terminal to chat
- **WHEN** the user triggers mode switch from an open terminal session
- **THEN** the client SHALL kill the terminal session, spawn a chat session with `resume=<uuid>`, and open the chat surface for it

### Requirement: Futurism design system throughout
The chat surface SHALL follow the existing Futurism design system (square corners, 2px ink borders, solid offset shadows, single `--accent`, Helvetica Neue UI font) consistent with the rest of the dashboard. New styles SHALL be added to `app.css` only — `futurism.css` (the vendored kit) SHALL NOT be hand-edited.

#### Scenario: Chat surface matches the dashboard's visual language
- **WHEN** the chat surface is rendered in either light or dark theme
- **THEN** it SHALL use the same tokens, borders, and shadow style as the rest of the dashboard, with no new CSS framework or CDN dependency introduced
