## 1. Relay: activity tracking

- [x] 1.1 Add `lastActivity time.Time` and `lastActivityMu sync.RWMutex` fields to `Relay` struct in `relay.go`
- [x] 1.2 At the top of `broadcast()`, stamp `r.lastActivity = time.Now()` under `lastActivityMu.Lock()` (before the viewer iteration)
- [x] 1.3 Add `GetLastActivity() time.Time` getter that reads under `lastActivityMu.RLock()`

## 2. SessionManager: names and enriched DisplaySession

- [x] 2.1 Add `sessionNames map[string]string` to `SessionManager` struct, initialize in `NewSessionManager`
- [x] 2.2 Add `SetSessionName(sessionName, displayName string)` and `GetSessionName(sessionName string) string` methods (protected by existing `mu`)
- [x] 2.3 Add `LastActivity time.Time`, `LastActiveStr string`, `RecentActivity bool`, and `DisplayName string` fields to `DisplaySession`
- [x] 2.4 Add `humanRelativeTime(t time.Time) string` helper that returns "2s ago", "1m ago", "1h ago", or empty for zero time
- [x] 2.5 Update `ListSessions` to enrich each `DisplaySession` on every call (not just cache miss): get relay via `GetRelay`, call `GetLastActivity`, compute `LastActiveStr` and `RecentActivity` (within 5s), get custom name via `GetSessionName`, compute `DisplayName` (custom name or DirName fallback). Enrichment must happen after cache lookup since activity data changes faster than the 2s cache TTL.
- [x] 2.6 Clean up `sessionNames` entries in `syncRelays` when sessions are removed (prevent stale name accumulation)

## 3. Handler: rename endpoint

- [x] 3.1 Add `handleSetSessionName` handler: parse `{terminalId}` path value, decode `{"name":"..."}` JSON body, call `sm.SetSessionName`, publish SSE event, return updated sessions fragment. Return 404 if session not found.
- [x] 3.2 Register `PUT /api/sessions/{terminalId}/name` route in `NewServer`

## 4. Frontend: session card template

- [x] 4.1 Update `sessions.html`: show `{{.DisplayName}}` as primary label, keep `{{.CWD}}` as secondary text
- [x] 4.2 Add `{{.LastActiveStr}}` display near the duration text
- [x] 4.3 Conditionally add `pulse-output` class to session card div when `{{.RecentActivity}}` is true
- [x] 4.4 Update `data-session` attribute to use `{{.DisplayName}}`
- [x] 4.5 Add a rename button (small pencil icon or text link) on each session card that prompts for a new name and sends a PUT request to `/api/sessions/{terminalId}/name`

## 5. Frontend: CSS animation

- [x] 5.1 Add `@keyframes pulse-output` animation in `style.css` — green glow on left border, 1.5s cycle
- [x] 5.2 Add `.pulse-output` class that applies the animation

## 6. SSE refresh for activity freshness

- [x] 6.1 Update `pollLoop` to always publish an SSE event on every tick (not just when the session list changes), so activity timestamps and pulse states refresh in the browser every 5 seconds

## 7. Verification

- [x] 7.1 `go build ./...` and `go vet ./...` pass
- [x] 7.2 Docker image builds and dashboard starts
- [x] 7.3 Session card shows relative timestamp that updates every 5 seconds
- [x] 7.4 Pulse animation appears on cards with recent output, disappears after idle
- [x] 7.5 Rename via UI button updates sidebar card and tab header
- [x] 7.6 Rename via `curl -X PUT` works
- [x] 7.7 Clearing name (empty string) reverts to directory basename
