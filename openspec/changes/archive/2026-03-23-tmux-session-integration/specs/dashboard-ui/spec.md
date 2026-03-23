## MODIFIED Requirements

### Requirement: Sidebar session list with SSE real-time updates
The dashboard SHALL display a persistent left sidebar listing all Claude Code sessions (discovered via tmux). The sidebar SHALL remain visible in all view modes. The list SHALL auto-update via Server-Sent Events — when session state changes, the server SHALL push an SSE event that triggers HTMX to re-fetch and swap the session list HTML fragment. Each entry SHALL show the session's working directory, tmux session name, start time, duration, and status as subtle monospace text. Status indicators: emerald dot and "active" label for running sessions, dim dot and "stopped" label for dead sessions. All sessions SHALL have uniform styling — there is no "external" or "managed" distinction.

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

### Requirement: Kill session from UI
The dashboard SHALL allow users to terminate any session from the sidebar. The kill button SHALL appear on hover for all sessions (not just dashboard-spawned ones). Clicking the kill button SHALL send a DELETE request which runs `tmux kill-session` on the server.

#### Scenario: Kill a running session
- **WHEN** the user clicks the kill button on any session entry
- **THEN** HTMX SHALL send a DELETE request, the server SHALL terminate the tmux session, push an SSE update event, all connected clients SHALL receive the updated session list, AND the terminal tab/view SHALL be cleaned up (xterm instance destroyed, tab bar cleared, welcome screen shown if no remaining tabs)

