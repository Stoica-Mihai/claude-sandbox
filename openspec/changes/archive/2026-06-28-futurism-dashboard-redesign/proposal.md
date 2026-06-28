## Why

The dashboard's visual language is a generic DaisyUI/Tailwind skin loaded from CDNs and a 10-theme switcher that nobody asked for. We want a distinctive, self-contained identity — the Futurism design system (square corners, 2px ink borders, solid offset shadows, a single accent, bold italic display type, fast directional motion) — plus a simpler dark/light toggle and a user-selectable accent color. The redesign was prototyped and approved as an interactive mockup (`mockup-futurism.html`).

## What Changes

- **BREAKING**: Remove the Tailwind CSS and DaisyUI CDN dependencies. All dashboard styling becomes self-contained in `style.css` using the Futurism token system. Markup in `layout.html`, `sessions.html`, and `directory-picker.html` is rewritten from Tailwind/DaisyUI utility classes to plain semantic HTML + Futurism classes, preserving every JS hook, HTMX attribute, and Go template directive.
- Apply the Futurism design language to all dashboard chrome: header (logo mark, session-count pill, theme toggle, accent picker), session sidebar cards, terminal tab bar, terminal control bar, welcome screen, New Session modal, and rename modal.
- **BREAKING**: Replace the 10-theme DaisyUI dropdown (`theme.js`) with a simple **light/dark toggle**; persist in `localStorage`.
- Add an **accent color picker**: a single trigger swatch + popover offering 7 accents (Red/Amber/Lime/Cyan/Blue/Violet/Pink), each with a dark and light variant. It overrides the `--accent` token (and `--shadow` in the dark base); persists in `localStorage`. The theme toggle itself must not follow the accent.
- Replace the Outfit UI web font with the Futurism `Helvetica Neue` system stack; keep a system monospace stack for code/cwd/duration and xterm unchanged.
- Rewrite responsiveness in plain CSS media queries (no Tailwind `md:` prefixes), keeping all existing mobile behavior: sidebar drawer + backdrop, hamburger, compact header, mobile input bar, touch targets, no horizontal body scroll. The accent picker collapses to a compact trigger+popover (never an inline 7-swatch row) so it fits the cramped mobile header.
- Update `terminal.js`'s session-badge `MutationObserver` to target the new badge markup, and trim its terminal color themes to the dark/light pair.

## Capabilities

### New Capabilities
- `futurism-design-system`: The Futurism visual language applied to the dashboard (square corners, 2px borders, solid offset shadows, single-accent rule, display/body type discipline, directional motion, skewed CTAs) and the user-selectable accent color picker.

### Modified Capabilities
- `dashboard-ui`: `Self-contained application with embedded assets` (drop Tailwind/DaisyUI CDNs — styling becomes fully self-contained), `Dark/light theme toggle` (light/dark only via Futurism tokens, replacing the multi-theme dropdown), `Responsive layout` (plain CSS media queries instead of Tailwind prefixes), and `Terminal font rendering` (Helvetica Neue UI font instead of Outfit).

## Impact

- **Affected files**: `frontend/web/static/css/style.css`, `frontend/web/templates/layout.html`, `frontend/web/templates/fragments/sessions.html`, `frontend/web/templates/fragments/directory-picker.html`, `frontend/web/static/js/views.js`, `frontend/web/static/js/theme.js`, `frontend/web/static/js/terminal.js`.
- **No backend changes**: Go template data (`.Sessions`, `.DisplayName`, `.CWD`, `.Duration`, `.Hue`, `.Alive`, `.Name`, `.CreatedAt`, `.SessionID`, `.Breadcrumbs`, `.Dirs`, `.FullPath`, `.Path`) and all endpoints are unchanged. The per-session `Hue` field becomes unused (single-accent rule) but is left in place.
- **Dependencies removed**: `cdn.tailwindcss.com`, `cdn.jsdelivr.net/npm/daisyui`, and the Google Fonts (`Outfit`) CDN link. Core embedded assets (htmx, xterm + addons) are unchanged.
- **Preserved behavior**: xterm relay/WebSocket, HTMX swaps, SSE updates, resume-sessions, rename, kill, keyboard shortcuts (Alt+N/Alt+W/Alt+1–9), pull-to-refresh, mobile input bar, copy-on-select, image paste.
