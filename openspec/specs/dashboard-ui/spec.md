# dashboard-ui Specification

## Purpose
TBD - created by archiving change web-dashboard. Update Purpose after archive.
## Requirements
### Requirement: Sidebar session list with SSE real-time updates
The dashboard SHALL display a persistent left sidebar listing all Claude Code sessions (discovered via the backend). The sidebar SHALL remain visible in all view modes. The list SHALL auto-update via Server-Sent Events — when session state changes, the server SHALL push an SSE event that triggers HTMX to re-fetch and swap the session list HTML fragment. All sessions SHALL have uniform styling — there is no "external" or "managed" distinction.

#### Scenario: Dashboard loads with active sessions
- **WHEN** the user opens the dashboard and sessions are running
- **THEN** the sidebar SHALL render all sessions with directory, session name, duration, and emerald "active" status

#### Scenario: Dashboard loads with no sessions
- **WHEN** the user opens the dashboard and no sessions exist
- **THEN** the sidebar SHALL display an empty state prompting to create a new session

#### Scenario: Session status updates via SSE
- **WHEN** a session is spawned, killed, or exits while the dashboard is open
- **THEN** the server SHALL push an SSE `update` event, HTMX SHALL trigger a fetch of the session list fragment, and swap the updated HTML into the sidebar without a full page reload

#### Scenario: Click any session to open terminal
- **WHEN** the user clicks on any session card in the sidebar
- **THEN** an xterm.js terminal SHALL initialize and connect to that session's WebSocket endpoint, displaying the current terminal state via the relay's scrollback replay. There SHALL be no dimmed or non-interactive session entries.

### Requirement: Session card display
Each session card in the sidebar SHALL display: the session name, a live-ticking duration (updated every second client-side from the `CreatedAt` timestamp), a `DisplayName` as the primary label (custom name or directory basename), the full CWD path as secondary text, and an "active"/"stopped" status label. The pulsing status dot indicator SHALL be removed — the text label is sufficient.

#### Scenario: Session card layout
- **WHEN** a session card is rendered
- **THEN** it SHALL show the session name, duration (ticking every second), display name as primary label, CWD as secondary text, and status label

#### Scenario: Duration ticks client-side
- **WHEN** a session card is visible in the browser
- **THEN** a client-side `setInterval` SHALL update the duration text every second using the `data-created` Unix timestamp attribute, matching the server's `humanDuration` format (Xs, Xm Xs, Xh Xm)

### Requirement: Editable session name in sidebar and headers
Each session card SHALL display a `DisplayName`. By default, this SHALL be the directory basename. Users SHALL be able to set a custom name via a rename button (pencil icon) on each card that opens a modal dialog. The modal SHALL be a bottom sheet on mobile (`modal-bottom`) and centered on desktop (`sm:modal-middle`). The full CWD path SHALL remain visible on the card as secondary text. Tab headers SHALL use `DisplayName` via the `data-session` attribute.

#### Scenario: Session with custom name
- **WHEN** a session has a custom name set (e.g., "backend refactor")
- **THEN** the session card SHALL display the custom name as primary label, tab headers SHALL use the custom name, and the CWD path SHALL still appear as secondary text

#### Scenario: Session without custom name
- **WHEN** a session has no custom name
- **THEN** the session card and tab headers SHALL display the directory basename

#### Scenario: Rename via modal
- **WHEN** the user clicks the rename button (pencil icon) on a session card
- **THEN** a modal SHALL open with the current display name pre-filled and selected, the input SHALL be focused, and pressing Enter or clicking Save SHALL send a PUT request to `/api/sessions/{terminalId}/name`

#### Scenario: Rename modal on mobile
- **WHEN** the rename modal opens on a mobile viewport (< 640px)
- **THEN** it SHALL render as a bottom sheet sliding up from the bottom of the screen

#### Scenario: Rename updates all views
- **WHEN** a session's name is updated via the API
- **THEN** the SSE update SHALL trigger a session list refresh, and the new name SHALL appear on the sidebar card and tab headers

### Requirement: Three view modes — Single, Split, Grid
The dashboard SHALL support three view modes for the main content area, toggled via icon buttons in the header. The current view mode SHALL persist in localStorage.

#### Scenario: Single terminal view with tabs
- **WHEN** the single view mode is active and sessions are open
- **THEN** the main area SHALL display the active terminal at full width with a tab bar showing all open session tabs. Clicking a session in the sidebar that is not yet open SHALL open it in a new tab (not replace the current one). Clicking a session that already has a tab SHALL switch to that tab. Closing a tab SHALL switch to the last remaining tab, or show the welcome screen if no tabs remain. The sidebar SHALL highlight the active tab's session with a green left border; closing a tab SHALL immediately remove the highlight.

#### Scenario: No terminal open (welcome screen — desktop)
- **WHEN** the single view mode is active, no session tab is open, and the viewport is >= 768px
- **THEN** the main area SHALL display a centered welcome screen with the project logo, title, and desktop-specific instructions: New (create session), Click (open from sidebar), Shift+Click (split right pane), view mode switching. The tab bar and status bar SHALL be hidden.

#### Scenario: No terminal open (welcome screen — mobile)
- **WHEN** the single view mode is active, no session tab is open, and the viewport is < 768px
- **THEN** the welcome screen SHALL show mobile-specific instructions: open the menu (hamburger), New (create from menu), Tap (open a session), pull down to refresh. Desktop-only instructions (Shift+Click, view modes) SHALL be hidden.

#### Scenario: Split terminal view
- **WHEN** the split view mode is active
- **THEN** the main area SHALL display two terminals side by side, each with its own header (session name, maximize/close buttons) and compact status bar. A draggable divider SHALL separate the panes. Clicking a sidebar session SHALL open it in the left pane; Shift+click or right-click SHALL open it in the right pane. A hint banner SHALL appear in the sidebar explaining the controls. Empty panes SHALL show placeholder text ("Click a session to open here" / "Shift+click a session to open here").

#### Scenario: Maximize split pane
- **WHEN** the user clicks the maximize button on a split pane
- **THEN** the system SHALL save both split pane session IDs, destroy the split xterm instances, switch to single view, and open the maximized pane's session as a full-width terminal. The PTY processes SHALL continue running.

#### Scenario: Restore split from single after maximize
- **WHEN** the user switches back to split view after maximizing a pane
- **THEN** the system SHALL restore both saved sessions by creating new xterm instances and reconnecting via WebSocket with scrollback replay. This cycle SHALL work repeatedly without losing state.

#### Scenario: Switch from split to single via view toggle
- **WHEN** the user clicks the single view button while in split view
- **THEN** the system SHALL save the split session IDs, destroy the split xterm instances, and open the left pane's session in single view. The right pane's session SHALL remain available for restoration when returning to split.

#### Scenario: Grid overview
- **WHEN** the grid view mode is active
- **THEN** the main area SHALL display all sessions as cards in a responsive grid (2 columns on medium screens, scaling with viewport). Each card SHALL show the session name, PID, duration, status badge, and a mini terminal preview showing recent output. Clicking a card SHALL switch to single view and open that session's terminal.

### Requirement: Spawn new session via HTMX
The dashboard SHALL provide a "New" button in the sidebar header and a "+" placeholder card in grid view to spawn a new Claude Code session. A modal dialog SHALL present a directory picker for selecting a directory under `/workspace`.

#### Scenario: Create new session via UI
- **WHEN** the user clicks "New", selects a directory from the picker, and confirms
- **THEN** HTMX SHALL POST to the spawn endpoint, the server SHALL spawn the session and push an SSE update event, and the terminal view SHALL auto-open for the new session (reading the `X-Terminal-Id` response header)

#### Scenario: Shift+Click New for split right pane
- **WHEN** the user Shift+clicks the "New" button, selects a directory, and confirms
- **THEN** the new session SHALL auto-open in the split view right pane (moving any existing single terminal to the left pane if not already in split view)

#### Scenario: Directory picker browsing
- **WHEN** the user opens the directory picker
- **THEN** HTMX SHALL GET the directory listing endpoint and render folders under `/workspace` inside the modal dialog with breadcrumb navigation, allowing drill-down into subfolders via `hx-get` with `hx-target` swaps. Folder names SHALL NOT be selectable as text (`user-select: none`). Selection and confirmation behavior is defined by the "New Session modal browses folders and resumes past sessions" and "Select-then-confirm with a single morphing action" requirements.

### Requirement: Terminal control buttons (desktop)
On desktop (>=768px), a control bar SHALL appear below the tab bar with `Esc`, `Ctrl+C`, and `Ctrl+D` buttons styled as DaisyUI `kbd` (keyboard key cap) elements on the terminal dark background. In split view, `Esc` and `^C` buttons SHALL appear in each pane's header. Buttons SHALL send the raw byte directly to the PTY WebSocket as a `Uint8Array` (e.g., `[27]` for Escape, `[3]` for Ctrl+C) and re-focus the terminal after clicking. The control bar SHALL be hidden on mobile (the mobile input bar provides these controls instead).

#### Scenario: Send Escape via button
- **WHEN** the user clicks the Esc button while a Claude Code screen (e.g., /usage) is displayed
- **THEN** the system SHALL send byte `0x1b` to the PTY and Claude Code SHALL dismiss the current screen, identical to pressing the Escape key in a native terminal

### Requirement: Mobile input bar
On mobile (<768px), the mobile input bar SHALL be split into two rows stacked vertically:

**Row 1 (top):** Full-width text input field with clear button (X) and send button. The input SHALL occupy the maximum available width for comfortable typing.

**Row 2 (bottom):** Control buttons spread across the full width with adequate spacing for touch targets. Buttons SHALL be grouped logically: signal buttons (Esc, ^C) on the left, editing (⌫) in the middle, navigation arrows (← → ↑ ↓) on the right. Each button SHALL have a minimum tap target of 36px height for comfortable touch interaction.

#### Scenario: Two-row mobile input layout
- **WHEN** a terminal is open on a mobile viewport (<768px)
- **THEN** the mobile input bar SHALL display as two rows: the text input + send button on top, and the control buttons spread across the bottom row with visual grouping

#### Scenario: Input field has full width
- **WHEN** the mobile input bar is visible
- **THEN** the text input field SHALL span the full width of the bar (minus the send button), giving the user ample space to type and see their input

#### Scenario: Touch-friendly button sizing
- **WHEN** the user taps control buttons on the bottom row
- **THEN** each button SHALL have adequate spacing and size (minimum 36px height) to prevent accidental mis-taps on adjacent buttons

### Requirement: Interactive terminal view
The dashboard SHALL embed xterm.js terminals that connect to sessions via native WebSocket (not HTMX SSE). Terminals SHALL be resizable and support the full Claude Code TUI. In split mode, each pane SHALL have its own independent xterm.js instance and WebSocket connection.

#### Scenario: Open terminal for a session
- **WHEN** the user clicks on a session in the sidebar (single/split mode) or a card (grid mode)
- **THEN** an xterm.js terminal SHALL initialize and connect to that session's WebSocket endpoint, displaying the current terminal state including scrollback replay

#### Scenario: Terminal fills available space
- **WHEN** the terminal view is displayed in any mode
- **THEN** the xterm.js terminal SHALL fill the available pane area and resize dynamically with the browser window using the fit addon, sending resize messages to the server

#### Scenario: Split pane resize
- **WHEN** the user drags the split divider
- **THEN** both terminal panes SHALL resize and both xterm.js instances SHALL re-fit and send updated dimensions to the server

### Requirement: WebSocket reconnection indicator
When the WebSocket connection drops unexpectedly (close code != 1000), the terminal SHALL display reconnection status messages inline using `term.write()`.

#### Scenario: Connection drops
- **WHEN** the WebSocket closes with a code other than 1000
- **THEN** the terminal SHALL display "[Reconnecting... (attempt 1)]" in dim gray text

#### Scenario: Reconnection attempt in progress
- **WHEN** each subsequent reconnection attempt is made
- **THEN** the terminal SHALL display "[Reconnecting... (attempt N)]" showing the attempt number

#### Scenario: Reconnection succeeds
- **WHEN** the WebSocket successfully reconnects
- **THEN** the terminal SHALL display "[Reconnected]" in green text and resume normal interaction

#### Scenario: All retries exhausted
- **WHEN** 10 consecutive attempts fail
- **THEN** the terminal SHALL display "[Connection lost]" in red text and stop retrying

### Requirement: Kill session from UI
The dashboard SHALL allow users to terminate any session from the sidebar. The kill button SHALL appear on hover for all sessions. Clicking the kill button SHALL send a DELETE request to `/api/sessions/{name}` which terminates the session's process group on the server.

#### Scenario: Kill a running session
- **WHEN** the user clicks the kill button on any session entry
- **THEN** HTMX SHALL send a DELETE request, the server SHALL terminate the session's process group, push an SSE update event, all connected clients SHALL receive the updated session list, AND the terminal tab/view SHALL be cleaned up (xterm instance destroyed, tab bar cleared, welcome screen shown if no remaining tabs)

### Requirement: Dark/light theme toggle
The dashboard SHALL support exactly two themes — dark and light — implemented via the Futurism token system on the `data-theme` attribute (`dark` = carbon, `light` = paper). A single toggle control in the header SHALL switch between them; there SHALL be no multi-theme dropdown. The user's preference SHALL persist in localStorage and be restored on reload. Switching the theme SHALL also update the terminal background CSS variable and re-theme open xterm.js instances.

#### Scenario: Toggle theme
- **WHEN** the user clicks the theme toggle
- **THEN** the `data-theme` attribute on the HTML element SHALL switch between `light` and `dark`, the preference SHALL be saved to localStorage, and open terminals SHALL be re-themed

#### Scenario: Theme persistence
- **WHEN** the user reloads the dashboard
- **THEN** the theme SHALL be restored from localStorage (defaulting to dark when unset)

#### Scenario: No multi-theme picker
- **WHEN** the user opens the theme control
- **THEN** it SHALL be a binary light/dark toggle, not a list of named DaisyUI themes

### Requirement: Terminal font rendering
The xterm.js terminal SHALL use system monospace fonts (Menlo, Monaco, Consolas, Liberation Mono) instead of web fonts. UI typography SHALL use the Futurism `Helvetica Neue` system stack (not a web font such as Outfit), and monospace UI text (cwd, duration, tab labels) SHALL use a system monospace stack. Global CSS font rules SHALL NOT use the `*` universal selector, as this interferes with xterm.js's internal character grid measurement; the `body` selector SHALL be used for UI fonts instead.

#### Scenario: Terminal text renders correctly
- **WHEN** a Claude Code session is displayed in the xterm.js terminal
- **THEN** all characters SHALL render with correct monospace spacing with no extra gaps or misaligned characters

#### Scenario: CSS does not interfere with xterm.js
- **WHEN** custom CSS sets a UI font (Helvetica Neue)
- **THEN** the font rule SHALL be scoped to `body` (not `*`) so xterm.js internal measurement elements are not affected

### Requirement: Responsive layout
The dashboard SHALL be responsive across mobile (<768px), tablet (768-1024px), and desktop (>1024px) using plain CSS media queries (no utility-framework responsive prefixes). There SHALL be no horizontal scrolling of the page body at any width; wide content SHALL scroll within its own container.

#### Scenario: Mobile layout
- **WHEN** the viewport is narrower than 768px (e.g., phone in portrait)
- **THEN** the sidebar SHALL be hidden as a slide-out drawer accessible via a hamburger menu (☰) in the header. The header SHALL show only the hamburger, logo mark, session count, theme toggle, and the accent picker collapsed to its compact trigger. A semi-transparent backdrop SHALL overlay the content when the drawer is open. Clicking a session in the drawer SHALL close the drawer and open the terminal. Control buttons SHALL meet a minimum 36px touch target.

#### Scenario: Tablet layout
- **WHEN** the viewport is between 768px and 1024px
- **THEN** the sidebar SHALL be visible with a narrower width. The header SHALL show full text and all controls.

#### Scenario: Desktop layout
- **WHEN** the viewport is wider than 1024px
- **THEN** the sidebar SHALL be full width. All features SHALL be available unchanged.

#### Scenario: No horizontal body scroll
- **WHEN** the dashboard is viewed at any width, including the narrowest mobile widths
- **THEN** the page body SHALL NOT scroll horizontally; any wide content (breadcrumb, tab bar) SHALL scroll within its own `overflow-x` container

### Requirement: Pull to refresh
The dashboard SHALL support pull-to-refresh on touch devices. The implementation SHALL match the tunnel-hub style: an inline text element that expands from zero height showing "Pull to refresh" → "Release to refresh" → "Refreshing..." — no floating indicators, no icons. Touch-only (no mouse support). The pull gesture SHALL NOT activate inside terminal areas.

#### Scenario: Pull to refresh on mobile
- **WHEN** the user pulls down on a non-terminal area (header, sidebar, welcome screen) past the threshold
- **THEN** the indicator text SHALL show "Release to refresh" and releasing SHALL reload the page

#### Scenario: Pull cancelled
- **WHEN** the user pulls down but releases before reaching the threshold
- **THEN** the indicator SHALL collapse back to zero height without refreshing

#### Scenario: Pull inside terminal
- **WHEN** the user pulls down inside a terminal pane
- **THEN** the pull-to-refresh gesture SHALL NOT activate (to avoid interfering with terminal scrolling)

### Requirement: Self-contained application with embedded assets
The dashboard SHALL be served from a single Go binary with core JS assets (htmx.js, the htmx SSE extension, xterm.js + addons) embedded via `go:embed`. All visual styling SHALL be self-contained: the dashboard SHALL NOT load any third-party CSS framework or font from a CDN (no Tailwind, no DaisyUI, no Google Fonts). Styling SHALL come solely from the embedded `style.css` (Futurism token system), and UI typography SHALL use system font stacks.

#### Scenario: Load dashboard
- **WHEN** the user navigates to `http://host:8080/`
- **THEN** the browser SHALL load the dashboard with HTMX and xterm.js from embedded assets and all styling from the embedded `style.css`, making no requests to any CSS, font, or framework CDN

#### Scenario: No external styling dependencies
- **WHEN** the dashboard page is served
- **THEN** the HTML SHALL contain no `<link>` or `<script>` tags pointing to `cdn.tailwindcss.com`, `daisyui`, `fonts.googleapis.com`, or `unpkg.com`

### Requirement: Dashboard supports keyboard shortcuts for tab and view management

The dashboard SHALL provide keyboard shortcuts for common tab and view operations, allowing power users to manage sessions without using the mouse.

#### Scenario: Open new session modal with Ctrl+T

- **WHEN** the user presses Ctrl+T
- **AND** no modal dialog is currently open
- **AND** focus is not in a non-terminal input field
- **THEN** the new session modal is displayed
- **THEN** the default browser new-tab action is prevented (where browser policy allows)

#### Scenario: Close current tab with Ctrl+W

- **WHEN** the user presses Ctrl+W
- **AND** at least one session tab is open
- **AND** no modal dialog is currently open
- **THEN** the currently active session tab is closed
- **THEN** if the session is still running, a confirmation prompt is shown before closing
- **THEN** the default browser close-tab action is prevented (where browser policy allows)

#### Scenario: Switch to tab by index with Ctrl+1 through Ctrl+9

- **WHEN** the user presses Ctrl+N where N is a digit from 1 to 9
- **AND** there are at least N open tabs
- **THEN** the Nth tab becomes the active tab (1-indexed)
- **THEN** the corresponding terminal receives focus

#### Scenario: Tab index exceeds open tab count

- **WHEN** the user presses Ctrl+N where N exceeds the number of open tabs
- **THEN** no action is taken
- **THEN** no error is shown

#### Scenario: Toggle split view with Ctrl+\

- **WHEN** the user presses Ctrl+\
- **AND** at least two session tabs are open
- **THEN** split view is toggled (enabled if disabled, disabled if enabled)

#### Scenario: Shortcuts suppressed when modal is open

- **WHEN** any modal dialog is visible
- **AND** the user presses any registered shortcut key combination
- **THEN** the shortcut action is NOT executed
- **THEN** the keypress is handled normally by the modal

#### Scenario: Shortcuts suppressed in non-terminal input fields

- **WHEN** focus is on an `<input>` or `<textarea>` element that is not the terminal or mobile input bar
- **AND** the user presses a registered shortcut key combination
- **THEN** the shortcut action is NOT executed

### Requirement: New Session modal browses folders and resumes past sessions
The New Session modal SHALL let the user navigate directories under `/workspace` (clicking a folder enters it; the breadcrumb navigates back). Inside a folder the modal SHALL present a "Start a new session" option and a list of that folder's previous sessions fetched from `GET /api/sessions/history`. Each previous-session row SHALL be labeled with its custom name when set, otherwise its relative time and short uuid.

#### Scenario: Browse into a folder and see its sessions
- **WHEN** the user opens the modal and navigates into `/workspace/cmux`
- **THEN** the modal SHALL show "Start a new session" and the folder's previous sessions, each labeled by custom name or `<relative time> · <short uuid>`

#### Scenario: Folder with no previous sessions
- **WHEN** the user navigates into a folder that has no recorded sessions
- **THEN** the modal SHALL show a "no previous sessions" empty state under "Start a new session"

### Requirement: Select-then-confirm with a single morphing action
The modal SHALL indicate the chosen option by background color only (no radio control, no edge bar) and SHALL allow only one selection at a time. "Start a new session" SHALL be selected by default when a folder is entered. The footer SHALL contain a permanent Cancel button and a single primary action button that is always present and relabels in place: **Launch** when "Start a new session" is selected, **Resume** when a previous session is selected. At the `/workspace` root the primary button SHALL be present but disabled. Navigating to another folder SHALL reset the selection to the default.

#### Scenario: Default selection launches a new session
- **WHEN** the user enters a folder and clicks the primary button without changing the selection
- **THEN** the modal SHALL POST `{cwd}` to start a new session

#### Scenario: Selecting a previous session changes the action to Resume
- **WHEN** the user selects a previous-session row
- **THEN** that row SHALL be highlighted, the primary button SHALL read "Resume", and clicking it SHALL POST `{cwd, resume:<uuid>}`

#### Scenario: Primary action stays anchored
- **WHEN** the selection changes between "new" and a previous session
- **THEN** the primary button SHALL relabel in place without appearing/disappearing, and the Cancel button SHALL remain present and unmoved

### Requirement: Delete a previous session from the resume list
Each previous-session row in the New Session modal's "Previous sessions" list SHALL carry an inline delete affordance. The delete affordance SHALL appear ONLY in the modal's resume list and SHALL NOT be added to the live sidebar session cards (which retain their existing Kill control). Because the resume row is itself a `<button>` and may not nest another interactive control, the row SHALL be wrapped in a non-interactive container (`div.arow-wrap`, `position: relative`) holding the existing resume `<button class="arow sa-row">` AND a SIBLING delete `<button class="arow-del">`. The delete control's icon SHALL be a `currentColor` stroke SVG or a text glyph — NOT an emoji.

The delete control's click handler SHALL call `e.stopPropagation()` and `e.preventDefault()` so it never triggers the row's resume-select handler (`dirPickerSetSel('resume', <uuid>, row)`).

Deletion SHALL require an inline two-step confirm (no native `window.confirm`). The first click SHALL swap the `.arow-del` control's contents into an accent confirm + ghost cancel pair, styled with existing Futurism tokens (`--accent` for confirm, `--muted`/`--line` for the ghost cancel) and no hardcoded hex; the resume button markup SHALL remain untouched. New styles SHALL be added to `app.css` only (never `futurism.css`).

Confirming SHALL issue `fetch("/api/sessions/history/" + uuid, { method: "DELETE" })`. On HTTP 204 the modal SHALL re-fetch `GET /api/sessions/history?cwd=<path>` for the current folder and re-render the Previous-sessions list (the re-fetch-and-render is the source of truth for the modal — the modal SHALL NOT rely on the SSE/broker update to refresh its list), so the deleted row disappears. On a non-204 response (e.g. 404) the inline confirm SHALL revert to the idle delete affordance and surface a brief on-brand failure indication (a transient text/class swap on the control — no `window.alert`).

#### Scenario: Delete a previous session row
- **WHEN** the user clicks the delete control on a previous-session row, then clicks the inline confirm, and the server responds 204
- **THEN** the row's `DELETE /api/sessions/history/{uuid}` SHALL be issued, the folder's history SHALL be re-fetched and re-rendered, and the deleted row SHALL disappear from the list

#### Scenario: Delete control does not trigger resume-select
- **WHEN** the user clicks the delete control (or its confirm/cancel) on a previous-session row
- **THEN** `stopPropagation`/`preventDefault` SHALL prevent the row's resume-select handler from firing, so the primary action does NOT change to "Resume" and no resume selection occurs

#### Scenario: Cancel the inline confirm
- **WHEN** the user clicks the delete control and then clicks the inline cancel
- **THEN** the control SHALL revert to the idle delete affordance and no DELETE request SHALL be issued

#### Scenario: Delete failure reverts and surfaces feedback
- **WHEN** the user confirms deletion and the server responds with a non-204 status
- **THEN** the inline confirm SHALL revert to the idle delete affordance and a brief on-brand failure indication SHALL be shown without any `window.alert` or `window.confirm`

#### Scenario: Sidebar cards are unaffected
- **WHEN** the delete affordance is present in the resume list
- **THEN** the live sidebar session cards SHALL continue to show only their existing Kill control and SHALL NOT gain a history-delete control

### Requirement: Create a new project folder from the directory picker
The directory picker in the NEW SESSION modal SHALL provide a "+ NEW PROJECT…" affordance that lets the user create a new folder in the directory they are currently browsing and immediately proceed to launch a session in it. The affordance SHALL be a row pinned directly under the folder list and SHALL be present at every browse depth (workspace root and any subfolder), so the new folder is created inside the current breadcrumb path rather than only at the root.

Because the picker fragment is re-rendered on every drill-down and breadcrumb navigation, the affordance SHALL reset to its idle (collapsed) state after any such navigation; this reset is intended behavior.

The affordance SHALL be styled per the approved mockup and remain kit-conformant (square corners, 2px borders, a single accent): an idle `.newrow` rendered in the muted text color with an accent `+`, and an inline `.newedit` editor rendered on the `--field` background. Any divergence from the Futurism kit SHALL be recorded in the `app.css` override ledger.

#### Scenario: New-project row pinned at every depth
- **WHEN** the user opens the directory picker or drills into any subfolder
- **THEN** a "+ NEW PROJECT…" row SHALL be rendered directly beneath the folder list for the currently browsed directory

#### Scenario: Navigating away resets the editor
- **WHEN** the inline editor is open and the user navigates via a breadcrumb segment or drills into a subfolder
- **THEN** the picker fragment SHALL re-render and the affordance SHALL return to its idle collapsed "+ NEW PROJECT…" row

### Requirement: Inline new-project editor
Clicking the "+ NEW PROJECT…" row SHALL swap the row in place for an inline editor containing: a text input for the folder name that receives focus automatically, a "git init" checkbox that defaults to on, and CANCEL and CREATE actions. A hint line SHALL read "Enter to create · Esc to cancel". Pressing Enter in the name input SHALL trigger create; pressing Esc SHALL cancel and collapse back to the idle row. The Esc handler SHALL call `preventDefault` and `stopPropagation` so cancelling the editor does not also close the surrounding modal dialog (the native `<dialog>` Esc-cancel is a keydown default action, which `stopPropagation` alone cannot block).

#### Scenario: Open the editor
- **WHEN** the user clicks the "+ NEW PROJECT…" row
- **THEN** the row SHALL be replaced in place by the inline editor, the name input SHALL be focused, and the "git init" checkbox SHALL be checked by default

#### Scenario: Keyboard controls
- **WHEN** the editor is open and focused and the user presses Enter
- **THEN** the client SHALL attempt to create the project

#### Scenario: Esc cancels without closing the modal
- **WHEN** the editor is open and the user presses Esc
- **THEN** the editor SHALL collapse back to the idle "+ NEW PROJECT…" row, the event SHALL not propagate to the dialog, and the NEW SESSION modal SHALL remain open

### Requirement: Client-side new-project validation
Before sending a create request, the client SHALL pre-check the entered name against the same `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` regex the server enforces, and SHALL also check the name against the folder names currently listed in the picker to catch a duplicate. This client-side check is a UX convenience only — the server remains authoritative. On a failed pre-check the client SHALL show an inline error line and apply the existing `input.err-flash` accent outline to the name input, and SHALL NOT send the request. The inline error SHALL clear on the next keystroke in the name input.

#### Scenario: Reject an invalid name locally
- **WHEN** the user enters a name that fails the regex and triggers create
- **THEN** the client SHALL show the inline error "Invalid name" with the `input.err-flash` outline and SHALL NOT send a request

#### Scenario: Reject a duplicate name locally
- **WHEN** the user enters a name that matches a folder already listed in the picker and triggers create
- **THEN** the client SHALL show the inline error "Folder already exists" with the `input.err-flash` outline and SHALL NOT send a request

#### Scenario: Error clears on next keystroke
- **WHEN** an inline error is showing and the user types in the name input
- **THEN** the inline error line and the `input.err-flash` outline SHALL be removed

### Requirement: New project is created and a session auto-launched on success
On a create request that returns HTTP 201, the client SHALL spawn a session in the new folder immediately: a fresh folder has no conversations to resume, so a select-then-LAUNCH step would be a pointless extra click. The client sets the spawn form's hidden `cwd` to the new folder's full path, clears `resume`, and submits the existing spawn form — the spawn response's `X-Terminal-Id` handler then closes the modal and opens the terminal tab, identical to a manual LAUNCH. When the response is HTTP 201 with a `warning` field (folder created but `git init` failed), the client SHALL still launch the session and SHALL show the "created, git init failed" notice as a kit toast, since the notice must outlive the closing modal. When the server returns a `400` or `409`, the client SHALL surface the server's message inline using the same error affordance as the client-side pre-check, keeping the editor open. (For a plain folder-row click, the selected state SHALL show only the breadcrumb and session-actions: `dpSelectFolder` hides the folder list and the new-project affordance, and the browse reset restores them.)

#### Scenario: Successful creation launches a session
- **WHEN** the create request returns HTTP 201
- **THEN** the client SHALL submit the spawn form with the new folder as `cwd`, and on the spawn response the modal SHALL close and the new session's terminal tab SHALL open

#### Scenario: Creation with a git-init warning
- **WHEN** the create request returns HTTP 201 with a `warning` field
- **THEN** the client SHALL still launch the session and SHALL show a "created, git init failed" kit toast

#### Scenario: Server rejects a name the client did not catch
- **WHEN** the create request returns HTTP 409 (or 400)
- **THEN** the client SHALL display the server's message inline (for example "Folder already exists") with the `input.err-flash` outline and keep the editor open

### Requirement: Pinned sidebar-footer surface switcher

The sidebar SHALL carry a pinned footer navigation, separate from the scrolling body, with entries for the app's peer surfaces: **Dashboard**, **Logs**, and **Settings**. Dashboard and Logs are routes (`/`, `/logs`) and mark the active one; Settings opens the existing settings modal in place (not a route). The Settings trigger moves here from the header — the header no longer carries the settings gear. In the collapsed 48px rail the entries render icon-only; expanded, icon + label.

#### Scenario: switching surfaces from the footer
- **WHEN** the user clicks Logs in the sidebar footer
- **THEN** the app navigates to `/logs`, the Logs entry is marked active, and Settings still opens the modal (does not navigate)

#### Scenario: settings no longer in the header
- **WHEN** the dashboard renders
- **THEN** the settings gear is absent from the header and Settings is reachable from the sidebar footer

### Requirement: Per-surface sidebar body and header context

The sidebar body SHALL be per-surface: the session list on the dashboard (`/`), and a logs-context body on `/logs` (sessions are not shown on the logs surface). The header sub-label SHALL reflect the active surface (`DASHBOARD` on `/`, `LOGS` on `/logs`), and the brand logo SHALL link to `/`.

#### Scenario: logs surface hides sessions and labels itself
- **WHEN** the user is on `/logs`
- **THEN** the sidebar body shows logs context (not the session list), the header sub-label reads `LOGS`, and clicking the brand logo returns to `/`

### Requirement: Active session persists across surface navigation

The dashboard SHALL remember the currently-open session (persisted client-side) and, on returning to the dashboard, automatically reopen it if it is still live — re-attaching and repainting from sessiond's snapshot. A session the user explicitly closed/killed SHALL NOT reopen. Restoration MUST run after the view managers initialize, so the reopened session is not discarded.

#### Scenario: session survives a trip to the logs surface
- **WHEN** a session is open, the user navigates to `/logs`, then returns to the dashboard
- **THEN** that session reopens with its content restored (re-attached from the snapshot), provided it is still live

#### Scenario: a closed session does not spring back
- **WHEN** the user closes/kills the open session (returning to the welcome screen) and later returns to the dashboard
- **THEN** the welcome screen is shown, not the closed session

