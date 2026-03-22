# Proposal: Keyboard Shortcuts

## Summary

Add keyboard shortcuts to the dashboard for power-user workflows: Ctrl+T (open new session modal), Ctrl+W (close current tab), Ctrl+1/2/3 (switch to tab by index), and Ctrl+\ (toggle split view).

## Motivation

Power users managing multiple terminal sessions spend significant time clicking between tabs and reaching for buttons. Keyboard shortcuts reduce context switches and keep hands on the keyboard, which is the primary interaction mode for terminal users.

## Scope

- Register a global `keydown` listener for the defined shortcuts.
- Shortcuts use `Ctrl` as the modifier on all platforms (not `Cmd` on macOS) to avoid conflicting with browser-level shortcuts.
- Ctrl+T: open the "new session" modal (same as clicking the "+" button).
- Ctrl+W: close the currently active tab (with confirmation if the session is still running).
- Ctrl+1 through Ctrl+9: switch to the Nth open tab (1-indexed). If the index exceeds the tab count, do nothing.
- Ctrl+\: toggle split view on/off.
- Shortcuts are suppressed (no-op) when a modal dialog is open or when an `<input>` / `<textarea>` other than the terminal is focused.

## Affected Files

| File | Change Type |
|------|-------------|
| `dashboard/web/static/js/views.js` | Modified — add keydown listener and shortcut dispatch |

## Risks

- Ctrl+W is intercepted by most browsers to close the browser tab. The `keydown` handler can call `e.preventDefault()`, but some browsers do not allow pages to override Ctrl+W. This shortcut may only work reliably when the dashboard is installed as a PWA or run in Electron. Document this limitation.
- Ctrl+T is also a browser shortcut (new tab). Same caveat applies.

## Decision

Proceed with implementation. Document browser override limitations in the UI (tooltip or help modal).
