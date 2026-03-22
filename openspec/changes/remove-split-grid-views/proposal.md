## Why

The split and grid view modes introduce significant state management complexity and are the source of most UI bugs (stale tabs, empty panes, state not persisting across view transitions). The single terminal view with tabs covers the primary use case. Split and grid can be re-added later when needed.

## What Changes

- Remove the view mode toggle (single/split/grid buttons) from the header
- Remove the split terminal view container and all split pane logic
- Remove the grid overview container and grid card builder
- Remove all split/grid state variables, functions, CSS, and keyboard shortcuts
- Remove Shift+click and right-click session card handlers for split pane
- Simplify `setView()` to only handle single view
- Clean up related dead code in session cards, welcome screen instructions, pull-to-refresh guards

## Capabilities

### Modified Capabilities
- `dashboard-ui`: Remove split and grid view modes, keep only single terminal with tabs

## Impact

- **layout.html**: Remove ~70 lines (view toggle, split container, grid container, split hint, split instructions in welcome screen)
- **views.js**: Remove ~350 lines (8 functions, split/grid branches, state variables, keyboard shortcut)
- **style.css**: Remove ~50 lines (split divider, grid card, in-split, terminal-pane styles)
- **sessions.html**: Remove right-click handler attribute
- No backend changes
