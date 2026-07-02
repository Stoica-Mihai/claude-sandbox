## ADDED Requirements

### Requirement: Create a new directory under /workspace
The system SHALL expose a `POST /api/directories` endpoint that creates a single new directory beneath a parent path inside `/workspace`, for use by the directory picker when starting a session in a new project. The JSON request body SHALL be `{ "path": <parent-relative-path>, "name": <new-folder-name>, "gitInit": <bool> }`, where `path` is the currently browsed directory relative to `/workspace` (empty string = the workspace root) and `name` is the folder to create.

The endpoint SHALL validate `name` against the regular expression `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` — a single path segment of 1–64 characters, beginning with an alphanumeric character (no leading dot or dash), containing only ASCII letters, digits, `.`, `_`, and `-`, and therefore no path separators and no `..`. This regex is the single authoritative validation gate for the name; the endpoint SHALL NOT apply any additional name-specific rejection beyond it.

The endpoint SHALL resolve and prefix-check the **parent** path using the same logic as `GET /api/directories`: it SHALL compute `filepath.Join(workspaceRoot, path)`, take `filepath.Abs`, and require the result to be `workspaceRoot` itself or a path beneath it. Before creating the directory, it SHALL perform an explicit parent-existence check (`os.Stat` on the resolved parent, requiring a directory) so that a missing parent is classified as a client error and never reaches the create call.

On a valid request the endpoint SHALL create the directory with `os.Mkdir` at mode `0o755` (not `os.MkdirAll` — the parent MUST already exist). When `gitInit` is true, it SHALL then run `git -C <new-dir> init`. The umask-adjusted on-disk mode is acceptable; the endpoint SHALL NOT issue a follow-up `os.Chmod`.

The endpoint SHALL respond as follows:
- **201 Created** on success (directory created; `git init` succeeded or was not requested).
- **201 Created with a `warning` field** when the directory was created but `gitInit` was requested and `git init` failed. The directory SHALL be kept (not rolled back), and the `warning` field SHALL carry a human-readable notice.
- **400 Bad Request** when the name fails the regex (`"Invalid name"`), when the resolved parent path is not under `/workspace` (`"invalid path"`), or when the parent path does not exist / is not a directory (`"directory not found"`). The `"invalid path"` and `"directory not found"` messages SHALL be byte-identical to those returned by `GET /api/directories`.
- **409 Conflict** when the target directory already exists (`os.Mkdir` returns an `os.ErrExist` error), with the message `"Folder already exists"`.
- **500 Internal Server Error** for any other `os.Mkdir` failure, with a plain message (`"failed to create directory"`).

#### Scenario: Create a folder at the workspace root
- **WHEN** a POST request is made to `/api/directories` with body `{"path":"","name":"relay-visualizer","gitInit":false}` and `/workspace/relay-visualizer` does not exist
- **THEN** the system SHALL create `/workspace/relay-visualizer` with `os.Mkdir` at mode `0o755` and respond with HTTP 201

#### Scenario: Create a folder inside a browsed subdirectory
- **WHEN** a POST request is made to `/api/directories` with body `{"path":"experiments","name":"new-idea","gitInit":true}` and `/workspace/experiments` exists
- **THEN** the system SHALL create `/workspace/experiments/new-idea`, run `git -C /workspace/experiments/new-idea init`, and respond with HTTP 201

#### Scenario: git init fails after the folder is created
- **WHEN** a POST request creates the directory successfully but the subsequent `git init` fails
- **THEN** the system SHALL keep the created directory (no rollback) and respond with HTTP 201 including a `warning` field describing the git-init failure

#### Scenario: Invalid folder name
- **WHEN** a POST request is made to `/api/directories` with a `name` that fails `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` (for example `".."`, `"a/b"`, `".hidden"`, an empty string, or a 65-character name)
- **THEN** the system SHALL reject the request before any filesystem call and respond with HTTP 400 and the error `"Invalid name"`

#### Scenario: Parent path escapes the workspace
- **WHEN** a POST request is made to `/api/directories` with a `path` whose resolved absolute form is not `workspaceRoot` and not beneath it (for example `path="../../etc"`)
- **THEN** the system SHALL respond with HTTP 400 and the error `"invalid path"`

#### Scenario: Parent directory does not exist
- **WHEN** a POST request is made to `/api/directories` with a regex-valid `name` and a `path` that resolves under `/workspace` but does not exist on disk (for example `path="nope-does-not-exist"`)
- **THEN** the explicit pre-create parent-existence check SHALL reject the request with HTTP 400 and the error `"directory not found"`, without attempting to create anything

#### Scenario: Target folder already exists
- **WHEN** a POST request is made to `/api/directories` for a `name` whose target directory already exists under the resolved parent
- **THEN** `os.Mkdir` SHALL return an `os.ErrExist` error and the system SHALL respond with HTTP 409 and the error `"Folder already exists"`

### Requirement: Frontend mirrors the create-directory route as a per-route proxy
The frontend service SHALL register `POST /api/directories` as an explicit per-route proxy to the backend, consistent with how every other `/api/*` route is mirrored in the frontend. The proxy SHALL forward the JSON request body and pass the backend's status code and response body through unchanged so the client observes the backend's authoritative `201`/`201-with-warning`/`400`/`409`/`500` result.

#### Scenario: Frontend forwards a create request to the backend
- **WHEN** the dashboard client sends `POST /api/directories` with a JSON body to the frontend
- **THEN** the frontend SHALL forward the request to the backend's `POST /api/directories` and relay the backend's status code and body back to the client without altering them
