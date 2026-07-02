## 1. Shared wire types

- [x] 1.1 In `shared/types.go` (package `api`) add `CreateDirectoryRequest{ Path string \`json:"path"\`; Name string \`json:"name"\`; GitInit bool \`json:"gitInit"\` }` per design Decision 5.
- [x] 1.2 In `shared/types.go` add `CreateDirectoryResponse{ Path string \`json:"path"\`; Warning string \`json:"warning,omitempty"\` }` per design Decision 5.
- [x] 1.3 `go build ./...` in `shared/` (and confirm both services still compile against the module).

## 2. Backend: create-directory handler

- [x] 2.1 In `backend/handlers.go` add a package-level `var newDirNameRe = regexp.MustCompile(\`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$\`)`; add `os/exec` and `regexp` to the import block (`errors`, `os`, `path/filepath` already present).
- [x] 2.2 Add `handleCreateDirectory` immediately after `handleDirectories`: decode the JSON body into `api.CreateDirectoryRequest`; on decode error respond 400 `"invalid request body"`.
- [x] 2.3 Validate `req.Name` with `newDirNameRe`; on mismatch respond 400 `"Invalid name"` before any filesystem call (Decision 4).
- [x] 2.4 Resolve the PARENT exactly like the GET handler (Decision 1): `parent := filepath.Join(workspaceRoot, req.Path)`; `absParent, err := filepath.Abs(parent)`; if `err != nil || !underWorkspace(absParent)` respond 400 `"invalid path"` (byte-identical to the GET handler's message).
- [x] 2.5 Explicit pre-`Mkdir` parent-existence check: `info, err := os.Stat(absParent)`; if `err != nil || !info.IsDir()` respond 400 `"directory not found"` (byte-identical to the GET handler's message).
- [x] 2.6 Create the dir: `newDir := filepath.Join(absParent, req.Name)`; `err := os.Mkdir(newDir, 0o755)` (no `Chmod`, Decision 3); if `errors.Is(err, os.ErrExist)` respond 409 `"Folder already exists"`; else if `err != nil` respond 500 `"failed to create directory"`.
- [x] 2.7 If `req.GitInit`, run `exec.Command("git", "-C", newDir, "init")`; on failure keep the folder (no rollback), log combined output via slog, and respond 201 with `CreateDirectoryResponse{ Path: <rel>, Warning: "git init failed" }` (Decision 8). On success/absent git, respond 201 with `Warning` empty.
- [x] 2.8 Compute the returned `Path` as `filepath.Rel(workspaceRoot, newDir)` (same as the GET handler's `currentRel`); write the 201 body with `writeJSON`.
- [x] 2.9 Register the route: `mux.HandleFunc("POST /api/directories", s.handleCreateDirectory)` next to the existing `GET /api/directories` line in `NewServer` (`backend/handlers.go`).

## 3. Backend: tests

- [x] 3.1 In `backend/handlers_test.go` add a table-driven test wiring `POST /api/directories` on an `http.NewServeMux` with `httptest.NewRecorder`, matching the existing test style (JSON body assertions), for invalid names → 400 `"Invalid name"`: cases `".."`, `"a/b"`, `".hidden"`, `""`, a 65-char name, and a name containing a separator (Decision 2a).
- [x] 3.2 Add a case: valid name + parent-gone → 400 `"directory not found"` using `path` that resolves under `/workspace` but does not exist (e.g. `path="nope-does-not-exist"`) (Decision 2b).
- [x] 3.3 Add a case: traversal escaping the root → 400 `"invalid path"` (e.g. `path="../../etc"`) (Decision 2c).
- [x] 3.4 Add a comment in the test noting the coverage gap: the 409 (EEXIST) and 201 (success) branches require a writable `/workspace` (a hardcoded const, not injectable — Decision 6) and are covered by the client-side duplicate pre-check plus the container smoke path, not by unit tests.
- [x] 3.5 `cd backend && go test ./... -race` passes.

## 4. Frontend: proxy route

- [x] 4.1 In `frontend/handlers.go` add `handleCreateDirectoryProxy` delegating to the existing generic `httpProxy(w, r, s.backendURL)` (Decision 7) — the passthrough used by history/upload/settings.
- [x] 4.2 Register `mux.HandleFunc("POST /api/directories", s.handleCreateDirectoryProxy)` in `NewServer` (`frontend/handlers.go`), leaving the existing `GET /api/directories` template-render route intact.

## 5. Frontend: picker fragment markup

- [x] 5.1 In `frontend/web/templates/fragments/directory-picker.html`, after the `#dp-folders` block (and before `#session-actions`), add the idle `.newrow` button (label `+ NEW PROJECT…`, accent `.plus`) carrying `data-dp-path="{{.Path}}"` and `data-dp-full="{{.FullPath}}"` (Decision 10), wired to open the editor.
- [x] 5.2 Add the hidden `.newedit` editor markup per the mockup: autocomplete-off/spellcheck-off text input, an inline `.errline` (hidden by default, `role="status"`), an `.erow` with the default-checked `git init` checkbox and CANCEL/CREATE buttons, and the `.hintline` `Enter to create · Esc to cancel`.

## 6. Frontend: picker JS

- [x] 6.1 In `frontend/web/static/js/views.js` add `openEditor()` / `closeEditor()` (hide/show `.newrow`/`.newedit`, focus the input on open, clear input + error state on close), reading the browse path from the newrow's `data-dp-path` / `data-dp-full` (Decision 10).
- [x] 6.2 Add `createProject()`: client pre-check with `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` (error `"Invalid name"`) then a duplicate check against `#dp-folders .fnm` text (error `"Folder already exists"`); on failure show the inline `.errline` + `input.err-flash` and do not send (Decision 9).
- [x] 6.3 On pass, `fetch('/api/directories', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ path, name, gitInit })})`.
- [x] 6.4 On `201`: spawn directly — set `#dir-picker-cwd` to `fullPath + '/' + name`, clear `#dir-picker-resume`, `requestSubmit()` the spawn form (fresh folder has nothing to resume; the `X-Terminal-Id` handler closes the modal and opens the tab). If the parsed body's `warning` is non-empty, show `"created, git init failed"` as a kit toast (outlives the closing modal). `dpSelectFolder` hides the newrow/editor for the plain folder-click selected state; `dpResetBrowse` restores them.
- [x] 6.5 On `400`/`409`: read the JSON `error` and show it inline (error line + `input.err-flash`), keeping the editor open; on any other/failed response show a generic inline failure and keep the editor open.
- [x] 6.6 Wire keydown on the name input: Enter → `preventDefault()` + `createProject()`; Esc → `preventDefault()` + `stopPropagation()` + `closeEditor()` (preventDefault blocks the native `<dialog>` Esc-cancel default action, so the modal stays open). Wire `input` → clear the inline error + `input.err-flash` on the next keystroke.
- [x] 6.7 Confirm the existing `htmx:afterSwap` `#dir-picker` handler already resets the affordance on drill/breadcrumb nav (the fragment re-renders in idle state) — intended per the dashboard-ui spec; no extra reset code needed.

## 7. Frontend: styles

- [x] 7.1 In `frontend/web/static/css/app.css` add `.newrow` (muted, accent `.plus`, top border, hover/focus-visible) and `.newedit` (`--field` background, top border, input full-width, `.erow`/`.gitinit`/`.grp`/`.hintline`/`.errline`) copied from the approved `scratchpad/new-project-mockup.html`, kit-conformant (square corners, 2px borders, single accent).
- [x] 7.2 Add an entry to the `app.css` override ledger for each intentional divergence from the Futurism kit introduced by `.newrow`/`.newedit`.

## 8. Build, verify, and record the coverage gap

- [x] 8.1 Build both services (`go build ./...` in `backend/` and `frontend/`) — no compile errors.
- [x] 8.2 `cd backend && go test ./... -race` — clean.
- [x] 8.3 Container smoke check (covers the un-unit-tested 201 + 409 branches, Decision 2): with a running stack, create a folder at the root and inside a subfolder (verify 201, folder on disk under `/workspace`, `.git` present when git init requested), retry the same name (verify 409 `"Folder already exists"` surfaced inline), and confirm the new folder auto-selects so LAUNCH is the next click.
