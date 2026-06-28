# futurism-design-system Specification

## Purpose
The Futurism visual language applied to the dashboard chrome — square corners, 2px ink borders, solid offset shadows, a single accent, bold italic display type, fast directional motion, and skewed CTAs — plus a user-selectable accent color, expressed entirely in self-contained CSS tokens.

## Requirements
### Requirement: Futurism visual language for the dashboard
The dashboard chrome SHALL be styled with the Futurism design system, expressed entirely in self-contained CSS tokens (no third-party CSS framework). All surfaces and controls SHALL use square corners (`border-radius: 0`), 2px solid "ink" borders, and solid offset shadows (`Npx Npx 0`) with NO blur. A single accent color SHALL carry links, primary actions, focus outlines, status indicators, and (in the dark base) the offset shadow; no second hue SHALL be introduced for differentiation. Display headings SHALL be heavy (weight 900), italic, with negative tracking and tight line-height; body and secondary text SHALL be regular weight and calm. Primary action buttons SHALL be skewed (`skewX(-8deg)`) with their label counter-skewed so it reads upright. Motion SHALL be fast and directional (slide/dart/lurch), never spring/bounce, and SHALL be disabled under `prefers-reduced-motion`.

#### Scenario: Surfaces use square corners and solid offset shadows
- **WHEN** a card, modal, button, input, or session row is rendered
- **THEN** it SHALL have `border-radius: 0`, a 2px solid border, and any elevation SHALL be a non-blurred solid offset shadow

#### Scenario: Single accent drives interactive color
- **WHEN** the dashboard renders links, the primary/Launch/Resume action, focus outlines, and live status dots
- **THEN** they SHALL be colored by the single `--accent` token, with no additional accent hue introduced

#### Scenario: Primary actions are skewed with upright labels
- **WHEN** a primary action button (e.g. New, Launch, Resume) is rendered
- **THEN** the button SHALL be skewed `-8deg` and its text counter-skewed `+8deg` so the label stays upright

#### Scenario: Reduced motion is honored
- **WHEN** the user has `prefers-reduced-motion: reduce` set
- **THEN** the dashboard SHALL disable Futurism animations and transitions

### Requirement: User-selectable accent color
The dashboard SHALL provide an accent color picker in the header, separate from the theme toggle. It SHALL be a single trigger swatch (showing the current accent) that opens a popover containing 7 accents — Red, Amber, Lime, Cyan, Blue, Violet, and Pink — each defined with a distinct dark-base and light-base variant. Selecting an accent SHALL set the `--accent` CSS variable to the variant matching the active theme base and, in the dark base, SHALL set `--shadow` to the same color (the light base keeps the ink shadow). The chosen accent SHALL persist in `localStorage` and be restored on reload. Changing the theme SHALL re-resolve the current accent to the new base's variant. The theme toggle control itself SHALL NOT take the accent color.

#### Scenario: Choosing an accent recolors the dashboard
- **WHEN** the user opens the accent picker and selects an accent
- **THEN** `--accent` SHALL update to that accent's variant for the active theme, the dark-base offset shadow SHALL follow the accent, and the picker SHALL close

#### Scenario: Accent persists across reloads
- **WHEN** the user has selected an accent and reloads the dashboard
- **THEN** the previously chosen accent SHALL be restored from `localStorage`

#### Scenario: Accent re-resolves on theme change
- **WHEN** the user toggles between dark and light while an accent is selected
- **THEN** the accent SHALL switch to that accent's variant for the new theme base

#### Scenario: Theme toggle is accent-independent
- **WHEN** any accent is active
- **THEN** the theme toggle control SHALL render with the neutral ink color, not the accent

#### Scenario: Accent picker is compact on mobile
- **WHEN** the dashboard is viewed on a mobile-width viewport
- **THEN** the accent picker SHALL render as the single trigger swatch plus popover (never an inline row of 7 swatches), keeping the header uncluttered, and the popover swatches SHALL be touch-sized
