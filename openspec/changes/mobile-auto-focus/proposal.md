# Proposal: Mobile Auto-Focus Input Bar

## Summary

Auto-focus the mobile input bar (`#mobileInput`) when a session is opened on a mobile device. Currently, users must manually tap the input field before they can begin typing, which adds unnecessary friction to the mobile experience.

## Motivation

On mobile devices, every extra tap adds latency and degrades the user experience. When a user opens or switches to a session, their intent is almost always to type a command. Automatically focusing the input bar eliminates one tap per session open and aligns mobile behavior with desktop expectations where the terminal is immediately ready for input.

## Scope

- Detect mobile viewport or mobile input bar visibility.
- Programmatically focus `#mobileInput` when `openSession()` completes and the mobile input bar is visible.
- Ensure the virtual keyboard opens reliably on iOS Safari and Android Chrome.
- Avoid auto-focus when the user is performing a non-typing action (e.g., scrolling session list).

## Affected Files

| File | Change Type |
|------|-------------|
| `dashboard/web/static/js/views.js` | Modified — `openSession` function gains focus call |
| `dashboard/web/templates/layout.html` | Modified — add `autofocus` attribute as progressive enhancement |

## Risks

- iOS Safari restricts programmatic focus unless triggered by a user gesture. The focus call must be chained inside the tap/click event handler that opens the session.
- Auto-focus may be undesirable if the user is using an external Bluetooth keyboard with a tablet — but this is an edge case and does not block the change.

## Decision

Proceed with implementation.
