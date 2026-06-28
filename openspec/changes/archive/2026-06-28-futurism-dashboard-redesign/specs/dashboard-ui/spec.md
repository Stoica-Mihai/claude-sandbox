## MODIFIED Requirements

### Requirement: Self-contained application with embedded assets
The dashboard SHALL be served from a single Go binary with core JS assets (htmx.js, xterm.js + addons) embedded via `go:embed`. All visual styling SHALL be self-contained: the dashboard SHALL NOT load any third-party CSS framework or font from a CDN (no Tailwind, no DaisyUI, no Google Fonts). Styling SHALL come solely from the embedded `style.css` (Futurism token system), and UI typography SHALL use system font stacks.

#### Scenario: Load dashboard
- **WHEN** the user navigates to `http://host:8080/`
- **THEN** the browser SHALL load the dashboard with HTMX and xterm.js from embedded assets and all styling from the embedded `style.css`, making no requests to any CSS, font, or framework CDN

#### Scenario: No external styling dependencies
- **WHEN** the dashboard page is served
- **THEN** the HTML SHALL contain no `<link>` or `<script>` tags pointing to `cdn.tailwindcss.com`, `daisyui`, or `fonts.googleapis.com`

### Requirement: Dark/light theme toggle
The dashboard SHALL support exactly two themes — dark and light — implemented via the Futurism token system on the `data-theme` attribute (`dark` = carbon, `light` = paper). A single toggle control in the header SHALL switch between them; there SHALL be no multi-theme dropdown. The user's preference SHALL persist in localStorage and be restored on reload. Switching the theme SHALL also update the terminal background CSS variable and re-theme open xterm.js instances.

#### Scenario: Toggle theme
- **WHEN** the user clicks the theme toggle
- **THEN** the `data-theme` attribute on the HTML element SHALL switch between `light` and `dark`, the preference SHALL be saved to localStorage, and open terminals SHALL be re-themed

#### Scenario: Theme persistence
- **WHEN** the user reloads the dashboard
- **THEN** the theme SHALL be restored from localStorage (defaulting to dark when unset)

#### Scenario: No multi-theme picker
- **WHEN** the user opens the theme control
- **THEN** it SHALL be a binary light/dark toggle, not a list of named DaisyUI themes

### Requirement: Terminal font rendering
The xterm.js terminal SHALL use system monospace fonts (Menlo, Monaco, Consolas, Liberation Mono) instead of web fonts. UI typography SHALL use the Futurism `Helvetica Neue` system stack (not a web font such as Outfit), and monospace UI text (cwd, duration, tab labels) SHALL use a system monospace stack. Global CSS font rules SHALL NOT use the `*` universal selector, as this interferes with xterm.js's internal character grid measurement; the `body` selector SHALL be used for UI fonts instead.

#### Scenario: Terminal text renders correctly
- **WHEN** a Claude Code session is displayed in the xterm.js terminal
- **THEN** all characters SHALL render with correct monospace spacing with no extra gaps or misaligned characters

#### Scenario: CSS does not interfere with xterm.js
- **WHEN** custom CSS sets a UI font (Helvetica Neue)
- **THEN** the font rule SHALL be scoped to `body` (not `*`) so xterm.js internal measurement elements are not affected

### Requirement: Spawn new session via HTMX
The dashboard SHALL provide a "New" button in the sidebar header and a "+" placeholder card in grid view to spawn a new Claude Code session. A modal dialog SHALL present a directory picker for selecting a directory under `/workspace`.

#### Scenario: Create new session via UI
- **WHEN** the user clicks "New", selects a directory from the picker, and confirms
- **THEN** HTMX SHALL POST to the spawn endpoint, the server SHALL spawn the session and push an SSE update event, and the terminal view SHALL auto-open for the new session (reading the `X-Terminal-Id` response header)

#### Scenario: Shift+Click New for split right pane
- **WHEN** the user Shift+clicks the "New" button, selects a directory, and confirms
- **THEN** the new session SHALL auto-open in the split view right pane (moving any existing single terminal to the left pane if not already in split view)

#### Scenario: Directory picker browsing
- **WHEN** the user opens the directory picker
- **THEN** HTMX SHALL GET the directory listing endpoint and render folders under `/workspace` inside the modal dialog with breadcrumb navigation, allowing drill-down into subfolders via `hx-get` with `hx-target` swaps. Folder names SHALL NOT be selectable as text (`user-select: none`). Selection and confirmation behavior is defined by the "New Session modal browses folders and resumes past sessions" and "Select-then-confirm with a single morphing action" requirements.

### Requirement: Responsive layout
The dashboard SHALL be responsive across mobile (<768px), tablet (768-1024px), and desktop (>1024px) using plain CSS media queries (no utility-framework responsive prefixes). There SHALL be no horizontal scrolling of the page body at any width; wide content SHALL scroll within its own container.

#### Scenario: Mobile layout
- **WHEN** the viewport is narrower than 768px (e.g., phone in portrait)
- **THEN** the sidebar SHALL be hidden as a slide-out drawer accessible via a hamburger menu (☰) in the header. The header SHALL show only the hamburger, logo mark, session count, theme toggle, and the accent picker collapsed to its compact trigger. A semi-transparent backdrop SHALL overlay the content when the drawer is open. Clicking a session in the drawer SHALL close the drawer and open the terminal. Control buttons SHALL meet a minimum 36px touch target.

#### Scenario: Tablet layout
- **WHEN** the viewport is between 768px and 1024px
- **THEN** the sidebar SHALL be visible with a narrower width. The header SHALL show full text and all controls.

#### Scenario: Desktop layout
- **WHEN** the viewport is wider than 1024px
- **THEN** the sidebar SHALL be full width. All features SHALL be available unchanged.

#### Scenario: No horizontal body scroll
- **WHEN** the dashboard is viewed at any width, including the narrowest mobile widths
- **THEN** the page body SHALL NOT scroll horizontally; any wide content (breadcrumb, tab bar) SHALL scroll within its own `overflow-x` container
