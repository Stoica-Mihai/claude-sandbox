# Tasks: Resume past sessions per folder

## 1. Session index + paths (backend/sessionindex.go, backend/paths.go)
- [x] 1.1 `paths.go`: add `SessionID string` to `sessionMeta`; add `newUUID()` (UUIDv4) helper; add `sessionIndexPath()` = `$CLAUDE_CONFIG_DIR/dashboard-sessions.json` (reuse `claudeConfigDir()` resolution)
- [x] 1.2 New `backend/sessionindex.go`: `SessionIndex` (mutex-guarded), entry `{cwd, created, name}` keyed by uuid; `load`/`save` (atomic temp+rename, 0600); `add(uuid,cwd,created)`; `setName(uuid,name)`; `name(uuid) string`; `listByCwd(cwd) []entry sorted by created desc`
- [x] 1.3 Load the index once at startup; expose it via the SessionManager

## 2. Spawn / resume / rename / history (backend/session.go)
- [x] 2.1 `Spawn`: generate uuid, build inner script `echo $$ > <pid>; exec claude --session-id <uuid> --dangerously-skip-permissions`, store uuid in meta sidecar, add `{uuid,cwd,created}` to the index
- [x] 2.2 Add `Resume(uuid)`: look up the index entry's cwd, spawn a dtach session running `claude --resume <uuid> --dangerously-skip-permissions` in that cwd (reuse uuid, no new index entry); return session name
- [x] 2.3 Replace `session-names.json` storage with the index: `SetSessionName` resolves the live session's uuid (from meta sidecar) and calls `index.setName`; remove `loadSessionNames`/`saveSessionNames`/`sessionNames` map
- [x] 2.4 `enrichSessions`: resolve each live session's uuid from its meta and set `DisplayName` from `index.name(uuid)` (fallback dir basename)
- [x] 2.5 Add `History(cwd) []SessionHistoryEntry` returning `index.listByCwd(cwd)`

## 3. Backend API (backend/handlers.go)
- [x] 3.1 `handleSpawn`: accept optional `resume` in the JSON body; if set call `sm.Resume(uuid)`, else `sm.Spawn(cwd)`
- [x] 3.2 Add `handleHistory` for `GET /api/sessions/history?cwd=` → JSON array from `sm.History(cwd)`; register the route (before `{terminalId}` patterns as needed)
- [x] 3.3 Keep `handleSetSessionName` (now routes into the index via `sm.SetSessionName`)

## 4. Frontend proxy (frontend/handlers.go)
- [x] 4.1 Register `GET /api/sessions/history` → proxy to backend (httpProxy)

## 5. Frontend modal (frontend/web/templates/layout.html, fragments/directory-picker.html)
- [x] 5.1 Rework the directory-picker fragment: breadcrumb + folder rows (navigate), a selectable "Start a new session" row, and a "Previous sessions" list placeholder populated from `/api/sessions/history`
- [x] 5.2 Modal footer: permanent Cancel + single primary button (Launch/Resume) relabeled in place; selection via background color only

## 6. Frontend JS (frontend/web/static/js/views.js)
- [x] 6.1 Folder navigation + fetch `/api/sessions/history?cwd=` on entering a folder; render previous sessions (name if set, else relative time + short uuid)
- [x] 6.2 Select-then-confirm state: default-select "new" on folder entry; selecting a session switches primary to Resume; root disables primary
- [x] 6.3 Submit: POST `/api/sessions` with `{cwd}` (new) or `{cwd,resume:<uuid>}` (resume); on success open the session (existing flow)

## 7. Docs
- [x] 7.1 Update CLAUDE.md (session identity = claude `--session-id` uuid; persisted index; resume) and README if relevant

## 8. Verification
- [x] 8.1 `go build`/`go vet`/`go test -race` pass in `backend/` and `frontend/`
- [x] 8.2 Spawn → uuid in meta + index entry; `--session-id` present
- [x] 8.3 `GET /api/sessions/history?cwd=` returns spawned sessions for that cwd
- [x] 8.4 Rename → name in index, shows in sidebar + history; persists across backend restart
- [x] 8.5 Resume spawns `claude --resume <uuid>` in the right cwd
- [x] 8.6 Modal: navigate, default Launch, select session → Resume, Cancel always present
