## Context

`container-settings.json` is the authoritative Claude config for the container — gitignored, seeded from `container-settings.example.json` by `generate-env.sh`, mounted into the backend (currently `:ro`), and copied by `entrypoint.sh` to `$CLAUDE_CONFIG_DIR/settings.json` on every container start. `claude` reads `settings.json` at session spawn. The dashboard is Go backend (`backend/`, per-route `net/http` ServeMux) + Go frontend (`frontend/`, per-route reverse proxy + HTMX templates + Futurism CSS). An approved mockup exists at `mockup-settings.html`.

## Goals / Non-Goals

**Goals:**
- Edit a safe preference subset of `container-settings.json` from the dashboard.
- Apply to new sessions without a container restart.
- Never let the UI corrupt plugins/hooks/marketplaces/env.

**Non-Goals:**
- No editing of plugins, hooks, marketplaces, env flags, or `skipDangerousModePermissionPrompt` from the UI.
- No live re-config of running sessions; no container restart.
- No new persistence store — reuse the existing file + entrypoint copy semantics.

## Decisions

**D1 — Whitelist + merge, never wholesale write.** `PUT` decodes the editable subset, validates it, then reads the existing `container-settings.json` into a generic `map[string]any`, overlays only the whitelisted keys, and re-marshals. This guarantees `enabledPlugins`/`hooks`/`extraKnownMarketplaces`/`env`/`skipDangerousModePermissionPrompt` survive byte-for-byte (value-wise). *Alternative:* a typed struct for the whole file — rejected; it would drop unknown keys and couple the backend to the full settings schema.

**D2 — Validate against an allowlist constant.** `allowedModels = {"opus[1m]","opus","sonnet","haiku"}` (single `var`/const slice, easy to extend); `effortLevel ∈ {low,medium,high}`; `alwaysThinkingEnabled` bool; `advisorModel ∈ allowedModels ∪ {""}`; `language` a short string with no control chars. Invalid → 400, no file touched. Dropdowns in the UI keep input on-allowlist, but the backend is the enforcement point.

**D3 — Write source then refresh live copy.** On save: write `container-settings.json`, then write the same bytes to `$CLAUDE_CONFIG_DIR/settings.json`. Writes prefer atomic temp+rename, but `container-settings.json` is a **single-file bind mount** — rename onto the mount point fails (EBUSY/EXDEV), so the writer falls back to an in-place overwrite through the mount (the same way `entrypoint.sh`'s `cp -f` works). The live `settings.json` lives in a directory mount, so rename succeeds there. Next spawned `claude` reads the refreshed `settings.json`.

**D4 — RW mount.** `docker-compose.yml` drops `:ro` on the `container-settings.json` mount so the backend can write it. It's gitignored, so host-side writes don't dirty git. Risk is bounded: localhost single-user sandbox behind an external auth proxy.

**D5 — Paths.** Add `containerSettingsPath()` (env `CONTAINER_SETTINGS_PATH`, default `/home/claude/container-settings.json`) and `settingsJSONPath()` (= `filepath.Join(claudeConfigDir(), "settings.json")`) to `backend/paths.go`.

**D6 — Frontend proxy is per-route.** There is no catch-all `/api` proxy; add `GET /api/settings` and `PUT /api/settings` routes that call the existing `httpProxy` helper (forwards method/body/headers), like `handleUploadProxy`/`handleHealthzProxy`.

**D7 — UI is a new `settings.js` + a `<dialog>` (or `.overlay`) modal** matching the approved `mockup-settings.html`: gear `.iconbtn` in the header `.right`, custom `.sel` dropdowns (reuse the accent-popover open/pick/close pattern), `.toggle`, `.btn`. `openSettingsModal()` does `GET` then shows; Save does `PUT` then success state + close. Included via a `<script>` tag in `layout.html`.

## Risks / Trade-offs

- **[UI overwrites plugins/hooks]** → D1 merge + the GET/PUT subset never carry those keys; a spec scenario asserts preservation.
- **[Invalid value breaks every spawn]** → D2 allowlist + 400; dropdowns prevent it client-side too.
- **[Partial/corrupt write]** → D3 atomic temp+rename, 0600.
- **[RW mount widens write surface]** → D4: bounded to a gitignored file in a localhost sandbox; documented.
- **[settings.json and container-settings.json drift]** → D3 writes both from the same merged bytes in one handler; if the second write fails, return 500 (source already updated; next restart reconciles via entrypoint).

## Migration Plan

Deploy = rebuild both images + recreate the backend container so the new RW mount takes effect (`make up` / `make restart-backend`). Rollback = revert the commit and recreate (mount returns to `:ro`). No data migration; the file format is unchanged (same keys, merged in place).
