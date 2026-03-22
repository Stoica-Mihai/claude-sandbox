# Design: Mobile Auto-Focus Input Bar

## Overview

This change modifies the `openSession` function in `views.js` to programmatically call `.focus()` on `#mobileInput` when the mobile input bar is visible. A matching `autofocus` attribute is added to the HTML template as a progressive enhancement fallback.

## Approach

### 1. Focus call in `openSession` (views.js)

At the end of the `openSession` function, after the session is fully initialized and the DOM is updated:

```
if (isMobileViewport()) {
    document.getElementById('mobileInput').focus();
}
```

The `isMobileViewport()` check uses the existing responsive breakpoint logic (media query match or element visibility check on `#mobileInput`).

**Critical:** The `.focus()` call MUST remain within the synchronous call stack of the user's tap event. If `openSession` performs asynchronous work (e.g., WebSocket connection), the focus call must be placed before the first `await` or inside a `requestAnimationFrame` callback chained from the original gesture.

### 2. Autofocus attribute (layout.html)

Add `autofocus` to the `#mobileInput` element:

```html
<input id="mobileInput" type="text" autofocus ... />
```

This provides a no-JS fallback and improves first-load behavior.

### 3. Viewport detection

Reuse existing mobile detection. If none exists, check visibility of `#mobileInput`:

```
function isMobileViewport() {
    const el = document.getElementById('mobileInput');
    return el && el.offsetParent !== null;
}
```

## Edge Cases

- **iOS Safari gesture requirement:** `.focus()` only opens the keyboard if called during a user gesture. The implementation keeps the call synchronous within the tap handler.
- **Tab switching:** The same focus logic applies when switching tabs, not just opening new sessions.
- **Split view:** If split view is active on a tablet, the mobile input bar may be hidden; the visibility check prevents spurious focus.

## Testing Strategy

- Manual test on iOS Safari (iPhone) and Android Chrome.
- Verify keyboard opens on session tap.
- Verify no focus on desktop viewport.
