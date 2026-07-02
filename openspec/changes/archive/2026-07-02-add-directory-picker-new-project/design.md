## Context

The dashboard directory picker (`GET /api/directories`, rendered into `frontend/web/templates/fragments/directory-picker.html`, driven by `frontend/web/static/js/views.js`) browses `/workspace` and lets the user pick a folder to spawn a session in. There is no way to create a folder from the UI, and direct CLI use is disabled in the sandbox, so a new project requires a shell outside the dashboard. This change adds folder creation to the picker.

Relevant existing code this design builds on directly:

- `backend/handlers.go:208-266` — `handleDirectories` (the GET handler). Its resolve+prefix pattern is: `target := filepath.Join(workspaceRoot, subpath)` → `absTarget, err := filepath.Abs(target)` → `if err != nil || !underWorkspace(absTarget)` → 400 `"invalid path"`; then `info, err := os.Stat(absTarget)` → `if err != nil || !info.IsDir()` → 400 `"directory not found"`. The POST handler reuses this **verbatim on the parent path**.
- `backend/session.go:30` — `workspaceRoot = "/workspace"` (const). `backend/session.go:254` — `underWorkspace(abs)` (`abs == workspaceRoot || strings.HasPrefix(abs, workspaceRoot+"/")`).
- `backend/handlers.go:323` — precedent for `os.MkdirAll(sessionDir, 0o755)` with no follow-up `Chmod`.
- `backend/handlers.go:52-64` — the backend mux; `frontend/handlers.go:52-63` — the frontend per-route proxy table; `frontend/handlers.go` `httpProxy` passthrough used by history/settings/upload/healthz.
- `shared/types.go` — `api` package, the shared wire contract (`DirectoryData`, `Breadcrumb`, `DisplaySession`).
- `frontend/web/static/js/views.js:482` — `dpSelectFolder(path, name)`, the click path that hides the folder list, appends the breadcrumb crumb, builds `session-actions`, and defaults to "start new". `views.js:400-415` — the `htmx:afterSwap` handler that calls `dpResetBrowse()` when `#dir-picker` is re-rendered.
- `frontend/web/static/css/app.css:205-214` — `.folders`/`.frow` styles; `app.css:315` — `input.err-flash{outline:3px solid var(--accent)}`.
- Approved visual mockup: `scratchpad/new-project-mockup.html` (defines `.newrow`, `.newedit`, and the three states).

## Goals / Non-Goals

**Goals:**
- Create a single new directory under a browsed parent inside `/workspace` from the picker, with optional `git init`, and auto-select it for immediate launch.
- Keep the server the single authoritative validator; the client pre-check is pure UX.
- Reuse the GET handler's exact resolve+prefix+stat logic so POST and GET agree on what "under /workspace" and "not found" mean, byte-for-byte in the error messages.
- Keep the wire contract in the shared `claude-sandbox-api` module.

**Non-Goals:**
- No recursive/multi-segment creation (`os.Mkdir`, not `MkdirAll`; the parent must already exist).
- No rename/delete/move of directories from the picker.
- No configurable git identity, initial commit, remote, or branch — just `git init`.
- No change to `workspaceRoot` (stays a hardcoded const; not made injectable — see Decision 6).
- No `os.Chmod` after `Mkdir` (umask-adjusted `0o755` is accepted).

## Decisions

These carry forward the four FINAL decisions from the change request verbatim, and add the remaining open choices this change forces. Decisions 5–9 are made **here, once**; all later artifacts (tasks, and the implementation) MUST match them.

### Decision 1 (FINAL): Parent-gone is a 400 via an explicit pre-Mkdir check
`handleCreateDirectory` resolves the **parent** exactly like the GET handler: `filepath.Join(workspaceRoot, req.Path)` → `filepath.Abs` → `underWorkspace`. If `filepath.Abs` errors or `!underWorkspace(parent)` → 400 `"invalid path"`. Then `os.Stat(parent)`; if `err != nil || !info.IsDir()` → 400 `"directory not found"` (byte-identical to the GET handler's messages). This runs **before** `os.Mkdir`, so a missing parent (ENOENT) is classified 400, never 500, and never reaches `Mkdir`. The `Mkdir` call itself only distinguishes `EEXIST` → 409 (via `errors.Is(err, os.ErrExist)`) from any other error → 500. We do **not** rely on `Mkdir`'s ENOENT to signal parent-gone.

### Decision 2 (FINAL): Test only the branches reachable without a writable /workspace
`workspaceRoot` stays a const (Decision 6). Add table-driven cases to `backend/handlers_test.go` in the existing style (`http.NewServeMux` + `httptest.NewRecorder`, JSON-body assertions): (a) invalid names → 400 `"Invalid name"` (`..`, `a/b`, `.hidden`, ``, a 65-char name, a name with a separator); (b) valid-name-but-parent-gone → 400 `"directory not found"` (e.g. `path="nope-does-not-exist"`); (c) traversal escaping the root → 400 `"invalid path"`. The **409 (EEXIST)** and **201 (success)** branches need a writable path under the real `/workspace` const and are therefore **not** unit-tested; this gap is called out in tasks and covered by the client-side duplicate pre-check plus the container smoke path.

### Decision 3 (FINAL): `os.Mkdir(dir, 0o755)`, no `Chmod`
Umask-adjusted mode is acceptable. The backend container sets no umask (default 022) and `0o755 & ^0o022 == 0o755`, so the on-disk mode is `0755` as specified. This matches the `os.MkdirAll(sessionDir, 0o755)` precedent at `backend/handlers.go:323`. Pass the literal `0o755`; add no `os.Chmod`.

### Decision 4 (FINAL): The regex is the only name gate
The single validation gate is `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, applied identically on the client (UX) and server (authoritative), plus the resolve+`underWorkspace` prefix check and the parent-existence check. The mandatory non-dot leading character already excludes any folder the GET listing would hide (it skips leading-`.` names at `backend/handlers.go:235`), and `..` is rejected outright, so no regex-valid name is treated specially and no extra server-side name rejection is added.

### Decision 5 (NEW): Shared wire types and their JSON shape
Add to `shared/types.go` (package `api`):

```go
// CreateDirectoryRequest is the POST /api/directories body: create <Name>
// under /workspace/<Path> (Path empty = the workspace root).
type CreateDirectoryRequest struct {
    Path    string `json:"path"`
    Name    string `json:"name"`
    GitInit bool   `json:"gitInit"`
}

// CreateDirectoryResponse is the POST /api/directories success body. Warning is
// non-empty only when the folder was created but `git init` failed (still 201).
type CreateDirectoryResponse struct {
    Path    string `json:"path"`               // new folder relative to /workspace
    Warning string `json:"warning,omitempty"`
}
```

Rationale: mirrors the existing convention in `shared/types.go` (both services import `claude-sandbox-api`; the GET side already returns `api.DirectoryData`). The request field `gitInit` matches the change request's body spec and the mockup's `gitInit` note. `Warning` uses `omitempty` so a plain 201 carries no warning key. Errors continue to use the existing `{"error": "..."}` shape produced by `writeErr` — we do **not** introduce a new error envelope.

Alternative considered: per-service structs. Rejected — the project's memory and CLAUDE.md both mandate the shared module as the single source of truth for the wire contract.

### Decision 6 (NEW, FINAL per request): `workspaceRoot` stays a const
Do not make `workspaceRoot` injectable. The existing suite deliberately keeps it a const and asserts against `/workspace/...` literals (`session_test.go`, `handlers_test.go`), even though it does swap other globals (`sockDir`/`metaDir` via `setSessionDirs`). Introducing a var/override solely for a happy-path test would diverge from the established pattern for no coverage gain on the security-relevant validation branches, which are the ones we do test.

### Decision 7 (NEW): Frontend proxy uses the generic `httpProxy` passthrough
Register `mux.HandleFunc("POST /api/directories", s.handleCreateDirectoryProxy)` in `frontend/handlers.go`, where the handler is a one-liner delegating to the existing generic `httpProxy(w, r, s.backendURL)` — the same passthrough used by `handleHistoryProxy`, `handleUploadProxy`, and `handleSettingsProxy`.

Rationale: the **GET** `/api/directories` frontend handler decodes JSON and renders the `directory-picker` HTML template because HTMX swaps that fragment. The **POST** response, by contrast, is consumed as JSON by `views.js` (`fetch` → read status + body), never rendered as a template, so the correct mirror is a status/body passthrough, not a template render. `httpProxy` already forwards the method, body, status, and body verbatim, which is exactly what the spec's "relay the backend's status code and body unchanged" requires. This keeps GET (template render) and POST (passthrough) as two distinct handlers on the same path, consistent with how settings uses one passthrough handler for both GET and PUT.

Alternative considered: a bespoke proxy handler that re-decodes and re-encodes the JSON. Rejected — it adds a failure mode (decode error masking the backend's real status) for no benefit; the client needs the raw status and body.

### Decision 8 (NEW): Handler placement, imports, and git invocation
- Place `handleCreateDirectory` in `backend/handlers.go` immediately after `handleDirectories`, and register `mux.HandleFunc("POST /api/directories", s.handleCreateDirectory)` next to the existing `GET /api/directories` line.
- Decode the body with `json.NewDecoder(r.Body).Decode(&req)`; on a decode error return 400 with a plain message (`writeErr(w, 400, "invalid request body")`). Compile the name regex once as a package-level `var newDirNameRe = regexp.MustCompile(...)`.
- Run git via `exec.Command("git", "-C", dir, "init")` and treat a non-nil `Run()` error (or non-zero exit) as the git-init-failure case: keep the folder, respond 201 with `Warning` set. Capture combined output into the slog log line but keep the client `warning` message human-readable and fixed ("git init failed"). This adds `os/exec`, `regexp`, and (already imported) `errors`/`os`/`path/filepath` to `backend/handlers.go`.
- The new folder's relative path returned in `CreateDirectoryResponse.Path` is computed the same way the GET handler computes `currentRel` (`filepath.Rel(workspaceRoot, absNewDir)`), so the client can pass it straight back into the listing/select path.

### Decision 9 (NEW): Frontend create flow and message strings
In `views.js`, model the editor as a small in-fragment state machine that mirrors the mockup's `openEditor`/`closeEditor`/`createProject` functions but talks to the real endpoint:
- `openEditor()` hides `.newrow`, shows `.newedit`, focuses the input.
- `closeEditor()` restores the idle row and clears input + error state.
- `createProject()` reads name + `gitInit`, runs the client pre-check (regex, then duplicate against `#dp-folders .fnm` text), and on pass `fetch('/api/directories', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({path, name, gitInit})})` where `path` is the current browse path (read from `#dir-picker-cwd`'s browse context — the hidden `path` the fragment was rendered with; see Decision 10).
- On `res.status === 201`: parse the JSON body; spawn directly — set `#dir-picker-cwd` to the new folder's full path, clear `#dir-picker-resume`, and `requestSubmit()` the spawn form. A fresh folder has nothing to resume, so the select+LAUNCH step is skipped; the spawn response's `X-Terminal-Id` `htmx:afterRequest` handler closes the modal and opens the terminal tab, identical to a manual LAUNCH. If the parsed body has a non-empty `warning`, show "created, git init failed" as a kit toast (`.toaster`/`.toast.err`, host built on demand) — an inline notice would die with the closing modal. (`dpSelectFolder` still hides the newrow/editor for the plain folder-click selected state; `dpResetBrowse` restores them.)
- On `res.status === 400 || 409`: read the JSON `error` and show it inline via the same error affordance as the pre-check (error line text + `input.err-flash`), keeping the editor open. On any other/failed response, show a generic inline failure and keep the editor open.
- Exact user-facing strings (fixed, matching the request): row label `"+ NEW PROJECT…"`, hint `"Enter to create · Esc to cancel"`, client errors `"Invalid name"` / `"Folder already exists"`, git-init notice `"created, git init failed"`. The 409 path reuses the server's `"Folder already exists"` message.

### Decision 10 (NEW): How the fragment tells the client the current browse path
The `directory-picker` template already renders the current path into breadcrumbs and drill links (`{{.Path}}` / `{{.FullPath}}`). The newrow/editor need both the relative `Path` (to POST) and `FullPath` (to select). The template SHALL render these onto the newrow element as data attributes (e.g. `data-dp-path="{{.Path}}"` and `data-dp-full="{{.FullPath}}"`), and `views.js` SHALL read them when building the POST body and the post-success `dpSelectFolder(fullPath + '/' + name, name)` call. This keeps the browse path a property of the rendered fragment (so it is always correct for the current depth and resets naturally on re-render) rather than duplicating breadcrumb-parsing logic in JS.

## Risks / Trade-offs

- **[Success (201) and conflict (409) backend branches are not unit-tested]** → Mitigated by Decision 2's validation-branch tests (the security-relevant paths), the client-side duplicate pre-check, and the container smoke path. Making them testable would require injecting `workspaceRoot`, which Decision 6 rejects to preserve the existing test pattern.
- **[Client duplicate pre-check can be stale]** → The picker's `#dp-folders` list is the render-time snapshot; a folder created out-of-band between render and submit would slip past the client check. Mitigated because the server is authoritative: `os.Mkdir` returns EEXIST → 409, which the client surfaces inline. The pre-check is explicitly UX-only.
- **[git init partial state]** → If `git init` fails after a successful `mkdir`, the folder is intentionally kept (201 + warning) rather than rolled back, so the user still gets their folder and can retry git manually. Rolling back a just-created dir on a git failure was considered and rejected as surprising (it would delete a folder the user asked for over a non-fatal problem).
- **[TOCTOU between the parent `os.Stat` and `os.Mkdir`]** → A parent removed in that window makes `Mkdir` fail with ENOENT and fall into the 500 branch rather than the 400 "directory not found" branch. Accepted: the window is tiny, both are error responses, and the pre-check exists to classify the *common* missing-parent case (a stale UI), not to defeat a racing `rmdir`.
- **[Umask assumption]** → `0o755` on disk depends on the container umask staying at the default 022 (Decision 3). If a future non-default umask is introduced the mode only tightens (a safe direction), and no code changes.

## Migration Plan

Additive only — one new endpoint plus additive UI. No data migration, no changes to existing endpoints or stored formats. Deploy is a normal image rebuild of backend + frontend. Rollback is reverting the change: the new route disappears and the picker returns to browse-only; nothing persists that the old code cannot read (created folders are ordinary directories).

## Open Questions

None. All decisions the request left open are resolved in Decisions 5–10 above; the four FINAL decisions from the request are carried verbatim as Decisions 1–4.
