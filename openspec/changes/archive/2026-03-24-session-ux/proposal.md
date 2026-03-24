## Why

The sidebar session list shows directory name, duration, and a status dot — but there is no way to tell which sessions have recent output (e.g., Claude actively generating) versus sessions that are idle. All alive sessions look the same: a pulsing emerald dot and the word "active." When running 3-4 sessions in parallel, users must click into each terminal to figure out which one is actually doing work. Sessions are also identified only by their working directory basename, which is ambiguous when multiple sessions share the same directory.

## What Changes

- Add a "last active" relative timestamp to each session card (e.g., "2s ago", "5m ago") that updates on each SSE refresh, showing when the session last produced output
- Add an output pulse animation on session cards that have produced output within the last few seconds, giving an immediate visual signal of activity
- Add an optional editable session name/label that appears on the card and in tab headers, replacing the directory basename when set
- Add a backend endpoint to update a session's name, stored in-memory on the SessionManager

## Capabilities

### New Capabilities

### Modified Capabilities
- `dashboard-ui`: Session cards gain a "last active" timestamp, output pulse animation, and an editable name label
- `session-api`: Relay tracks last activity timestamp; SessionManager holds optional session names; new PUT endpoint for name updates

## Impact

- **`dashboard/relay.go`**: Add `lastActivity time.Time` field with `lastActivityMu sync.RWMutex`. Stamp `time.Now()` in `broadcast()` on each output chunk. Add `GetLastActivity() time.Time` getter.
- **`dashboard/session.go`**: Add `sessionNames map[string]string` to SessionManager. Add `LastActivity`, `LastActiveStr`, `DisplayName`, `RecentActivity` fields to `DisplaySession`. Update `ListSessions` to enrich DisplaySession from relay and name map. Add `SetSessionName`/`GetSessionName` methods. Add `humanRelativeTime` helper.
- **`dashboard/handlers.go`**: Add `PUT /api/sessions/{terminalId}/name` handler. Register route.
- **`dashboard/web/templates/fragments/sessions.html`**: Show `DisplayName` as primary label, render `LastActiveStr`, apply `pulse-output` class when `RecentActivity` is true. Update `data-session` to use `DisplayName`.
- **`dashboard/web/static/css/style.css`**: Add `@keyframes pulse-output` animation with green glow on left border.
- No new dependencies. No persistence — names and timestamps live in memory and reset on dashboard restart.
