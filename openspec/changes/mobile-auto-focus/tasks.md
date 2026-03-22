# Tasks: Mobile Auto-Focus Input Bar

## Task List

- [ ] 1.1 Add `isMobileViewport()` helper function to `views.js` (check `#mobileInput` visibility via `offsetParent`)
- [ ] 1.2 Modify `openSession()` in `views.js` to call `document.getElementById('mobileInput').focus()` when `isMobileViewport()` returns true, within the user-gesture call stack
- [ ] 1.3 Modify session tab-switch handler in `views.js` to call the same mobile focus logic
- [ ] 1.4 Add `autofocus` attribute to `#mobileInput` in `dashboard/web/templates/layout.html`
- [ ] 1.5 Manual test on iOS Safari — verify virtual keyboard opens on session open
- [ ] 1.6 Manual test on Android Chrome — verify virtual keyboard opens on session open
- [ ] 1.7 Manual test on desktop viewport — verify no spurious focus on `#mobileInput`
