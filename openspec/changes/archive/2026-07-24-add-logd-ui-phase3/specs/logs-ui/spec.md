## ADDED Requirements

### Requirement: Logs route and view

The frontend SHALL serve a `/logs` route that renders the Logs view inside the dashboard shell (shared header + sidebar). The view comprises a live status strip, a filter/search bar, a log list, and a footer. The dashboard (`/`) is unchanged.

#### Scenario: opening the logs surface
- **WHEN** the user navigates to `/logs` (via the sidebar-footer Logs entry or directly)
- **THEN** the dashboard shell renders with the Logs view in the main area and the header/sidebar in logs context

### Requirement: Live status strip

The Logs view SHALL show one status chip per health-probed peer (backend, sessiond, frontend, holesail — not logd), each with an up/down indicator, the service name, and last-seen. It reads `GET /api/status` on load and updates live via `/api/status/stream`. `up` is rendered with the committed (ink) treatment, `down` with the attention (accent) treatment — no green/amber.

#### Scenario: a service going down updates its chip live
- **WHEN** a monitored service transitions to `down` while the Logs view is open
- **THEN** its chip flips to the accent/down state without a reload (via the status SSE)

### Requirement: Filters mirror the query API

The filter bar SHALL provide a service selector, a level selector, and a text search, applied to the log list with the same semantics as `GET /api/logs`: exact service match, exact level match, and case-insensitive substring for search.

#### Scenario: filtering by service and level
- **WHEN** the user selects service=backend and level=error
- **THEN** the list shows only backend records whose level is exactly error

### Requirement: Log list rendering

The log list SHALL render records newest-first as dense monospace rows showing time, service, level, and message (with attributes). Error records are emphasized (accent) and non-JSON `raw` records are shown as such; the list is hairline-separated (no per-row card shadow).

#### Scenario: an error and a raw line render distinctly
- **WHEN** the list contains an error record and a non-JSON raw record
- **THEN** the error row is accent-emphasized and the raw row is shown as raw text, both legible newest-first

### Requirement: Live-tail with scroll-loaded history

Live-tail SHALL follow newest records via `/api/logs/stream` and auto-scroll. Scrolling up SHALL pause following and lazily load older records (querying older than the oldest shown); a jump-to-latest control SHALL appear while paused and resume following when activated. There is no page control.

#### Scenario: scroll up to browse, jump to resume
- **WHEN** the user scrolls up in the log list
- **THEN** following pauses, older records load as needed, and a jump-to-latest control appears; activating it scrolls to newest and resumes live-tail

### Requirement: Log service unavailable state

When logd is unreachable (the frontend proxy returns a gateway error), the Logs view SHALL show an explicit unavailable state rather than failing, and the rest of the dashboard SHALL remain usable.

#### Scenario: logd down
- **WHEN** `/api/logs` returns a 502 (logd unreachable)
- **THEN** the Logs view shows a "log service unavailable" state and the dashboard/other surfaces still work
