## 1. CSS tokens (style.css)

- [x] 1.1 Add `--ok:#3fb950;` to the light `:root` block (after `--terminal-bg`) and the SAME `--ok:#3fb950;` to the `[data-theme="dark"]` block (same concrete value in both per D2).
- [x] 1.2 Confirm `--terminal-bg` precedent is intact; no `--terminal-fg` default is added to `:root` (it is set at runtime in task 4.1, matching how `--terminal-bg` works).

## 2. Key-chip unification (style.css)

- [x] 2.1 Keep the base `.keycap` rule (style.css:196-197) as-is (it already holds the canonical ctrlbar values) and keep `.keycap:hover` (style.css:198) as the shared hover affordance.
- [x] 2.2 Add `.keycap--hint{font-size:11px;min-width:54px;text-align:center;cursor:default}`.
- [x] 2.3 Add `.keycap--mobile{font-size:13px;height:36px;min-width:36px;padding:0 10px;flex-shrink:0}` and `.keycap--mobile:active{background:var(--accent);color:var(--on-accent);border-color:var(--accent)}`.
- [x] 2.4 Add `.keycap--mobile.sel-active{color:var(--ok);border-color:color-mix(in srgb,var(--ok) 30%,transparent)}` (reproduces the old `rgba(63,185,80,0.3)` border exactly, per D2).
- [x] 2.5 Delete the `.keyhint kbd` rule (style.css:214); keep `.keyhint` (213) and `.keyhint span` (215) layout rules.
- [x] 2.6 Delete the `.mobile-key` and `.mobile-key:active` rules (style.css:293-296), now replaced by `.keycap--mobile`.
- [x] 2.7 Delete the dead `.terminal-controls-bg .kbd` block outright — the base rule, its `:hover`, and both `[data-theme-base="light"]` overrides (style.css:304-307). No alias (Q3 FINAL).

## 3. Lifted affordance rules (style.css)

- [x] 3.1 Add `#selectOverlay{position:absolute;inset:0;z-index:50;margin:0;padding:12px;overflow-y:auto;-webkit-overflow-scrolling:touch;font-size:13px;font-family:var(--mono);line-height:1.4;color:var(--terminal-fg);background:var(--terminal-bg);user-select:text;-webkit-user-select:text;white-space:pre-wrap;word-break:break-all}` (per D3; font-family is `var(--mono)`).
- [x] 3.2 Add `input.err-flash{outline:3px solid var(--accent)}` for the rename-error state (per D4).
- [x] 3.3 Add `.btn-spawn-fail{background:var(--accent);color:var(--on-accent)}` for the spawn-failure button state (per D4).

## 4. Runtime token (terminal.js)

- [x] 4.1 In `syncTerminalBgVar()` (terminal.js ~line 94), add `document.documentElement.style.setProperty('--terminal-fg', theme.foreground);` immediately after the existing `--terminal-bg` setProperty.
- [x] 4.2 Leave the xterm palette (terminal.js:41-49, incl. green at :47) untouched.

## 5. views.js — replace inline color writes with class/text toggles

- [x] 5.1 `mobileToggleSelect` remove branch (line 194): replace `btn.style.color=''; btn.style.borderColor='';` with `btn.classList.remove('sel-active')`.
- [x] 5.2 `mobileToggleSelect` overlay creation (line 215): delete the `overlay.style.cssText = '...'` line entirely (the `#selectOverlay` id rule now applies via the id set at line 213).
- [x] 5.3 `mobileToggleSelect` activate (line 220): replace `btn.style.color='#3fb950'; btn.style.borderColor='rgba(63,185,80,0.3)';` with `btn.classList.add('sel-active')`.
- [x] 5.4 Rename-error handler (line 324): replace `...style.outline = '3px solid #d22f1a'` with `el.classList.add('err-flash')` (resolve the `renameInput` element first if needed); in the timeout (lines 325-328) replace `el.style.outline = ''` with `el.classList.remove('err-flash')`.
- [x] 5.5 Spawn-fail handler (lines 472-473): replace `submitBtn.style.background='#d22f1a'; submitBtn.style.color='#efe9dc';` with `submitBtn.classList.add('btn-spawn-fail')`; in the timeout (lines 477-478) replace the `style.background=''`/`style.color=''` resets with `submitBtn.classList.remove('btn-spawn-fail')` (keep the `textContent` writes).
- [x] 5.6 Verify no hardcoded theme-able hex (`#d22f1a`, `#efe9dc`, `#3fb950`, `#c9d1d9`, `#0d1117`) and no `getComputedStyle`/`getPropertyValue` remain anywhere in views.js.

## 6. Markup migration (templates/layout.html)

- [x] 6.1 Change all 8 `class="mobile-key"` buttons (layout.html:107-116) to `class="keycap keycap--mobile"`.
- [x] 6.2 Change the 4 `.keyhint` `<kbd>` elements (layout.html:97-100) from `<kbd>` to `<kbd class="keycap keycap--hint">`.
- [x] 6.3 Leave the 3 existing ctrlbar `class="keycap"` buttons (layout.html:83-85) unchanged.

## 7. Verification (zero visual regression)

- [x] 7.1 Build/serve the frontend and load the dashboard in the dark theme; confirm ctrlbar keys, welcome/keyhint labels, and mobile toolbar keys look identical to before (border, bg, ink, font, padding, hover/active).
- [x] 7.2 Toggle to the light theme and confirm the same three chip groups are visually unchanged.
- [x] 7.3 Trigger the mobile Select toggle and confirm the active green color + border match the previous `#3fb950` / `rgba(63,185,80,0.3)` exactly, and toggling off clears it.
- [x] 7.4 Open the selection overlay and confirm foreground/background match the live xterm palette in both themes; confirm the font is the mono stack.
- [x] 7.5 Trigger a rename error and a spawn failure; confirm the outline/button colors now follow the active theme accent (and revert after the timeout).
- [x] 7.6 Confirm the accent-picker swatches (theme.js:4-10) and the xterm palette are unchanged.
- [x] 7.7 Run `openspec validate frontend-css-single-source-cleanup` and confirm it is clean.
