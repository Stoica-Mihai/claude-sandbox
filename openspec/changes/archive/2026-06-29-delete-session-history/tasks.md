## 1. Backend — index and paths helpers

- [x] 1.1 In `backend/sessionindex.go`, add `SessionIndex.remove(uuid string)` that locks `mu`, deletes the entry from the in-memory map, and calls `save()` (no-op when the uuid is absent).
- [x] 1.2 In `backend/paths.go`, add `deleteTranscript(uuid string)` that globs `filepath.Join(claudeConfigDir(), "projects", "*", uuid+".jsonl")` (mirroring the existing `hasTranscript` glob) and `os.Remove`s each match; zero matches / already-absent files are not errors.

## 2. Backend — SessionManager.DeleteHistory

- [x] 2.1 In `backend/session.go`, add `SessionManager.DeleteHistory(uuid string) error` that first checks index membership and returns an error if the uuid is absent (no kill, no transcript delete).
- [x] 2.2 In `DeleteHistory`, iterate `discoverSessions()` (NOT `ListSessions()`) and, if an entry's metadata `SessionID == uuid`, call `Kill(s.Name)` to kill that live dtach session first; skip the kill when no live session matches.
- [x] 2.3 In `DeleteHistory`, after the optional kill, call `index.remove(uuid)` then `deleteTranscript(uuid)`, in that order.

## 3. Backend — HTTP route

- [x] 3.1 In `backend/handlers.go`, register `DELETE /api/sessions/history/{uuid}` mapped to a new `handleDeleteHistory`, kept DISTINCT from the existing `DELETE /api/sessions/{terminalId}` kill route (do not merge or break it).
- [x] 3.2 Implement `handleDeleteHistory` to call `sm.DeleteHistory(uuid)`; on the not-in-index error respond HTTP 404, otherwise call `s.broker.Publish()` and respond HTTP 204.

## 4. Backend — tests

- [x] 4.1 In `backend/sessionindex_test.go`, add a test that `remove(uuid)` deletes the entry from the index and persists (the removal survives reload from disk).
- [x] 4.2 Add a `DeleteHistory` test: seed an index entry + a fake `projects/<dir>/<uuid>.jsonl` transcript file, call `DeleteHistory(uuid)`, and assert the entry is gone from the index AND the transcript file is removed.
- [x] 4.3 Add a `DeleteHistory` test asserting that calling it with a uuid not in the index returns an error (and does not delete any transcript).
- [x] 4.4 Run `go test ./...` in `backend/` (with `-race`) and confirm the suite passes.

## 5. Frontend — resume-list delete affordance

- [x] 5.1 In `frontend/web/static/js/views.js`, factor the entries-fetch + Previous-sessions list-render block (~lines 422–455) into a reusable function keyed by the current folder path so it can be re-invoked after a delete.
- [x] 5.2 In the resume-row builder (~line 435), wrap each entry in a non-interactive `div.arow-wrap` (`position: relative`) containing the existing resume `<button class="arow sa-row">` AND a sibling `<button class="arow-del">`; append the wrapper to `#session-actions` instead of the bare button.
- [x] 5.3 Give `.arow-del` a `currentColor` stroke SVG or text glyph (NOT an emoji); its click handler calls `e.stopPropagation()` and `e.preventDefault()` so the row's `dirPickerSetSel('resume', uuid, row)` never fires.
- [x] 5.4 Implement the inline two-step confirm: first click swaps `.arow-del` contents into an accent confirm + ghost cancel pair (Futurism tokens `--accent`, `--muted`/`--line`; no hardcoded hex), leaving the resume button untouched; cancel reverts to idle.
- [x] 5.5 On confirm, issue `fetch('/api/sessions/history/'+uuid, {method:'DELETE'})`; on HTTP 204 re-invoke the factored history-fetch-and-render for the current folder so the row disappears (the re-render is the source of truth — do not rely on SSE).
- [x] 5.6 On a non-204 response, revert the inline confirm to idle and surface a brief transient on-brand failure indication (text/class swap; no `window.alert`/`window.confirm`).

## 6. Frontend — styles

- [x] 6.1 In `frontend/web/static/css/app.css` (never `futurism.css`), add `.arow-wrap`, `.arow-del`, and confirm/cancel styles using existing tokens/classes only — no hardcoded hex, no inline theme colors.

## 7. Verification

- [x] 7.1 Verify the kill route `DELETE /api/sessions/{terminalId}` still works unchanged (keyed by dtach name) alongside the new history-delete route.
- [x] 7.2 Manually verify in the modal: deleting a dead conversation removes its row; deleting a live conversation kills its session first then removes the row; the sidebar cards still show only Kill.
- [x] 7.3 Run `openspec validate delete-session-history` and confirm it is clean.
