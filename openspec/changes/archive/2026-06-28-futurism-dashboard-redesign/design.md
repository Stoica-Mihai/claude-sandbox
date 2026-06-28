## Context

The dashboard frontend (`frontend/web/`) is server-rendered Go templates + HTMX, with three JS modules (`terminal.js`, `views.js`, `theme.js`) and one stylesheet (`style.css`). Today it pulls Tailwind CSS + DaisyUI + Google Fonts from CDNs and uses a 10-theme DaisyUI switcher. An interactive Futurism mockup (`mockup-futurism.html`, in the repo root) was iterated with the user and approved; it is the visual + behavioral source of truth for this change.

Go emits no inline HTML — all markup is in the three templates, all styling in `style.css`. The backend, all endpoints, SSE, the relay, and Go template data structs are unchanged.

## Goals / Non-Goals

**Goals:**
- Replace Tailwind/DaisyUI/Outfit CDNs with a self-contained Futurism `style.css`.
- Restyle every dashboard surface to match `mockup-futurism.html`.
- Light/dark toggle (replaces multi-theme dropdown) + persisted accent picker.
- Top-notch responsiveness via plain CSS media queries; no horizontal body scroll.
- Preserve 100% of existing behavior and every JS/HTMX/Go-template contract.

**Non-Goals:**
- No backend/Go changes (template data and endpoints stay as-is; `Hue` is left unused).
- No fixing of pre-existing spec drift unrelated to this restyle (e.g. stale split/grid/Ctrl-shortcut wording in `dashboard-ui`).
- No new dependencies; xterm + addons + htmx embedded assets stay.

## Decisions

**D1 — Rip out Tailwind/DaisyUI rather than layer over them.** The approved mockup is pure Futurism with square corners and the single-accent rule, which fights DaisyUI's rounded, multi-token components. Layering would mean overriding hundreds of utility classes. Cleaner to delete the CDNs and author the dashboard with a small Futurism vocabulary. *Alternative considered:* keep Tailwind for layout utilities — rejected; the markup churn is the same and we'd keep a 3MB CDN for nothing.

**D2 — The mockup is the canonical reference.** `mockup-futurism.html` contains the complete, working CSS + markup + JS for header, sidebar cards, tab bar, control bar, welcome, modal, theme toggle, and accent picker. Each implementation task ports the corresponding mockup section into the real file, swapping mock data for the real Go template directives / JS hooks. This keeps the class vocabulary consistent across files authored in parallel.

**D3 — Canonical class vocabulary (contract).** All files MUST use these names so markup and CSS agree:
- Tokens (`:root` / `[data-theme="dark"]`): `--bg --surf --ink --muted --accent --line --shadow --field --on-accent --ease --fast --med --font --mono`.
- Chrome: `.app .hdr .brand .mark .title .sub .right .statpill .themewrap .toggle`.
- Accent picker: `.accpick .acctrig .accpop .acc` (+ `.acc.on` selected).
- Sidebar: `.side .shead .list .scard (.active) .scard .live(.dead) .nm .acts .iconbtn(.kill) .cwd .dur`.
- Main: `.main .tabbar .ttab(.on) .tdot(.on) .ctrlbar .keycap .term .welcome .keyhint`.
- Modal: `.overlay(.open) .modal .mhead .crumb(.seg/.sep/.cur) .folders .frow(.fmain/.fico/.fnm/.drill) .actions .actitle .arow(.sel) .atxt(.at1/.at2) .mfoot(.hint/.grp)`, plus Futurism `.btn .btn-primary .btn-ghost`.

**D4 — Preserve every JS/HTMX/Go contract.** Implementers MUST keep these intact (the JS depends on them):
- IDs: `sidebar sidebarBackdrop session-list singleTabBar singleControls singleTerminal singleWelcome mobileInputBar newSessionModal renameModal renameInput renameSubmit dir-picker dir-picker-form dir-picker-cwd dir-picker-resume dir-picker-submit dp-breadcrumb dp-folders session-actions pullIndicator session-count session-badge-text themeCurrentLabel(→removed, see D6)`.
- Data attrs: `data-terminal-id`, `data-session`, `data-created`, `--hue` may be dropped.
- Hooks: `openNewSessionModal(event)`, session-card click delegation (`.session-card` + `data-terminal-id`), `.session-duration[data-created]` ticker, rename buttons calling `openRenameModal(name, displayName)`, kill buttons (`hx-delete`/`cleanupKilledSession`), HTMX attrs on `#session-list` (`hx-get=/fragments/sessions hx-trigger=sse:update`) and `#dir-picker-form` (`hx-post=/api/sessions`).
- `<dialog>` elements are kept (native `showModal()`), only restyled — not replaced by DaisyUI `.modal`.
- Directory-picker template directives (`.Breadcrumbs .Dirs .FullPath .Path`) and `dpSelectFolder`/`dpResetBrowse`/`dirPickerSetSel`/`dpFooter` behavior unchanged; only the row/breadcrumb markup + classes are restyled (Futurism), and `views.js` DOM-builder strings (tabs, sa-rows, footer) are rewritten to the new classes.

**D5 — `style.css` is the foundation (Wave 0).** It defines the token system + every component + responsive media queries. Templates and JS consume its vocabulary, so it is authored first and everything else depends on it.

**D6 — Theme + accent in `theme.js`.** Replace the 10-theme IIFE with: read `localStorage` `theme` (default `dark`) and `accent` (default `Red`); set `data-theme` + `data-theme-base`; define `ACCENTS` (7 × {dark,light}); `applyAccent()` sets `--accent` and (dark base only) `--shadow`; wire the header toggle (`flipTheme`) and the accent trigger/popover; on any change call `syncTerminalBgVar()` + `TerminalManager.rethemeAll()`. The theme toggle track is forced to `--ink` (accent-independent). Drop `themeCurrentLabel`/`themeMenu`.

**D7 — `terminal.js` minimal touch.** Update the session-badge `MutationObserver` (currently writes Tailwind `hidden md:inline` + `text-emerald-500 pulse-alive`) to write the new `.statpill` badge text/state. Trim `terminalThemes` to the `dark` and `light` entries actually used (the other 8 are dead once the multi-theme picker is gone). `getTerminalTheme`/`syncTerminalBgVar`/`rethemeAll` logic stays (keys off `data-theme`).

**D8 — Responsiveness via media queries.** Mobile-first base + `@media (min-width:768px)` / `(min-width:1024px)`. Sidebar: off-canvas `fixed` + `translateX(-100%)` on mobile, `.sidebar-open` (toggled by existing `views.js`) slides it in with the existing `#sidebarBackdrop`; static in-flow at ≥768px. Header compacts on mobile (hide `.title`/`.sub` text, keep mark + pill + accent trigger + toggle). Control bar hidden on mobile (mobile input bar already handles keys). `.crumb` and `.tabbar` get `overflow-x:auto`; `body`/`.app` get `overflow:hidden` to forbid horizontal scroll. Touch targets ≥36px.

**D9 — Fonts.** UI: `--font: 'Helvetica Neue',Arial,sans-serif` on `body` (not `*`). Mono (cwd/dur/tab/term-ish UI): `--mono` system stack. xterm's own `fontFamily` (Menlo…) in `terminal.js` is unchanged. Remove the Google Fonts link.

## Risks / Trade-offs

- **[Parallel authors diverge on class names]** → D3 fixes the vocabulary explicitly and every task cites the exact mockup section + the JS/HTMX hooks to preserve.
- **[A preserved hook is missed, breaking click/rename/kill/resume/SSE]** → D4 enumerates the full contract; Phase 2e/2f verify against it; manual smoke covers spawn/click/rename/kill/resume.
- **[xterm grid mis-measures if a font rule hits `*` or `.xterm`]** → keep font rules on `body` only; never style `*`/`.xterm` (existing constraint, restated in spec).
- **[Removing CDNs breaks an unnoticed DaisyUI/Tailwind usage]** → grep confirmed styling lives only in the 3 templates + `style.css`; Go emits no classes. Post-change grep for `base-content|daisy|tailwind|md:` must return only intended results.
- **[Light-base terminal colors]** → `data-theme-base` still drives `style.css` light overrides and `terminal.js` light terminal theme; keep that mapping (`light` ⇒ light base).

## Migration Plan

No data migration. Deploy = rebuild the frontend image (`make restart-frontend`). Rollback = revert the commit. `localStorage` keys (`theme`, `accent`) are additive; an old value is simply re-defaulted.
