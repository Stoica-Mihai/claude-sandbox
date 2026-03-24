# dashboard-ui Specification

## Purpose
TBD - created by archiving change web-dashboard. Update Purpose after archive.
## Requirements
### Requirement: Sidebar session list with SSE real-time updates
The dashboard SHALL display a persistent left sidebar listing all Claude Code sessions (discovered via tmux). The sidebar SHALL remain visible in all view modes. The list SHALL auto-update via Server-Sent Events — when session state changes, the server SHALL push an SSE event that triggers HTMX to re-fetch and swap the session list HTML fragment. All sessions SHALL have uniform styling — there is no "external" or "managed" distinction.

#### Scenario: Dashboard loads with active sessions
- **WHEN** the user opens the dashboard and tmux sessions are running
- **THEN** the sidebar SHALL render all sessions with directory, session name, duration, and emerald "active" status

#### Scenario: Dashboard loads with no sessions
- **WHEN** the user opens the dashboard and no tmux sessions exist
- **THEN** the sidebar SHALL display an empty state prompting to create a new session

#### Scenario: Session status updates via SSE
- **WHEN** a session is spawned, killed, or exits while the dashboard is open
- **THEN** the server SHALL push an SSE `update` event, HTMX SHALL trigger a fetch of the session list fragment, and swap the updated HTML into the sidebar without a full page reload

#### Scenario: Click any session to open terminal
- **WHEN** the user clicks on any session card in the sidebar
- **THEN** an xterm.js terminal SHALL initialize and connect to that session's WebSocket endpoint, displaying the current terminal state via tmux's pane replay. There SHALL be no dimmed or non-interactive session entries.

### Requirement: Session card display
Each session card in the sidebar SHALL display: the tmux session name, a live-ticking duration (updated every second client-side from the `CreatedAt` timestamp), a `DisplayName` as the primary label (custom name or directory basename), the full CWD path as secondary text, and an "active"/"stopped" status label. The pulsing status dot indicator SHALL be removed — the text label is sufficient.

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
The dashboard SHALL provide a "New" button in the sidebar header and a "+" placeholder card in grid view to spawn a new Claude Code session. A DaisyUI modal SHALL present a directory picker for selecting a directory under `/workspace`.

#### Scenario: Create new session via UI
- **WHEN** the user clicks "New", selects a directory from the picker, and confirms
- **THEN** HTMX SHALL POST to the spawn endpoint, the server SHALL spawn the session and push an SSE update event, and the terminal view SHALL auto-open for the new session (reading the `X-Terminal-Id` response header)

#### Scenario: Shift+Click New for split right pane
- **WHEN** the user Shift+clicks the "New" button, selects a directory, and confirms
- **THEN** the new session SHALL auto-open in the split view right pane (moving any existing single terminal to the left pane if not already in split view)

#### Scenario: Directory picker browsing
- **WHEN** the user opens the directory picker
- **THEN** HTMX SHALL GET the directory listing endpoint and render directories under `/workspace` inside a DaisyUI modal with breadcrumb navigation, allowing drill-down into subdirectories via `hx-get` with `hx-target` swaps. Directory names SHALL NOT be selectable as text (`user-select: none`). The current browsed directory SHALL be the default selection (via hidden input). Subdirectories SHALL use toggleable checkboxes (not radio buttons) — clicking a checkbox selects that subdirectory, clicking again deselects and reverts to the current directory default. Only one checkbox may be checked at a time. Navigating into a subdirectory or clicking a breadcrumb SHALL re-render the picker with clean (unchecked) state, defaulting to the new current directory.

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
The dashboard SHALL allow users to terminate any session from the sidebar. The kill button SHALL appear on hover for all sessions (not just dashboard-spawned ones). Clicking the kill button SHALL send a DELETE request which runs `tmux kill-session` on the server.

#### Scenario: Kill a running session
- **WHEN** the user clicks the kill button on any session entry
- **THEN** HTMX SHALL send a DELETE request, the server SHALL terminate the tmux session, push an SSE update event, all connected clients SHALL receive the updated session list, AND the terminal tab/view SHALL be cleaned up (xterm instance destroyed, tab bar cleared, welcome screen shown if no remaining tabs)

### Requirement: Dark/light theme toggle
The dashboard SHALL support dark and light themes using DaisyUI's `data-theme` attribute. The toggle SHALL appear in the header. The user's preference SHALL persist in localStorage.

#### Scenario: Toggle theme
- **WHEN** the user clicks the theme toggle button
- **THEN** the `data-theme` attribute on the HTML element SHALL switch between `light` and `dark`, and the preference SHALL be saved to localStorage

#### Scenario: Theme persistence
- **WHEN** the user reloads the dashboard
- **THEN** the theme SHALL be restored from localStorage

### Requirement: Terminal font rendering
The xterm.js terminal SHALL use system monospace fonts (Menlo, Monaco, Consolas, Liberation Mono) instead of web fonts. Global CSS font rules SHALL NOT use the `*` universal selector, as this interferes with xterm.js's internal character grid measurement. The `body` selector SHALL be used for UI fonts instead.

#### Scenario: Terminal text renders correctly
- **WHEN** a Claude Code session is displayed in the xterm.js terminal
- **THEN** all characters SHALL render with correct monospace spacing with no extra gaps or misaligned characters

#### Scenario: CSS does not interfere with xterm.js
- **WHEN** custom CSS sets a UI font (e.g., Outfit)
- **THEN** the font rule SHALL be scoped to `body` (not `*`) so xterm.js internal measurement elements are not affected

### Requirement: Responsive layout
The dashboard SHALL be responsive across mobile (<768px), tablet (768-1024px), and desktop (>1024px) using Tailwind CSS responsive prefixes.

#### Scenario: Mobile layout
- **WHEN** the viewport is narrower than 768px (e.g., phone in portrait)
- **THEN** the sidebar SHALL be hidden as a slide-out drawer accessible via a hamburger menu (☰) in the header. The header SHALL show only the hamburger, logo icon (no text), session count with a terminal icon (no "sessions" label), and theme toggle. View mode buttons and session badge text SHALL be hidden. Split and grid views SHALL be forced to single view. The drawer width SHALL be `w-56` (224px). A semi-transparent backdrop SHALL overlay the content when the drawer is open. Clicking a session in the drawer SHALL close the drawer and open the terminal.

#### Scenario: Tablet layout
- **WHEN** the viewport is between 768px and 1024px
- **THEN** the sidebar SHALL be visible with a narrower width (`w-56`). All view modes (single, split, grid) SHALL be available. The header SHALL show full text and all controls.

#### Scenario: Desktop layout
- **WHEN** the viewport is wider than 1024px
- **THEN** the sidebar SHALL be full width (`w-72`). All features SHALL be available unchanged.

#### Scenario: Resize from desktop to mobile
- **WHEN** the browser window is resized from desktop to mobile width
- **THEN** the view SHALL be forced to single, the sidebar SHALL collapse to a drawer, and the sidebar SHALL close if open.

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
The dashboard SHALL be served from a single Go binary with core JS assets (htmx.js, xterm.js) embedded via `go:embed`. Tailwind CSS and DaisyUI SHALL be loaded from CDN. No external CDN dependencies for core interactivity.

#### Scenario: Load dashboard
- **WHEN** the user navigates to `http://host:8080/`
- **THEN** the browser SHALL load the dashboard with HTMX and xterm.js from embedded assets and Tailwind/DaisyUI from CDN

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

