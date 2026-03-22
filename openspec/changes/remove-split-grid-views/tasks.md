## 1. Remove from layout.html

- [ ] 1.1 Remove the view mode toggle buttons (single/split/grid join group) from the header
- [ ] 1.2 Remove the split hint banner
- [ ] 1.3 Remove Shift+Click and view mode switching instructions from welcome screen (desktop and mobile)
- [ ] 1.4 Remove the entire `#viewSplit` container (both panes, divider, headers, status bars)
- [ ] 1.5 Remove the entire `#viewGrid` container

## 2. Remove from views.js

- [ ] 2.1 Remove state variables: splitLeftTerminalId, splitRightTerminalId, spawnToRightPane, savedSplitLeft, savedSplitRight
- [ ] 2.2 Simplify setView() to only handle 'single' — remove all split/grid branches, split state saving/restoring, grid building
- [ ] 2.3 Simplify openSession() to only call openSessionSingle() — remove split/grid switch cases
- [ ] 2.4 Remove functions: openSessionInPane, openSessionRight, closeSplitPane, updateSplitPaneHeader, initSplitDivider, buildGridView, maximizeSplitPane, sendKeyToPane
- [ ] 2.5 Simplify cleanupKilledSession() to only handle single tabs — remove split pane cleanup
- [ ] 2.6 Simplify openNewSessionModal() — remove spawnToRightPane flag
- [ ] 2.7 Simplify HTMX afterRequest listener — remove spawnToRightPane branch
- [ ] 2.8 Remove Ctrl+\ keyboard shortcut from handleShortcuts()
- [ ] 2.9 Remove Shift+click handling from session card click delegation
- [ ] 2.10 Remove initSplitDivider() call from DOMContentLoaded
- [ ] 2.11 Simplify updateSessionCardStates() — remove in-split class logic
- [ ] 2.12 Remove split terminal references from pull-to-refresh guards

## 3. Remove from sessions.html

- [ ] 3.1 Remove oncontextmenu handler from session cards

## 4. Remove from style.css

- [ ] 4.1 Remove .session-card.in-split styles (dark and light mode)
- [ ] 4.2 Remove .split-divider styles
- [ ] 4.3 Remove .terminal-pane styles
- [ ] 4.4 Remove .grid-card styles (dark and light mode)

## 5. Cleanup related OpenSpec changes

- [ ] 5.1 Delete the view-polish change (split ratio persistence is now irrelevant)

## 6. Verify

- [ ] 6.1 Go build passes
- [ ] 6.2 Dashboard loads on desktop with only single view, no view toggle visible
- [ ] 6.3 Dashboard loads on mobile — no split/grid references
- [ ] 6.4 Sessions open as tabs, close properly, sidebar highlights update correctly
