## Why

The dashboard's key-chip styling is duplicated across four divergent CSS rules (`.keycap`, `.keyhint kbd`, `.mobile-key`, and a dead `.terminal-controls-bg .kbd`), and several theme-able colors are hardcoded as hex literals in `views.js` (the light accent `#d22f1a`, the terminal green `#3fb950`, the on-accent cream `#efe9dc`, and the xterm foreground/background `#c9d1d9`/`#0d1117`). Because the same visual is expressed in multiple places, a single fix does not propagate, and the hardcoded colors silently break under the light/dark theme toggle. This consolidates each element to a single source of truth so one fix propagates and theme values always follow the design tokens — with zero visual change in either theme.

## What Changes

- **A — Unify key chips:** Collapse the four key-chip definitions into ONE `.keycap` base atom plus modifier classes. `.keycap` keeps the current `.ctrlbar` values as the default; `.keycap--hint` (welcome/keyhint labels) and `.keycap--mobile` (mobile toolbar keys) carry only the genuine deltas. The shared hover affordance lives on `.keycap:hover`; touch interaction uses `.keycap--mobile:active`. The dead `.terminal-controls-bg .kbd` rule (and its `:hover` and both `[data-theme-base="light"]` overrides) is **deleted outright** — no element carries `class="kbd"`. Go template markup is migrated: `class="mobile-key"` → `class="keycap keycap--mobile"`; `.keyhint <kbd>` → `<kbd class="keycap keycap--hint">`; existing `.keycap` ctrlbar buttons unchanged.
- **B — Tokenize theme-able colors in `views.js`:** Replace the hardcoded `#d22f1a` (rename-error outline at views.js:324; spawn-fail button background at views.js:472) and `#efe9dc` (spawn-fail button text at views.js:473) with the existing `var(--accent)` / `var(--on-accent)` tokens applied via CSS, so they follow the theme. The terminal green `#3fb950` (mobile Select-toggle active, views.js:220) moves to a new `--ok` token and a `.sel-active` state class.
- **C1 — Lift theme-baking inline styles into classes:** The `selectOverlay` inline `cssText` (views.js:215, hardcoded `#c9d1d9`/`#0d1117`) moves to a `#selectOverlay` CSS rule driven by `--terminal-fg`/`--terminal-bg` tokens that track the live xterm theme. A new `--terminal-fg` token is set at runtime in `terminal.js` `syncTerminalBgVar()` alongside the existing `--terminal-bg`.
- **New tokens:** `--ok:#3fb950` added to BOTH `:root` blocks (same concrete value in light and dark — a fixed status green, deliberately not theme-derived, mirroring the constant xterm palette green). `--terminal-fg` set at runtime like `--terminal-bg`.
- **Out of scope (left untouched):** the xterm color palette in `terminal.js` lines 41-49 (incl. the green at line 47), the accent-picker swatch list in `theme.js` lines 4-10, and one-off structural inline styles (C2 — e.g. `#singleTerminal` flex/overflow, brand layout).
- No `getComputedStyle`/`getPropertyValue` read pattern is introduced; `views.js` only toggles classes and text, letting the cascade resolve all theme values.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `futurism-design-system`: The key-chip control is now specified as a single base atom with modifier classes (one source of truth), and theme-able colors used by dashboard JS are required to resolve through CSS tokens (`--accent`, `--on-accent`, `--ok`, `--terminal-fg`, `--terminal-bg`) rather than hardcoded hex, so they track the active theme.

## Impact

- `frontend/web/static/css/style.css` — unify `.keycap`/modifiers, delete dead `.kbd` rule, add `--ok` token (both `:root` blocks), add `.sel-active` state class, add `#selectOverlay` rule.
- `frontend/web/static/js/views.js` — drop inline color writes at lines 194, 215, 220, 324, 472-473; toggle `.sel-active` class instead.
- `frontend/web/static/js/terminal.js` — set `--terminal-fg` in `syncTerminalBgVar()`.
- `frontend/web/templates/layout.html` — migrate `mobile-key` and `keyhint kbd` markup to the unified classes.
- No backend, API, or behavioral changes. Goal is zero visual regression in both light and dark themes.
