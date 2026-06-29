## ADDED Requirements

### Requirement: Key-chip controls share a single source of truth
The dashboard's key-chip controls — the desktop control-bar keys, the welcome/keyhint labels, and the mobile toolbar keys — SHALL be styled from a single base atom class `.keycap` plus role/size modifier classes, so that one change to the chip's shared visual identity (border, background, ink color, mono font, weight, transition, hover affordance) propagates to every key chip. The base `.keycap` SHALL define the shared identity and the default size used by the control-bar keys. Modifier classes SHALL override ONLY the genuine per-variant deltas: `.keycap--hint` for non-interactive welcome labels and `.keycap--mobile` for the mobile toolbar keys. The shared hover affordance SHALL live on `.keycap:hover`; mobile touch interaction SHALL use `.keycap--mobile:active`. No duplicate, divergent definition of a key chip SHALL exist; in particular the previously dead `.terminal-controls-bg .kbd` rule SHALL NOT be re-introduced as an alias. There SHALL be no visual change to any key chip in either the light or dark theme.

#### Scenario: One change propagates to all key chips
- **WHEN** the shared key-chip identity (border, background, ink color, mono font, weight, transition, or hover) is changed on the `.keycap` base
- **THEN** the control-bar keys, welcome/keyhint labels, and mobile toolbar keys SHALL all reflect the change without editing any per-variant rule

#### Scenario: Variant modifiers carry only genuine deltas
- **WHEN** a welcome/keyhint label or a mobile toolbar key is rendered
- **THEN** it SHALL carry `.keycap` plus its modifier (`.keycap--hint` or `.keycap--mobile`), and the modifier SHALL override only the values that genuinely differ (font-size, min-width, padding, height, cursor, interaction trigger)

#### Scenario: No dead or duplicate chip rule remains
- **WHEN** the stylesheet is inspected after the change
- **THEN** there SHALL be exactly one base `.keycap` atom with its modifiers, and the dead `.terminal-controls-bg .kbd` rule (and its `:hover` and `[data-theme-base="light"]` overrides) SHALL be absent

#### Scenario: Key chips are visually unchanged in both themes
- **WHEN** the dashboard is rendered in the light theme and in the dark theme
- **THEN** every key chip SHALL appear identical to its pre-change rendering in that theme

### Requirement: Theme-able colors used by dashboard JS resolve through CSS tokens
Colors that are meant to follow the active theme SHALL be expressed through CSS custom-property tokens applied via CSS rules or class toggles, NOT as hardcoded hex literals written to element styles from JavaScript. The dashboard's accent-colored affordances driven from JavaScript (the rename-error outline and the spawn-failure button styling) SHALL resolve through `var(--accent)` / `var(--on-accent)`. The mobile Select-toggle active indicator SHALL resolve through the single `--accent` token, applied via a `.sel-active` state class rather than an inline style; no second hue (e.g. a status green) SHALL be introduced — the system stays one-red (law 4). The terminal text-selection overlay SHALL match the live xterm palette through the runtime-set tokens `--terminal-fg` and `--terminal-bg` applied via a CSS rule, rather than hardcoded foreground/background hex. JavaScript SHALL toggle classes and text only; it SHALL NOT read or write concrete theme color strings (no `getComputedStyle`/`getPropertyValue` and no inline color writes for these affordances). Concrete-hex sources that are deliberately not theme-derived — the xterm color palette and the accent-picker swatch list — SHALL remain untouched.

#### Scenario: JS-driven accent affordances follow the theme
- **WHEN** the rename-error outline or the spawn-failure button is shown while the dark (or any non-default-accent) theme is active
- **THEN** its color SHALL resolve from `var(--accent)` / `var(--on-accent)` and match the active theme, not a fixed light-theme hex

#### Scenario: Mobile Select indicator uses the accent via a class
- **WHEN** the user activates the mobile Select toggle
- **THEN** the button SHALL gain a `.sel-active` class whose color and border resolve from `var(--accent)`, and deactivating SHALL remove the class — with no inline color written from JavaScript and no second hue introduced

#### Scenario: Selection overlay tracks the live terminal palette
- **WHEN** the mobile text-selection overlay is shown
- **THEN** its foreground and background SHALL resolve from `--terminal-fg` / `--terminal-bg` (set at runtime to the active xterm theme) via the `#selectOverlay` CSS rule, with no hardcoded color in JavaScript

#### Scenario: Non-theme concrete colors are preserved
- **WHEN** the change is applied
- **THEN** the xterm color palette and the accent-picker swatch hex values SHALL remain unchanged, since they are intentionally fixed and not theme-derived
