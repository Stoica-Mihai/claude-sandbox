## 1. Split ratio persistence

- [ ] 1.1 In `initSplitDivider()` mouseup handler in `views.js`, save the final left-pane percentage to `localStorage.setItem('splitRatio', clamped)` after confirming `isDragging` was true
- [ ] 1.2 In `setView('split')` in `views.js`, read `localStorage.getItem('splitRatio')` and if present, apply the saved percentage to the left and right pane flex styles (`flex: 0 0 N%` / `flex: 0 0 (100-N)%`) before calling `TerminalManager.resizeAll()`
- [ ] 1.3 Verify that if no `splitRatio` exists in localStorage, the panes default to `flex-1` (50/50)

## 2. Scrollback preview endpoint

- [ ] 2.1 Add a helper function in `ringbuffer.go` (or in `handlers.go`) that takes the ring buffer bytes, strips ANSI escape codes via regex, splits into lines, and returns the last N lines as a string
- [ ] 2.2 Register `GET /api/sessions/{terminalId}/preview` route in `handlers.go` that calls `sm.Get(terminalId)`, reads `Scrollback.Bytes()`, strips ANSI, extracts the last 8 lines, and returns them as `text/plain`
- [ ] 2.3 Handle edge cases: session not found (404), empty scrollback (return empty string), session is external/detected-only (404 since no ring buffer)

## 3. Grid view preview rendering

- [ ] 3.1 Update `buildGridView()` in `views.js` to fetch `GET /api/sessions/{terminalId}/preview` for each managed (non-external) session card
- [ ] 3.2 Replace the static "Click to open terminal" placeholder with the fetched preview text rendered as monospace `text-[10px]` lines in the dark preview box, or "No output yet" if the response is empty
- [ ] 3.3 Keep the external session placeholder unchanged ("External session -- no terminal access")
- [ ] 3.4 Ensure `buildGridView()` is called on SSE-triggered session list updates (already happens via `setView` flow) so previews refresh when sessions change

## 4. Verify and test

- [ ] 4.1 Verify the Go code builds cleanly with the new endpoint and any new helper functions
- [ ] 4.2 Test split ratio persistence: drag divider, reload page, confirm ratio is restored
- [ ] 4.3 Test split ratio persistence across view switches: drag divider, switch to single, switch back to split, confirm ratio is restored
- [ ] 4.4 Test grid view previews: spawn a session, produce some output, switch to grid view, confirm the card shows recent output lines
- [ ] 4.5 Test grid view with empty session: spawn a new session, immediately switch to grid, confirm "No output yet" placeholder appears
- [ ] 4.6 Test grid view with external session: confirm external cards still show the existing placeholder
