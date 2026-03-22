## ADDED Requirements

### Requirement: Dashboard provides a button to export terminal scrollback as a text file
The dashboard SHALL display an export/download button in both the desktop controls bar and the mobile input bar. Clicking the button downloads the current session's scrollback buffer as a `.txt` file.

#### Scenario: User clicks export on desktop
- **WHEN** the user clicks the export button in the desktop controls bar and a session is currently active
- **THEN** a GET request is sent to `/api/sessions/{terminalId}/export` and the browser downloads the response as `session-{terminalId}-{timestamp}.txt`

#### Scenario: User clicks export on mobile
- **WHEN** the user taps the export button in the mobile input bar and a session is currently active
- **THEN** the same export flow is triggered as on desktop

#### Scenario: No active session
- **WHEN** no session tab is currently active
- **THEN** the export button SHALL be disabled or hidden

#### Scenario: Export endpoint unreachable — client-side fallback
- **WHEN** the GET request to the export endpoint fails (network error or non-200 response)
- **THEN** the client SHALL extract the scrollback buffer from the xterm.js instance and trigger a Blob download with the extracted text
