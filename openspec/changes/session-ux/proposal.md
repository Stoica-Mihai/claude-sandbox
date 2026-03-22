## Why

The sidebar session list shows directory name, PID, duration, and a status dot — but there is no way to tell which sessions have recent output (e.g., Claude actively generating) versus sessions that are idle. All alive sessions look the same: a pulsing emerald dot and the word "active." When running 3-4 sessions in parallel, users must click into each terminal to figure out which one is actually doing work. Sessions are also identified only by their working directory basename, which is ambiguous when multiple sessions share the same directory or when the directory name is generic (e.g., "src").

## What Changes

- Add a "last active" relative timestamp to each session card (e.g., "2s ago", "5m ago") that updates on each SSE refresh, showing when the session last produced PTY output
- Add an output pulse animation on session cards that have produced output within the last few seconds, giving an immediate visual signal of activity
- Add an optional editable session name/label that appears on the card and in tab/pane headers, replacing the directory basename when set
- Add a backend endpoint to update a session's name, stored in-memory on the SessionManager

## Capabilities

### Modified Capabilities
- `dashboard-ui`: Session cards gain a "last active" timestamp, output pulse animation, and an editable name label
- `session-api`: ManagedSession tracks last activity timestamp; optional session name field; new PUT endpoint for name updates

## Impact

- **dashboard/session.go**: Add `LastActivity time.Time` and `Name string` fields to `ManagedSession`. Add `LastActivity` and `Name` to `DisplaySession`. Update `readPTY` to stamp `LastActivity` on each read. Add `humanRelativeTime` helper. Add `SetName` method on `SessionManager`.
- **dashboard/handlers.go**: Add `PUT /api/sessions/:terminalId/name` handler. Pass `LastActivity` and `Name` through to templates.
- **dashboard/web/templates/fragments/sessions.html**: Show `Name` (or fall back to `DirName`), render "last active" timestamp, add `pulse-output` CSS class when recently active.
- **dashboard/web/static/js/views.js**: Read `data-session-name` from cards for tab bar / pane headers. Add inline rename on double-click (stretch goal).
- **dashboard/web/static/css/style.css**: Add `@keyframes pulse-output` animation with a distinct green glow, separate from the existing `pulse-alive` dot animation.
- No database or file persistence — names and timestamps live in memory and reset on dashboard restart.
