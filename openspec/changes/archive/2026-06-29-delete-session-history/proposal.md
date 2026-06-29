## Why

The dashboard lets users spawn, kill (stop the process of), and resume sessions, but there is no way to permanently delete a conversation. Killed and dead conversations linger forever in each folder's "Previous sessions" resume list, with no path to clear them or reclaim the on-disk transcript files.

## What Changes

- Add a full, irreversible delete of a recorded session (conversation) from the dashboard's resume history. Delete removes **both** the dashboard index entry in `dashboard-sessions.json` **and** claude's transcript file(s) matching `projects/*/<uuid>.jsonl` under `$CLAUDE_CONFIG_DIR`.
- Add backend plumbing keyed by the claude conversation **uuid**: `SessionIndex.remove(uuid)`, a `deleteTranscript(uuid)` glob-and-remove helper, and `SessionManager.DeleteHistory(uuid)` which (1) errors if the uuid is not in the index, (2) kills the matching live dtach session first when one is running, then (3) removes the index entry and (4) deletes the transcript file(s), in that order.
- Add a new HTTP route `DELETE /api/sessions/history/{uuid}` (handler `handleDeleteHistory`) returning 204 on success and 404 for an unknown uuid, then publishing an SSE update for the live sidebar. This is **distinct** from the existing kill route `DELETE /api/sessions/{terminalId}` (keyed by the live dtach/socket name) — both routes coexist.
- Add a per-row delete affordance to the New Session modal's "Previous sessions" resume list **only** (not the live sidebar cards). It uses an inline two-step confirm (Futurism-styled, no native `window.confirm`); confirming issues the DELETE and re-fetches/re-renders that folder's history so the row disappears.
- **BREAKING**: none. The kill route, resume flow, and history endpoint are unchanged in behavior; only additions.

## Capabilities

### New Capabilities
<!-- none — this extends existing session-api and dashboard-ui capabilities -->

### Modified Capabilities
- `session-api`: add a `DELETE /api/sessions/history/{uuid}` requirement (full delete of index entry + transcript, live-kill first, 204/404), distinct from the existing kill route.
- `dashboard-ui`: extend the New Session modal's resume-list requirement so each previous-session row carries an inline two-step delete affordance that removes the conversation and re-renders the list.

## Impact

- Backend (Go): `backend/sessionindex.go`, `backend/paths.go`, `backend/session.go`, `backend/handlers.go`; tests in `backend/sessionindex_test.go` and a `DeleteHistory` test.
- Frontend (vanilla JS): `frontend/web/static/js/views.js` (resume-row builder), `frontend/web/static/css/app.css` (new `.arow-wrap` / `.arow-del` / confirm-cancel styles; `futurism.css` is never edited).
- API surface: one new route; no change to existing routes. SSE/broker behavior is reused for the sidebar only.
- Data: irreversible deletion of transcript `.jsonl` files and index entries.
