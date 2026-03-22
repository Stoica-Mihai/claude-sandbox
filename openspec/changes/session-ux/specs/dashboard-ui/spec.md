## MODIFIED Requirements

### Requirement: Session activity indicator on sidebar cards
Each session card in the sidebar SHALL display a "last active" relative timestamp (e.g., "2s ago", "1m ago", "idle") showing when the session last produced PTY output. When a managed session has produced output within the last 5 seconds, the session card SHALL display an output pulse animation (a brief green glow on the card border) to give an immediate visual signal that the session is actively generating. The pulse animation SHALL be visually distinct from the existing `pulse-alive` status dot animation. External (non-managed) sessions SHALL NOT show the output pulse or last-active timestamp, since the dashboard has no PTY access to track their output.

#### Scenario: Session with recent output
- **WHEN** the session list is rendered and a managed session's last activity is within 5 seconds of the current time
- **THEN** the session card SHALL display the `pulse-output` CSS class (green glow animation on the left border) AND the last-active timestamp SHALL show a low value (e.g., "2s ago")

#### Scenario: Session idle for a while
- **WHEN** a managed session has not produced PTY output for more than 5 seconds
- **THEN** the session card SHALL NOT have the `pulse-output` animation, and the last-active timestamp SHALL show the elapsed time (e.g., "3m ago", "1h ago")

#### Scenario: Session never produced output
- **WHEN** a managed session was just spawned and has not yet produced any PTY output
- **THEN** the last-active timestamp SHALL display "idle" or fall back to showing time since session start

#### Scenario: External session has no activity data
- **WHEN** a session is detected from `~/.claude/sessions/` but is not managed by the dashboard
- **THEN** the session card SHALL NOT show a last-active timestamp or output pulse, since the dashboard has no PTY to monitor

### Requirement: Editable session name in sidebar and headers
Each managed session card SHALL display a session name/label. By default, the name SHALL be the directory basename (matching current behavior). Users SHALL be able to set a custom name that replaces the directory basename on the card, in tab bar labels, and in split pane headers. The full CWD path SHALL remain visible on the card below the name. The name SHALL be settable via an API call (e.g., from a rename button or inline edit). When no custom name is set, all existing display behavior SHALL be preserved unchanged.

#### Scenario: Session with custom name
- **WHEN** a managed session has a custom name set (e.g., "backend refactor")
- **THEN** the session card SHALL display the custom name as the primary label, the tab bar SHALL use the custom name, and split pane headers SHALL use the custom name. The CWD path SHALL still appear on the card as secondary text.

#### Scenario: Session without custom name
- **WHEN** a managed session has no custom name (empty string)
- **THEN** the session card, tab bar, and pane headers SHALL display the directory basename, identical to current behavior

#### Scenario: Rename updates all views
- **WHEN** a session's name is updated via the API
- **THEN** the SSE update SHALL trigger a session list refresh, and the new name SHALL appear on the sidebar card. If the session is open in a tab or split pane, the header/tab label SHALL update on the next card state refresh.

#### Scenario: External sessions cannot be named
- **WHEN** an external (non-managed) session appears in the sidebar
- **THEN** no rename option SHALL be available, and the card SHALL continue to show the directory basename only
