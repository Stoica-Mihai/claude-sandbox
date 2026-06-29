## Context

The dashboard is styled by a single self-contained Futurism stylesheet (`frontend/web/static/css/style.css`); markup is Go `html/template` files (`frontend/web/templates/`) plus vanilla-JS template strings and direct DOM manipulation (`frontend/web/static/js/{views,terminal,theme}.js`). Three smells violate single-source-of-truth:

1. **Four divergent key-chip rules.** `.keycap` (style.css:196-198, ctrlbar), `.keyhint kbd` (style.css:214, welcome), `.mobile-key` (style.css:293-296, mobile toolbar), and `.terminal-controls-bg .kbd` (style.css:304-307, dead) all re-express the same chip look with small deltas. A confirming grep shows no element carries `class="kbd"`, so the fourth rule is dead.
2. **Hardcoded theme-able hex in `views.js`.** `#d22f1a` (light accent) at lines 324 and 472; `#efe9dc` (on-accent cream) at line 473; `#3fb950` (status green) at line 220. These do not follow the theme toggle.
3. **A theme-baking inline `cssText`** on the `selectOverlay` (views.js:215) hardcoding `#c9d1d9`/`#0d1117` — the xterm foreground/background.

Existing precedent: `--terminal-bg` is already a runtime-set token (terminal.js:94, syncTerminalBgVar) consumed by `.terminal-bg`, `#mobileInputBar`, `#viewSingle`. The `.sa-row` rules (style.css:258-263) already demonstrate driving JS-built buttons from CSS classes rather than inline styles.

## Goals / Non-Goals

**Goals:**
- One source of truth per key chip and per theme-able color, so a single fix propagates.
- All theme-able colors resolve through CSS tokens and follow the light/dark toggle.
- Zero visual regression in either theme — values are preserved exactly.

**Non-Goals:**
- No restyling or visual change of any kind.
- No touching the xterm palette (terminal.js:41-49, incl. green at :47) or the accent-picker swatch list (theme.js:4-10) — these are intentionally fixed.
- No touching one-off structural inline styles (C2): `#singleTerminal` flex/overflow, brand layout, the modal-row `width/border/text-align` inline `cssText` in views.js (dpSelectFolder), tab-bar inline spans. Only theme-baking or repeated inline styles are lifted.
- No new JS read pattern (`getComputedStyle`/`getPropertyValue`).

## Decisions

### D1 — Key chips: one `.keycap` atom + modifiers (Q4, FINAL)
Base `.keycap` keeps the current ctrlbar values verbatim (the most common chip):
`border:2px solid var(--line);background:var(--surf);color:var(--ink);font-family:var(--mono);font-size:10px;font-weight:700;padding:3px 9px;cursor:pointer;transition:background var(--fast),color var(--fast)`.
Shared `.keycap:hover{background:var(--accent);color:var(--on-accent);border-color:var(--accent)}`.
Modifiers carry only deltas:
- `.keycap--hint` → `font-size:11px;min-width:54px;text-align:center;cursor:default` (non-interactive labels; cursor reverts; no active/hover use intended).
- `.keycap--mobile` → `font-size:13px;height:36px;min-width:36px;padding:0 10px;flex-shrink:0`.
- `.keycap--mobile:active{background:var(--accent);color:var(--on-accent);border-color:var(--accent)}` (touch devices fire `:active`, not `:hover`).

Markup migration: `class="mobile-key"` → `class="keycap keycap--mobile"`; `.keyhint <kbd>` → `<kbd class="keycap keycap--hint">` (keep the `kbd` tag); ctrlbar `class="keycap"` buttons unchanged. Drop `.mobile-key`, `.keyhint kbd`; keep `.keyhint` and `.keyhint span` layout rules. **Delete** the dead `.terminal-controls-bg .kbd` block outright (Q3, FINAL) — no alias, since aliasing re-introduces a divergent definition.

*Alternative considered:* keep `.mobile-key`/`.keyhint kbd` as thin aliases of `.keycap`. Rejected — aliases still divide the source of truth and the request mandates one atom.

### D2 — Status green: `--ok` token + `.sel-active` class (Q1, FINAL)
Add `--ok:#3fb950` to BOTH `:root` blocks (same value light and dark — a fixed terminal/status green, mirroring how the xterm green is constant, deliberately not theme-derived). Add `.keycap--mobile.sel-active{color:var(--ok);border-color:color-mix(in srgb,var(--ok) 30%,transparent)}` — the `color-mix` reproduces the old `rgba(63,185,80,0.3)` border exactly. In `views.js` `mobileToggleSelect`, toggle the class instead of writing `element.style`: on activate `btn.classList.add('sel-active')`; on deactivate/remove `btn.classList.remove('sel-active')` (replacing the `btn.style.color=''/borderColor=''` resets at line 194 and the inline assignments at line 220).

*Alternative considered:* read the green from the xterm theme object. Rejected — the Select indicator is a Futurism UI affordance, not terminal content, so it belongs in the design system, not coupled to xterm's palette.

### D3 — Selection overlay: `#selectOverlay` rule + `--terminal-fg` token (Q2, FINAL)
Introduce `--terminal-fg`, set at runtime exactly like `--terminal-bg`: in `terminal.js` `syncTerminalBgVar()` (around line 94) add `document.documentElement.style.setProperty('--terminal-fg', theme.foreground);` immediately after the existing `--terminal-bg` setProperty, so both track the live xterm theme. Add the CSS rule:
`#selectOverlay{position:absolute;inset:0;z-index:50;margin:0;padding:12px;overflow-y:auto;-webkit-overflow-scrolling:touch;font-size:13px;font-family:var(--mono);line-height:1.4;color:var(--terminal-fg);background:var(--terminal-bg);user-select:text;-webkit-user-select:text;white-space:pre-wrap;word-break:break-all}`.
In `views.js`, drop the inline `cssText` at line 215 entirely — the element already has `id="selectOverlay"` (set at line 213) so the rule applies automatically. Note the font-family becomes `var(--mono)` (was bare `monospace`) for consistency with the terminal font; this matches the existing mono stack and is not a visual regression.

*Alternative considered:* keep the overlay colors concrete since it must match xterm not the Futurism theme. Rejected — `--terminal-bg` already establishes the runtime-token precedent; `--terminal-fg` completes the pair so the overlay can never drift from the xterm palette.

### D4 — Accent affordances in `views.js`: drive via CSS, no JS color writes (Q5, FINAL)
The remaining `#d22f1a`/`#efe9dc` writes are the rename-error outline (line 324) and the spawn-fail button (lines 472-473). These are styled via CSS so they follow the theme, with JS toggling a class and the CSS rule supplying `var(--accent)`/`var(--on-accent)`. **Decision (open item resolved here, FINAL for later artifacts):** introduce two small state classes in `style.css` and have `views.js` toggle them instead of writing inline color:
- Rename error: `input.err-flash{outline:3px solid var(--accent)}`. `views.js:324` becomes `el.classList.add('err-flash')` and the timeout reset (lines 325-328) becomes `el.classList.remove('err-flash')` (replacing the `style.outline` writes).
- Spawn fail: `.btn-spawn-fail{background:var(--accent);color:var(--on-accent)}`. `views.js:472-473` becomes `submitBtn.classList.add('btn-spawn-fail')` and the reset (lines 477-478) becomes `submitBtn.classList.remove('btn-spawn-fail')` (replacing the `style.background`/`style.color` writes; the `textContent` writes are unchanged).

This keeps `views.js` doing only class/text toggles (its existing idiom, matching `.sa-row`), introduces no `getComputedStyle` read pattern, and removes every theme-able hex literal from `views.js`.

*Alternative considered:* assign `el.style.outline = '3px solid var(--accent)'` directly. Rejected per Q5 — moving styling into CSS classes is the cleaner single-source-of-truth outcome.

### D5 — Scope boundary for inline styles (C1 vs C2)
Lift ONLY inline styles that bake a theme value or repeat. Lifted: `selectOverlay` (D3), Select-active color (D2), rename-error outline + spawn-fail button (D4). Left alone (C2, one-off structural): the `dpSelectFolder` row `cssText` (`width/background:var(--row-bg)/border/text-align`) which already uses a token and is structural; tab-bar `<span style="font-family:var(--mono);font-style:normal">`; `label.style.cssText` flex layouts; `indicator.style.height` animation writes; `#singleTerminal` flex/overflow and brand layout in templates.

## Risks / Trade-offs

- [`color-mix` browser support for the `.sel-active` border] → `color-mix(in srgb,…)` is already used throughout `style.css` (state washes, `.btn.saving`), so the baseline already depends on it; no new risk.
- [Visual drift if any preserved value is mistyped] → Mitigation: every base/modifier value is copied verbatim from the current rules; reviewer diffs the computed style of each chip before/after in both themes.
- [`--terminal-fg` not set before first overlay open] → Mitigation: `syncTerminalBgVar()` runs at startup (terminal.js:479) and on every theme change, exactly as `--terminal-bg` already does; the overlay is only reachable after a terminal exists, by which point both tokens are set.
- [Light theme uses a different xterm palette] → `--terminal-fg`/`--terminal-bg` are set from `getTerminalTheme()` which already switches palette by app theme, so the overlay follows whichever palette is live — same behavior as today, just tokenized.

## Migration Plan

Pure frontend, no data/state migration. Apply CSS + JS + template edits together (one cohesive change), then visually verify each key chip and each affordance in both light and dark themes. Rollback is a straight revert of the three asset files plus the template; no backend or persisted state is touched.

## Open Questions

None. Q1-Q5 are decided (FINAL) and the one remaining open item — how to remove the `views.js` accent hex without a JS color read — is resolved in D4 (state classes `err-flash` / `btn-spawn-fail`).
