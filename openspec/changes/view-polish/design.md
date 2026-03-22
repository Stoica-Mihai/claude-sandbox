## Context

The dashboard split view uses a draggable divider implemented in `initSplitDivider()` in `views.js`. The drag handler calculates a left-pane percentage (clamped 20-80%) and applies it via inline `flex` styles, but the value is never persisted. On reload, the panes revert to the CSS default (`flex-1` each, i.e., 50/50).

Grid view cards are built dynamically by `buildGridView()` in `views.js`. Managed session cards currently render a static dark box with "Click to open terminal" text. Each managed session has a `RingBuffer` on the server holding the last ~10K lines of raw terminal output (including ANSI escape sequences), but this data is only served to WebSocket clients on reattach — there is no HTTP endpoint to read it.

## Goals / Non-Goals

**Goals:**
- Persist split divider ratio in localStorage so it survives reloads and view switches
- Show recent terminal output in grid view cards for managed sessions
- Keep the implementation simple — no new WebSocket connections for grid previews, just an HTTP fetch

**Non-Goals:**
- Live-updating grid previews via WebSocket or SSE (polling on grid rebuild is sufficient)
- Rendering ANSI colors in grid previews (plain text is enough for a small preview)
- Persisting split ratio on the server (localStorage is client-side only, which is fine)

## Decisions

### D1: localStorage key for split ratio
**Choice:** Store the split ratio under the key `splitRatio` in localStorage as a plain number string (e.g., `"62.5"`) representing the left pane's percentage width. The key name follows the existing convention (`viewMode` for view mode, `theme` for theme).

**Rationale:** A single numeric value is the simplest representation. The existing clamping logic (20-80%) applies on both save and restore, so stale or corrupted values are safely bounded. Using a percentage rather than pixel value means the ratio adapts to different window sizes.

**Alternatives considered:**
- Storing as a JSON object with both pane sizes: Unnecessary complexity — the right pane is always `100 - left`.
- Using a CSS custom property instead of inline flex: Would still need localStorage for persistence, adding a layer of indirection.

### D2: Grid preview via HTTP endpoint polling
**Choice:** Add a `GET /api/sessions/{terminalId}/preview` endpoint that returns the last N lines (default 8) of the session's scrollback buffer as plain text with ANSI escape codes stripped. `buildGridView()` fetches this endpoint for each managed session card when the grid is built or rebuilt (on view switch or SSE update).

**Rationale:** Grid view is an overview — it does not need real-time streaming. Fetching preview text on grid rebuild (triggered by SSE updates or view switch) provides sufficiently fresh content without the complexity of maintaining additional WebSocket or SSE connections per card. The HTTP endpoint is stateless and cacheable.

**Alternatives considered:**
- WebSocket per grid card: Heavy — would require N concurrent connections for N sessions, just for a small text preview.
- SSE with preview data embedded in the event: Would couple the SSE event format to grid-specific data, and most SSE consumers (the session list fragment) don't need preview text.
- Fetching on a timer (polling every few seconds): Wasteful when the user is not in grid view. Rebuilding on SSE update is event-driven and already happens.

### D3: Max preview lines — 8 lines
**Choice:** The preview endpoint returns at most 8 lines of text. The grid card preview area has a fixed height (`h-28`, roughly 112px) and uses `overflow-hidden` to clip. At `text-[10px]` with `leading-relaxed`, 8 lines fit comfortably.

**Rationale:** 5 lines felt too sparse for a preview; 8 lines fills the card height without overflow. The server truncates to 8 lines to keep the response small, and the fixed-height card clips cleanly if line wrapping causes extra visual lines.

### D4: ANSI stripping on the server
**Choice:** The preview endpoint strips ANSI escape codes on the server side before returning plain text. This is done with a simple regex (`\x1b\[[0-9;]*[A-Za-z]` and related CSI/OSC sequences) applied to the raw scrollback bytes.

**Rationale:** Sending raw ANSI to the client and stripping in JS would work but adds unnecessary payload size and client-side complexity. The server already has the raw bytes; stripping there is a single `regexp.ReplaceAll` call. Plain text is also easier to render safely (no risk of injecting HTML from escape sequences).

**Alternatives considered:**
- Client-side ANSI stripping: More work in JS, larger payloads, and the stripped result is the same.
- Rendering ANSI colors in the preview: Would require a mini terminal renderer or inline styles for each color span. Overkill for a tiny preview card.

### D5: Save ratio on mouseup, restore on split view init
**Choice:** Save the split ratio to localStorage in the existing `mouseup` handler (after `isDragging` is confirmed). Restore the ratio in `setView('split')` by reading localStorage and applying the flex styles before terminal resize.

**Rationale:** Saving on every `mousemove` would be excessive (hundreds of writes per drag). Saving on `mouseup` captures the final position. Restoring in `setView` ensures the ratio is applied whether the user switches to split via button click or page load.

## Risks / Trade-offs

- **[Stale grid previews]** Grid previews are only fetched when the grid is built (view switch or SSE update). If a session produces output between rebuilds, the preview is stale until the next SSE event. This is acceptable — the grid is an overview, not a live monitor.

- **[N+1 HTTP requests on grid build]** Building the grid fires one preview fetch per managed session. With many sessions (e.g., 10), this is 10 small HTTP requests. Acceptable for the expected session count (< 10). If it becomes a concern, a batch endpoint (`GET /api/previews?ids=a,b,c`) could consolidate them.

- **[ANSI stripping edge cases]** The regex-based ANSI stripper may miss unusual escape sequences (OSC hyperlinks, sixel graphics). For the purpose of a small text preview, partial stripping is acceptable — the preview is not a terminal emulator.
