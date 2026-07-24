## ADDED Requirements

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
