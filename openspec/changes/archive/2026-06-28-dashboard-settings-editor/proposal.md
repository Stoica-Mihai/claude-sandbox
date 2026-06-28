## Why

The container's Claude config (`container-settings.json`) currently can only be changed by editing a file on the host and restarting. Users want to adjust their global preferences (model, effort, etc.) from the dashboard. Now that `container-settings.json` is gitignored, writing to it from the app produces no git noise — so an in-app editor is clean and safe.

## What Changes

- Add backend endpoints `GET /api/settings` and `PUT /api/settings` that read and persist a **whitelisted preference subset** of `container-settings.json`: `model`, `effortLevel`, `alwaysThinkingEnabled`, `language`, `advisorModel`.
- `PUT` validates against allowed values, **merges** the subset into the existing JSON (every other key — `enabledPlugins`, `hooks`, `extraKnownMarketplaces`, `env`, `skipDangerousModePermissionPrompt` — is preserved untouched), writes `container-settings.json` atomically, then refreshes `$CLAUDE_CONFIG_DIR/settings.json` so the change applies to the next spawned session.
- Changes apply to **new sessions only** (claude reads settings at spawn); running sessions are unaffected and the UI says so. No container restart.
- **BREAKING (deploy)**: the `container-settings.json` bind mount changes from read-only (`:ro`) to read-write so the backend can persist edits.
- Frontend: a gear button in the header opens a Futurism settings modal (`.sel` dropdowns for model/advisorModel/effortLevel, an input for language, a `.toggle` for alwaysThinkingEnabled), populated via `GET` and saved via `PUT`, with an "applies to new sessions" hint and a Save success state. The frontend reverse-proxies `/api/settings` to the backend.

## Capabilities

### New Capabilities
- `settings-editor`: Read/write a whitelisted preference subset of the container's Claude settings from the dashboard, persisted to `container-settings.json` + refreshed into the live `settings.json`, applying to newly spawned sessions.

### Modified Capabilities
<!-- None: no existing requirement's behavior changes. -->

## Impact

- **Backend**: `backend/handlers.go` (2 routes), new `backend/settings.go` (handlers + whitelist + merge/atomic write), `backend/paths.go` (path helpers for `container-settings.json` and `settings.json`).
- **Frontend**: `frontend/handlers.go` (proxy routes), `frontend/web/templates/layout.html` (gear + modal), `frontend/web/static/js/settings.js` (new; modal logic), `layout.html` script include.
- **Config**: `docker-compose.yml` (mount `:ro` → rw); optional `CONTAINER_SETTINGS_PATH` env (default `/home/claude/container-settings.json`).
- **Docs**: CLAUDE.md, README (note the in-app settings editor).
- **No change** to session lifecycle, relay, SSE, resume, rename, or the `entrypoint.sh` seed flow (the editor reuses it: write source + refresh copy).
