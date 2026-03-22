# Design: Keyboard Shortcuts

## Overview

Add a global `keydown` event listener in `views.js` that dispatches keyboard shortcut actions for tab management and view toggling. Shortcuts are guarded by modal-visibility and focus-target checks to prevent interference with normal text input.

## Approach

### 1. Shortcut registration (views.js)

Add a `registerKeyboardShortcuts()` function called once on DOMContentLoaded:

```javascript
function registerKeyboardShortcuts() {
    document.addEventListener('keydown', handleShortcut);
}

function handleShortcut(e) {
    // Guard: skip if modal is open
    if (document.querySelector('.modal.active, .modal.show, dialog[open]')) {
        return;
    }

    // Guard: skip if focus is in a non-terminal input
    const tag = document.activeElement?.tagName;
    const id = document.activeElement?.id;
    if ((tag === 'INPUT' || tag === 'TEXTAREA') && id !== 'mobileInput') {
        return;
    }

    if (!e.ctrlKey || e.altKey || e.metaKey) return;

    switch (e.key) {
        case 't':
            e.preventDefault();
            openNewSessionModal();
            break;
        case 'w':
            e.preventDefault();
            closeCurrentTab();
            break;
        case '\\':
            e.preventDefault();
            toggleSplitView();
            break;
        default:
            // Ctrl+1 through Ctrl+9
            if (e.key >= '1' && e.key <= '9') {
                e.preventDefault();
                switchToTab(parseInt(e.key, 10) - 1);
            }
    }
}
```

### 2. Action functions

Each shortcut delegates to an existing function or a thin wrapper:

- `openNewSessionModal()` — triggers the same logic as the "+" button click.
- `closeCurrentTab()` — identifies the active tab, optionally shows a confirmation dialog if the session is running, then closes.
- `switchToTab(index)` — looks up the tab list, clicks the tab at `index` if it exists, otherwise no-op.
- `toggleSplitView()` — calls the existing split-view toggle logic.

### 3. Browser override caveats

`e.preventDefault()` is called for all matched shortcuts, but browsers may still intercept Ctrl+T and Ctrl+W before the page handler fires. This is a known platform limitation. The shortcuts will work reliably in:
- Standalone PWA mode
- Electron/Tauri wrappers
- Some Chromium-based browsers that allow override

A tooltip on the tab bar or a help modal (Ctrl+?) can document available shortcuts and caveats.

## Edge Cases

- **Multiple modifiers:** If Shift, Alt, or Meta is also held, the shortcut is ignored (the guard checks `e.altKey` and `e.metaKey`).
- **Focus in terminal:** xterm.js captures most keystrokes, but Ctrl+key combinations not bound by xterm pass through to the document listener. If xterm binds any of these combos, the xterm binding takes precedence.
- **Zero tabs open:** Ctrl+W and Ctrl+1..9 are no-ops. Ctrl+T still opens the modal. Ctrl+\ is a no-op with fewer than 2 tabs.

## Testing Strategy

- Unit test `handleShortcut` with synthetic KeyboardEvent objects.
- Manual test each shortcut in Chrome and Firefox.
- Verify guards by focusing a text input and pressing Ctrl+T — should type normally, not open modal.
