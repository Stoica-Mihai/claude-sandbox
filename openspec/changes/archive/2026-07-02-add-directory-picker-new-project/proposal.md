## Why

The NEW SESSION directory picker can only browse and select folders that already exist under `/workspace`. Starting a session in a brand-new project means dropping to a shell outside the dashboard to `mkdir` (and `git init`) first — which is exactly the CLI path the sandbox disables. Users need to create a project folder from the picker itself, in the folder they are currently browsing, and immediately launch a session in it.

## What Changes

- Add a `POST /api/directories` backend endpoint that creates a single new folder under a browsed parent path inside `/workspace`, with an optional `git init`.
  - Request body `{ path, name, gitInit }`; `path` is the current browse dir (may be empty = workspace root), `name` is the new folder, `gitInit` toggles repo init.
  - Name is validated against `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` (single segment; no leading dot/dash, no separators, no `..`).
  - The parent path is resolved and prefix-checked under `/workspace` using the exact same logic as the existing `GET /api/directories` handler, plus an explicit pre-`Mkdir` parent-existence check.
  - Creates the folder with `os.Mkdir` (mode `0o755`, not `MkdirAll` — the parent must already exist), then optionally runs `git -C <dir> init`.
  - Responses: `201` on success; `201` **with a `warning` field** when the folder was created but `git init` failed (folder is kept, not rolled back); `400` for an invalid name, invalid path, or a missing parent; `409` when the folder already exists; `500` for any other creation error.
- Mirror `POST /api/directories` in the frontend as a per-route proxy, like every other `/api/*` route.
- Add the wire types (request + response) to the shared `claude-sandbox-api` module — the single source of truth for backend↔frontend shapes — rather than duplicating them per service.
- Add a "+ NEW PROJECT…" affordance to the directory-picker fragment, pinned under the folder list at **every** browse depth:
  - Clicking the row swaps it in place for an inline editor (autofocused name input, a default-on "git init" checkbox, CANCEL/CREATE; Enter = create, Esc = cancel).
  - Client-side pre-check mirrors the server regex and a duplicate check against the currently listed folders (UX only — the server remains authoritative), surfacing an inline error line plus the existing `input.err-flash` accent outline; the error clears on the next keystroke.
  - On `201`, the listing refreshes and the new folder is auto-selected via the same `dpSelectFolder` path as a folder click, so `session-actions` populate and LAUNCH is the next click. On `201`-with-`warning`, still select but show an inline "created, git init failed" notice.
- Add the picker styles (`.newrow`, `.newedit`) to `app.css` per the approved mockup — kit-conformant (square corners, 2px borders, a single accent), with the divergences recorded in the app.css override ledger.

## Capabilities

### New Capabilities
<!-- None — this extends two existing capabilities. -->

### Modified Capabilities
- `session-api`: Add a `POST /api/directories` requirement (name validation, parent resolution/prefix check, `os.Mkdir` semantics, optional `git init`, and the `400/409/500/201/201-with-warning` response contract), and require the frontend to mirror the route as a per-route proxy.
- `dashboard-ui`: Add a requirement for creating a new project folder from the directory picker (the pinned "+ NEW PROJECT…" row → inline editor at every browse depth, client-side pre-check, and auto-select-on-success including the git-init-warning case).

## Impact

- **Backend:** `backend/handlers.go` (new `handleCreateDirectory`, new `POST /api/directories` mux route), `backend/handlers_test.go` (new table-driven validation tests). Reuses `workspaceRoot` / `underWorkspace` from `backend/session.go` (unchanged).
- **Frontend:** `frontend/handlers.go` (new `POST /api/directories` proxy route), `frontend/web/templates/fragments/directory-picker.html` (the newrow + editor markup), `frontend/web/static/js/views.js` (open/close editor, validate, submit, auto-select), `frontend/web/static/css/app.css` (`.newrow`/`.newedit` + ledger entries).
- **Shared:** `shared/types.go` (`CreateDirectoryRequest`, `CreateDirectoryResponse`).
- **APIs:** one new endpoint (`POST /api/directories`); no changes to existing endpoints. No breaking changes.
- **Dependencies:** none new — `git` is already in the backend image.
