## REMOVED Requirements

### Requirement: Three view modes — Single, Split, Grid
**Reason**: Split and grid views introduce excessive state management complexity and are the source of most view transition bugs.
**Migration**: Single terminal view with tabs is the only view mode. Users open multiple sessions as tabs.

### Requirement: Split ratio persistence
**Reason**: No split view means no split ratio to persist.
**Migration**: None needed.

## MODIFIED Requirements

### Requirement: Single terminal view with tabs
The dashboard SHALL display only a single terminal view with a tab bar. There SHALL be no view mode toggle, no split view, and no grid view. Clicking a session in the sidebar opens it as a new tab or switches to its existing tab. The header SHALL not contain view mode buttons.

#### Scenario: Only single view available
- **WHEN** the dashboard loads
- **THEN** only the single terminal view with tabs SHALL be displayed, with no view mode toggle in the header

#### Scenario: Session opens as tab
- **WHEN** the user clicks a session in the sidebar
- **THEN** it SHALL open as a new tab (or switch to existing tab) in the single terminal view — no split or grid option
