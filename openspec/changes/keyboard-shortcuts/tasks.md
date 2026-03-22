# Tasks: Keyboard Shortcuts

## Task List

- [ ] 1.1 Implement `handleShortcut(e)` function in `views.js` with modal-open guard and non-terminal input guard
- [ ] 1.2 Add Ctrl+T handler to invoke `openNewSessionModal()` with `preventDefault()`
- [ ] 1.3 Add Ctrl+W handler to invoke `closeCurrentTab()` with confirmation prompt if session is running
- [ ] 1.4 Add Ctrl+1 through Ctrl+9 handler to invoke `switchToTab(index)` — no-op if index exceeds tab count
- [ ] 1.5 Add Ctrl+\ handler to invoke `toggleSplitView()` — no-op if fewer than 2 tabs open
- [ ] 1.6 Implement `registerKeyboardShortcuts()` and call it on DOMContentLoaded in `views.js`
- [ ] 1.7 Verify xterm.js does not capture the chosen Ctrl+key combinations; adjust bindings if conflicts exist
- [ ] 1.8 Manual test all shortcuts in Chrome — verify correct actions fire
- [ ] 1.9 Manual test all shortcuts in Firefox — verify correct actions fire
- [ ] 1.10 Manual test guard: focus a non-terminal input, press Ctrl+T — verify no modal opens
- [ ] 1.11 Manual test guard: open a modal, press Ctrl+W — verify no tab closes
