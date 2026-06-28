## 1. Backend — settings endpoints

- [x] 1.1 In `backend/paths.go`, add `containerSettingsPath()` (returns `CONTAINER_SETTINGS_PATH` env, default `/home/claude/container-settings.json`) and `settingsJSONPath()` (= `filepath.Join(claudeConfigDir(), "settings.json")`).
- [x] 1.2 Create `backend/settings.go`: define the allowlist constants (`allowedModels = ["opus[1m]","opus","sonnet","haiku"]`, effort levels `["low","medium","high"]`), an `editableSettings` struct (`model, effortLevel, alwaysThinkingEnabled, language, advisorModel` with json tags), and `handleGetSettings` — read `containerSettingsPath()` into `map[string]any`, project only the editable keys into the struct, `writeJSON` it (missing keys → zero values; never emit non-editable keys).
- [x] 1.3 In `backend/settings.go`, add `handlePutSettings`: decode the editable subset; validate (`model` and `advisorModel` in allowedModels, advisorModel may be ""; `effortLevel` in the set; `language` short, no control chars; `alwaysThinkingEnabled` bool) → 400 on any violation without touching files; read existing `container-settings.json` into `map[string]any`, overlay ONLY the whitelisted keys, marshal indented; atomic-write (temp in same dir + `os.Rename`, `0600`) to `containerSettingsPath()`; then write the same bytes to `settingsJSONPath()`; respond 204 (or the saved subset). Preserve all other keys exactly.
- [x] 1.4 In `backend/handlers.go` `NewServer`, register `mux.HandleFunc("GET /api/settings", s.handleGetSettings)` and `mux.HandleFunc("PUT /api/settings", s.handlePutSettings)`.

## 2. Frontend — proxy + modal + JS

- [x] 2.1 In `frontend/handlers.go` `NewServer`, register `GET /api/settings` and `PUT /api/settings` to a `handleSettingsProxy` that calls `httpProxy(w, r, s.backendURL)` (forwards method/body/headers — mirrors `handleUploadProxy`).
- [x] 2.2 In `frontend/web/templates/layout.html`, add a settings gear `.iconbtn` in the header `.right` (before/after the accent picker) calling `openSettingsModal()`, and add the settings modal markup (a `<dialog>` or `.overlay` `.modal` matching `mockup-settings.html`: `.sel` dropdowns for model/advisorModel/effortLevel with `data-field`, an input `#settings-language`, a `.toggle` `#settings-thinking`, the "applies to new sessions" note, Cancel + a `#settings-save` button). Add a `<script src="/static/js/settings.js">` include.
- [x] 2.3 Create `frontend/web/static/js/settings.js`: `openSettingsModal()` fetches `GET /api/settings` and populates the `.sel` current values / language input / toggle, then shows the modal; wire the `.sel` open/pick/close behavior (reuse the accent-popover pattern); a save handler `PUT`s the collected subset as JSON, shows success (button state) then closes, or shows an error state and stays open; close on Cancel/backdrop/Escape.

## 3. Config

- [x] 3.1 In `docker-compose.yml`, change the `container-settings.json` bind mount from read-only to read-write (drop the `:ro` suffix) so the backend can persist edits.

## 4. Verification

- [x] 4.1 `go build ./...` + `go vet ./...` pass in both `backend/` and `frontend/`.
- [x] 4.2 Build + run (`make restart-backend && make restart-frontend`); smoke test: `GET /api/settings` returns the subset; `PUT` a valid change persists to both `container-settings.json` and `settings.json` and preserves `enabledPlugins`/`hooks`; an invalid `model` returns 400 and changes nothing; the gear modal opens, populates, saves, and shows "applies to new sessions"; modal works at mobile width.
