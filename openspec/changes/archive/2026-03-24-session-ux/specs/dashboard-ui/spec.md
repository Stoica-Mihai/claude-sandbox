## MODIFIED Requirements

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
