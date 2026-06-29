## Context

The dashboard persists a `uuid → {cwd, created, name}` index at `$CLAUDE_CONFIG_DIR/dashboard-sessions.json` (`backend/sessionindex.go`). A folder's resume list is `SessionManager.History(cwd)` = `index.listByCwd` filtered by `hasTranscript`, surfaced via `GET /api/sessions/history?cwd=` and rendered in the New Session modal (`frontend/web/static/js/views.js`, the `.arow`/`.sa-row` rows built around line 435). Sessions can be spawned, killed by dtach name (`DELETE /api/sessions/{terminalId}`), and resumed — but never permanently deleted. Killed/dead conversations stay in the resume list forever and their `projects/*/<uuid>.jsonl` transcripts are never reclaimed.

This change is cross-cutting: backend session/index/path code, a new HTTP route, and frontend JS + CSS. All key decisions were settled during investigation and are recorded below as binding constraints, not open questions.

## Goals / Non-Goals

**Goals:**
- Permanently and irreversibly delete a recorded conversation: both the `dashboard-sessions.json` index entry AND every `projects/*/<uuid>.jsonl` transcript under `$CLAUDE_CONFIG_DIR`.
- Expose delete only from the New Session modal's resume list, keyed by conversation uuid.
- If the conversation is currently live, kill the dtach session first, then delete.
- Keep the existing kill route and resume flow byte-for-byte unchanged.
- On-brand Futurism inline two-step confirm; no native dialogs.

**Non-Goals:**
- No delete control on the live sidebar session cards (they keep Kill).
- No soft-delete / undo / trash — deletion is irreversible.
- No batch/multi-select delete.
- No change to how the resume list is fetched or how `hasTranscript` filtering works.

## Decisions

### D1 — Live conversation is killed before deletion, resolved via discoverSessions()
`Kill(sessionName)` is keyed by the dtach session name (e.g. `claude-a1b2c3d4`), not the conversation uuid. `DeleteHistory(uuid)` therefore iterates `discoverSessions()` (the canonical live scan) and matches the entry whose metadata `SessionID == uuid`; on a match it calls `Kill(s.Name)` before removing the index entry and transcript. `discoverSessions()` is used directly (NOT `ListSessions()`, which adds caching/enrichment and is heavier) so liveness reflects the current scan rather than the 2s cache. `SessionID` is already exposed on `DisplaySession` (json:"-", set from `meta.SessionID`).

Order is fixed: **index-membership check → (optional) Kill live → `index.remove(uuid)` → `deleteTranscript(uuid)`**. The membership check runs first so an unknown uuid never kills a session or deletes a transcript; it maps to HTTP 404.

*Alternative considered:* resolve via `ListSessions()` — rejected as heavier and cache-dependent.

### D2 — New route is distinct from the kill route
`DELETE /api/sessions/history/{uuid}` (handler `handleDeleteHistory`) is registered alongside, and never merged with, the existing `DELETE /api/sessions/{terminalId}`. The two are disambiguated by path segment (`history/{uuid}` vs `{terminalId}`): the history route is keyed by conversation uuid, the kill route by live dtach/socket name. `handleDeleteHistory` calls `sm.DeleteHistory(uuid)`, then `s.broker.Publish()`, returning 204 on success and 404 when `DeleteHistory` reports the uuid is absent from the index.

### D3 — Sibling delete control, not nested
The resume row is created via `document.createElement('button')` with `class="arow sa-row"`, and a button may not contain another interactive control. So each entry is wrapped in a non-interactive `div.arow-wrap` (`position: relative`) holding the existing resume `<button class="arow sa-row">` AND a sibling `<button class="arow-del">`. The wrapper (not the bare button) is appended to `#session-actions`. The delete button's handler calls `e.stopPropagation()` + `e.preventDefault()` so it never fires the row's `dirPickerSetSel('resume', uuid, row)` onclick.

*Alternative considered:* nest the delete control inside the row button — rejected as invalid HTML and would conflict with the row's onclick.

### D4 — Inline two-step confirm, modal re-render is the source of truth
First click on `.arow-del` swaps its contents into an accent confirm + ghost cancel pair (tokens: `--accent` confirm, `--muted`/`--line` ghost cancel; no hardcoded hex), leaving the resume button untouched. Confirm issues `fetch('/api/sessions/history/'+uuid, {method:'DELETE'})`. On 204 the modal re-runs the same history-fetch-and-render used by the row builder for the current folder — this re-render is the authoritative refresh of the modal list. The backend's `broker.Publish()` is for the **sidebar** only (the SSE-driven sidebar does not render the modal's resume list, so relying on it would leave the modal stale). On non-204 the control reverts to idle and shows a brief transient on-brand failure indication; no `window.alert`/`window.confirm`. To support the re-render, the entries-fetch + list-render block in `views.js` (~lines 422–455) is factored into a reusable function keyed by the current folder path.

### D5 — CSS placement
All new styles (`.arow-wrap`, `.arow-del`, the confirm/cancel pair) go in `app.css`. `futurism.css` is the vendored kit and is never edited. Themeable colors come from CSS tokens via classes — no hardcoded hex, no inline theme colors.

### D6 — deleteTranscript mirrors hasTranscript (open decision, settled here)
`deleteTranscript(uuid)` globs `filepath.Join(claudeConfigDir(), "projects", "*", uuid + ".jsonl")` and `os.Remove`s each match, mirroring the existing `hasTranscript` glob. A glob with zero matches or an `os.Remove` on an already-absent path is NOT an error — deletion is best-effort on the filesystem side; the authoritative removal is the index entry. This keeps `DeleteHistory` succeeding (204) even when the transcript was already gone, which is correct for a "remove from history" action.

## Risks / Trade-offs

- [Irreversible data loss — a mistaken delete destroys the transcript] → Mitigated by the required inline two-step confirm before any DELETE is issued; no single-click deletion.
- [Killing a live session as a side effect of "deleting history"] → Intended and specified; the order guarantees the membership check passes first, and only the conversation whose `SessionID == uuid` is killed (by exact dtach name), never a broad kill.
- [Route ambiguity between `history/{uuid}` and `{terminalId}`] → The distinct `history/` path segment prevents collision; the kill route's behavior is asserted unchanged by a spec scenario.
- [Modal list going stale if it relied on SSE] → Avoided by making the explicit history re-fetch/re-render the source of truth (D4); `broker.Publish()` only refreshes the sidebar.
- [Partial failure: index removed but transcript remove fails] → Order puts `index.remove` before `deleteTranscript`; a failed transcript remove still leaves the row gone from history (the user-visible goal). Best-effort transcript removal (D6) avoids surfacing benign "already absent" as an error.

## Migration Plan

Pure additive change — no data migration. Deploy backend (new route + manager method) and frontend (JS + CSS) together. Rollback is removing the route, the manager/index/path additions, and the frontend affordance; existing index entries and transcripts are untouched by rollback.

## Open Questions

None — all decisions (live-kill path, route distinctness, sibling markup, re-render source of truth, CSS placement, best-effort transcript removal) are settled above.
