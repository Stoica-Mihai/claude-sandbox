## MODIFIED Requirements

### Requirement: Split divider ratio persistence
The split view divider position SHALL be saved to localStorage whenever the user finishes dragging. When the user enters split view (via view toggle or page load with split as the stored view mode), the divider position SHALL be restored from localStorage. If no saved ratio exists, the default SHALL be 50/50.

#### Scenario: Drag divider and reload
- **WHEN** the user drags the split divider to a new position and then reloads the page
- **THEN** the split view SHALL restore the divider to the previously saved position, not the default 50/50

#### Scenario: Drag divider and switch views
- **WHEN** the user drags the split divider, switches to single or grid view, and then switches back to split view
- **THEN** the split view SHALL restore the divider to the last dragged position

#### Scenario: No saved ratio
- **WHEN** the user enters split view for the first time (no ratio in localStorage)
- **THEN** the split view SHALL use the default 50/50 ratio

### Requirement: Grid view terminal preview
In grid view, managed session cards SHALL display a preview of recent terminal output instead of a static placeholder. The preview SHALL show the last 5-8 lines of terminal output from the session's scrollback buffer, with ANSI escape codes stripped to plain text. External sessions SHALL continue to show the existing placeholder ("External session -- no terminal access").

#### Scenario: Grid card shows recent output
- **WHEN** the grid view is active and a managed session has terminal output in its scrollback buffer
- **THEN** the session's grid card SHALL display the most recent lines (up to 8) of plain-text terminal output in the mini preview area, styled as monospace text on a dark background

#### Scenario: Grid card with empty scrollback
- **WHEN** the grid view is active and a managed session has no scrollback content yet
- **THEN** the session's grid card SHALL display a placeholder such as "No output yet" in the preview area

#### Scenario: Grid view refreshes previews
- **WHEN** the user switches to grid view or the session list updates via SSE
- **THEN** the grid view SHALL rebuild and fetch fresh preview text for each managed session card

#### Scenario: External session in grid view
- **WHEN** the grid view displays an external (detected-only) session
- **THEN** the card SHALL continue to show the existing placeholder text, not a terminal preview
