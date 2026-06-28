# settings-editor Specification

## Purpose
Read and write a whitelisted preference subset of the container's Claude settings (`container-settings.json`) from the dashboard, persisting changes to the source file and refreshing the live `settings.json` so they apply to newly spawned sessions.

## Requirements
### Requirement: Read editable settings
The backend SHALL expose `GET /api/settings` returning only the editable preference subset of the container's Claude settings — `model`, `effortLevel`, `alwaysThinkingEnabled`, `language`, `advisorModel` — read from `container-settings.json`. Keys that are absent SHALL be returned as empty/zero values. The endpoint SHALL NOT return non-editable keys (`enabledPlugins`, `hooks`, `extraKnownMarketplaces`, `env`, `skipDangerousModePermissionPrompt`). The frontend SHALL reverse-proxy this route to the backend.

#### Scenario: Fetch current preferences
- **WHEN** the dashboard requests `GET /api/settings`
- **THEN** the response SHALL be JSON containing only `model`, `effortLevel`, `alwaysThinkingEnabled`, `language`, and `advisorModel` reflecting the current `container-settings.json`

#### Scenario: Missing optional key
- **WHEN** `container-settings.json` has no `advisorModel` key
- **THEN** `GET /api/settings` SHALL return `advisorModel` as an empty string (not error)

### Requirement: Persist editable settings with a whitelist and merge
The backend SHALL expose `PUT /api/settings` that accepts the editable subset, validates it, merges only the whitelisted fields into the existing `container-settings.json`, and persists the result. All non-whitelisted keys in `container-settings.json` SHALL be preserved exactly. The write SHALL prefer an atomic temp-file + rename (mode `0600`); because `container-settings.json` is a single-file bind mount that cannot be replaced by rename, the write SHALL fall back to an in-place overwrite through the mount. After writing the source file, the backend SHALL refresh `$CLAUDE_CONFIG_DIR/settings.json` with the same merged content so a newly spawned session uses it. The `container-settings.json` path SHALL be resolvable via `CONTAINER_SETTINGS_PATH` (default `/home/claude/container-settings.json`).

#### Scenario: Save preserves non-editable keys
- **WHEN** a client `PUT`s `{model:"sonnet"}` and `container-settings.json` also contains `enabledPlugins`, `hooks`, and `env`
- **THEN** the saved file SHALL set `model` to `sonnet` and SHALL retain `enabledPlugins`, `hooks`, and `env` unchanged

#### Scenario: Source and live settings both updated
- **WHEN** a valid `PUT /api/settings` succeeds
- **THEN** both `container-settings.json` and `$CLAUDE_CONFIG_DIR/settings.json` SHALL contain the merged result

#### Scenario: Durable write with bind-mount fallback
- **WHEN** the backend persists settings
- **THEN** it SHALL attempt an atomic temp-file + rename (mode `0600`), and SHALL fall back to an in-place overwrite when the target is a bind-mounted file that rename cannot replace

### Requirement: Reject invalid setting values
`PUT /api/settings` SHALL validate values against an allowlist and reject invalid input with HTTP 400 without modifying any file. `model` SHALL be one of a known set of main-model aliases (e.g. `opus[1m]`, `opus`, `sonnet`, `haiku`); `advisorModel` SHALL be empty (off) or a canonical Claude model id (e.g. `claude-opus-4-8`) — the value shape the `/advisor` control writes, not a main-model alias; `effortLevel` SHALL be one of `low`, `medium`, `high`, `xhigh`, `max`; `alwaysThinkingEnabled` SHALL be a boolean; `language` SHALL be a short non-control string. The allowlists SHALL be single constants that are easy to extend.

#### Scenario: Invalid model rejected
- **WHEN** a client `PUT`s `{model:"gpt-4"}`
- **THEN** the backend SHALL respond `400` and SHALL NOT modify `container-settings.json` or `settings.json`

#### Scenario: Invalid effort rejected
- **WHEN** a client `PUT`s `{effortLevel:"extreme"}`
- **THEN** the backend SHALL respond `400` and leave both files unchanged

### Requirement: Settings apply to new sessions only
Persisted settings SHALL take effect for sessions spawned after the save (claude reads settings at startup). The change SHALL NOT restart the container and SHALL NOT alter any running session. The UI SHALL communicate that saved changes apply to new sessions.

#### Scenario: Running session unaffected
- **WHEN** a session is running and the user saves new settings
- **THEN** the running session SHALL continue with its original settings and a newly spawned session SHALL use the saved settings

#### Scenario: UI states the scope
- **WHEN** the settings modal is open
- **THEN** it SHALL display that changes apply to new sessions

### Requirement: Settings editor UI in the dashboard
The dashboard SHALL provide a settings control (a gear button in the header) that opens a modal styled with the Futurism design system. The modal SHALL present the editable fields using on-brand controls (custom `.sel` dropdowns for `model`, `advisorModel`, and `effortLevel`; an input for `language`; a `.toggle` for `alwaysThinkingEnabled`), populate them via `GET /api/settings` when opened, and save via `PUT /api/settings` with a visible success/failure state. The modal SHALL be usable on mobile viewports.

#### Scenario: Open and populate
- **WHEN** the user clicks the settings gear
- **THEN** the modal SHALL open and its controls SHALL reflect the values from `GET /api/settings`

#### Scenario: Save feedback
- **WHEN** the user saves and the `PUT` succeeds
- **THEN** the Save button SHALL show a success state and the modal SHALL close; on failure it SHALL show an error state and stay open

#### Scenario: Mobile usable
- **WHEN** the modal is viewed at a mobile width (<768px)
- **THEN** all controls SHALL be reachable and the modal SHALL not overflow the viewport horizontally
