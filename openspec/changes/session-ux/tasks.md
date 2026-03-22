## 1. Backend: activity tracking and name field

- [ ] 1.1 Add `LastActivity time.Time` and `Name string` fields to `ManagedSession` in `dashboard/session.go`
- [ ] 1.2 Update `readPTY` to set `ms.LastActivity = time.Now()` on each successful read (inside `if n > 0`)
- [ ] 1.3 Add `LastActivity time.Time`, `LastActiveStr string`, `Name string`, `DisplayName string`, and `RecentActivity bool` fields to `DisplaySession`
- [ ] 1.4 Update `ListSessions` to populate `LastActiveStr` (human relative time), `DisplayName` (Name or DirName fallback), and `RecentActivity` (LastActivity within 5s) for managed sessions
- [ ] 1.5 Add a `humanRelativeTime(t time.Time) string` helper that returns "2s ago", "1m ago", "idle", etc.

## 2. Backend: session name endpoint

- [ ] 2.1 Add `SetName(terminalID, name string) error` method on `SessionManager` that updates the Name field and calls `broker.Publish()`
- [ ] 2.2 Add `PUT /api/sessions/:terminalId/name` handler in `dashboard/handlers.go` that parses `{"name": "..."}` JSON body and calls `SetName`
- [ ] 2.3 Register the new route in the server's route setup

## 3. Frontend: session card template

- [ ] 3.1 Update `sessions.html` to show `{{.DisplayName}}` as the primary label instead of `{{.CWD}}`; keep CWD as secondary text below
- [ ] 3.2 Add `{{.LastActiveStr}}` display near the duration text on managed session cards
- [ ] 3.3 Add `pulse-output` CSS class to the session card div when `{{.RecentActivity}}` is true
- [ ] 3.4 Add `data-session` attribute to use `{{.DisplayName}}` so tab bar and pane headers pick up the name

## 4. Frontend: CSS animation

- [ ] 4.1 Add `@keyframes pulse-output` animation in `dashboard/web/static/css/style.css` with a green glow on the left border
- [ ] 4.2 Add `.pulse-output` class that applies the animation
- [ ] 4.3 Add light mode variant for `pulse-output` under `[data-theme="light"]`

## 5. Frontend: views.js name integration

- [ ] 5.1 Verify `updateSingleTabBar` reads `data-session` from cards (already does) — no change needed if `data-session` is updated in template
- [ ] 5.2 Verify `updateSplitPaneHeader` reads `data-session` from cards — same check
- [ ] 5.3 Verify `updateSingleStatusBar` picks up the display name correctly

## 6. Verify and test

- [ ] 6.1 Verify the Go code builds cleanly (`go build ./...` in dashboard/)
- [ ] 6.2 Test spawning a session and confirming `LastActiveStr` appears on the card after output is produced
- [ ] 6.3 Test the pulse animation appears on cards with recent output and disappears after idle
- [ ] 6.4 Test renaming a session via `curl -X PUT` and confirming the name updates in the sidebar, tab bar, and pane header
- [ ] 6.5 Test that clearing a name (empty string) reverts to directory basename display
- [ ] 6.6 Test that external sessions are unaffected (no activity data, no rename option)
