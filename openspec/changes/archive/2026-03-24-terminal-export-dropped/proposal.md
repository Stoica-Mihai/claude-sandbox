# Proposal: Terminal Scrollback Export

## Summary

Add a download button that exports the current terminal session's scrollback buffer as a plain-text `.txt` file. The button appears in the controls bar (desktop) and the mobile input bar (mobile).

## Motivation

Users frequently need to save terminal output for documentation, bug reports, or sharing. Currently they must manually select and copy text, which is error-prone (especially for long scrollback) and loses content that has scrolled out of the visible viewport on some terminals. A one-click export provides a reliable, complete capture of the session output.

## Scope

### Frontend
- Add a download/export button to the controls bar and the mobile input bar.
- On click, either: (a) extract the scrollback buffer from the xterm.js instance client-side and trigger a browser download, or (b) request the buffer from a new server endpoint.
- File naming convention: `session-{terminalId}-{timestamp}.txt`.

### Backend
- Add a new HTTP endpoint: `GET /api/sessions/{terminalId}/export`.
- The endpoint reads the session's scrollback buffer and returns it as `text/plain` with a `Content-Disposition: attachment` header.
- Returns 404 if the session does not exist.

## Affected Files

| File | Change Type |
|------|-------------|
| `dashboard/handlers.go` | Modified — add `handleExport` handler for new endpoint |
| `dashboard/web/templates/layout.html` | Modified — add export button to controls bar and mobile input bar |
| `dashboard/web/static/js/views.js` | Modified — add click handler for export button |

## Risks

- Large scrollback buffers (100k+ lines) may cause a brief UI freeze if extracted client-side. The server-side endpoint mitigates this.
- The scrollback buffer in xterm.js may not contain the full history if the terminal's `scrollback` option is limited.

## Decision

Proceed with implementation using the server-side endpoint as the primary mechanism, with client-side extraction as a fallback if the endpoint is unreachable.
