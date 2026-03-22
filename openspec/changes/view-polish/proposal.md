## Why

Two pieces of dashboard state are lost or missing:

1. **Split ratio resets on reload.** The user drags the split divider to their preferred position, but the ratio is not persisted. On any page reload or navigation, both panes snap back to 50/50. This is especially frustrating for users who keep a narrow monitoring pane on the right — they have to re-drag every time.

2. **Grid cards show no terminal content.** Grid view cards display a static "Click to open terminal" placeholder for managed sessions. There is no preview of what the session is actually doing, which defeats the purpose of a grid overview. Users must click into each card individually to see activity, negating the at-a-glance benefit of the grid layout.

## What Changes

- Persist the split divider ratio in localStorage and restore it when entering split view or reloading the page
- Add a scrollback text preview to grid view cards for managed sessions, showing the most recent 5-8 lines of terminal output from the session's ring buffer

## Capabilities

### Modified Capabilities
- `dashboard-ui`: Split ratio persistence in localStorage; grid cards show recent terminal output preview from scrollback buffer

## Impact

- **`dashboard/web/static/js/views.js`**: Save split ratio to localStorage on drag end, restore on split view init. Update `buildGridView()` to fetch and render scrollback preview text for each managed session card.
- **`dashboard/handlers.go`**: Add `GET /api/sessions/{terminalId}/preview` endpoint that reads the session's ring buffer, extracts the last N lines of plain text (ANSI codes stripped), and returns them as a text response.
- **`dashboard/ringbuffer.go`**: May add a helper method to extract the trailing N lines from the buffer as a string (or handle this in the handler).
- **`dashboard/web/templates/layout.html`**: No structural changes needed — the grid container is already built dynamically by JS.
- No new dependencies. No changes to WebSocket protocol, SSE, or session lifecycle.
