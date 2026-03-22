# Tasks: Terminal Scrollback Export

## Task List

- [ ] 1.1 Implement `handleExport` handler in `dashboard/handlers.go` — read scrollback buffer, set `Content-Type` and `Content-Disposition` headers, return plain text
- [ ] 1.2 Handle 404 for unknown `terminalId` and 405 for non-GET methods in `handleExport`
- [ ] 1.3 Register `GET /api/sessions/{terminalId}/export` route in the HTTP mux
- [ ] 2.1 Add export button (`#exportBtn`) to the desktop controls bar in `dashboard/web/templates/layout.html`
- [ ] 2.2 Add export button (`#mobileExportBtn`) to the mobile input bar in `dashboard/web/templates/layout.html`
- [ ] 2.3 Style both export buttons consistently with existing control buttons
- [ ] 3.1 Implement `exportSession()` function in `views.js` — fetch from export endpoint, trigger blob download
- [ ] 3.2 Implement `extractScrollback(term)` fallback in `views.js` — read xterm.js buffer line by line
- [ ] 3.3 Implement `downloadBlob(blob, filename)` utility in `views.js`
- [ ] 3.4 Attach click handlers to `#exportBtn` and `#mobileExportBtn` calling `exportSession()`
- [ ] 3.5 Enable/disable export buttons based on active session state in `openSession`, `switchTab`, and `closeTab`
- [ ] 4.1 Write unit tests for `handleExport` — valid session, missing session, wrong method
- [ ] 4.2 Manual test: export a session with scrollback, verify file contents match terminal output
- [ ] 4.3 Manual test: click export with no active session, verify button is disabled
- [ ] 4.4 Manual test: kill server, click export, verify client-side fallback downloads file
