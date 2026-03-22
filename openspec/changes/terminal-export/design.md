# Design: Terminal Scrollback Export

## Overview

Add a server-side endpoint and a frontend button to export a terminal session's scrollback buffer as a downloadable `.txt` file. The client-side includes a fallback that extracts the buffer from xterm.js if the server endpoint is unavailable.

## Approach

### 1. Backend endpoint (handlers.go)

Register a new route in the HTTP mux:

```
GET /api/sessions/{terminalId}/export
```

Handler implementation:

```go
func handleExport(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    terminalId := extractTerminalId(r) // from URL path
    session, ok := activeSessions[terminalId]
    if !ok {
        http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
        return
    }

    buffer := session.GetScrollbackBuffer()

    filename := fmt.Sprintf("session-%s.txt", terminalId)
    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
    w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
    w.Write([]byte(buffer))
}
```

`GetScrollbackBuffer()` reads from the PTY's recorded output or a ring buffer maintained by the session. The exact source depends on the existing session architecture.

### 2. Frontend button (layout.html)

Add an export button to the controls bar:

```html
<button id="exportBtn" class="control-btn" title="Export terminal output" disabled>
    <svg><!-- download icon --></svg>
</button>
```

Add a matching button to the mobile input bar:

```html
<button id="mobileExportBtn" class="mobile-control-btn" title="Export" disabled>
    <svg><!-- download icon --></svg>
</button>
```

Both buttons are disabled by default and enabled when a session tab is active.

### 3. Click handler (views.js)

```javascript
async function exportSession() {
    const terminalId = getActiveTerminalId();
    if (!terminalId) return;

    try {
        const resp = await fetch(`/api/sessions/${terminalId}/export`);
        if (!resp.ok) throw new Error(resp.statusText);
        const blob = await resp.blob();
        downloadBlob(blob, `session-${terminalId}-${Date.now()}.txt`);
    } catch (err) {
        // Fallback: extract from xterm.js
        const term = getActiveTerminal();
        const buffer = extractScrollback(term);
        const blob = new Blob([buffer], { type: 'text/plain' });
        downloadBlob(blob, `session-${terminalId}-${Date.now()}.txt`);
    }
}

function downloadBlob(blob, filename) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
}

function extractScrollback(term) {
    const lines = [];
    const buffer = term.buffer.active;
    for (let i = 0; i < buffer.length; i++) {
        lines.push(buffer.getLine(i).translateToString());
    }
    return lines.join('\n');
}
```

### 4. Button state management

In the existing `openSession` / `switchTab` / `closeTab` logic, enable or disable the export buttons:

```javascript
document.getElementById('exportBtn').disabled = !getActiveTerminalId();
document.getElementById('mobileExportBtn').disabled = !getActiveTerminalId();
```

## Edge Cases

- **Empty scrollback:** The endpoint returns an empty 200 response. The client downloads an empty file.
- **Very large scrollback:** The server streams the buffer. For extremely large buffers, consider chunked transfer encoding in a future iteration.
- **Concurrent close:** If the session closes between the button click and the fetch response, the endpoint returns 404, triggering the client-side fallback (which may also fail if xterm.js is destroyed). In that case, show a toast notification: "Session ended before export completed."

## Testing Strategy

- Unit test `handleExport` with valid session, missing session, and wrong HTTP method.
- Manual test: open a session, run commands, click export, verify downloaded file contents.
- Manual test: disconnect server, click export, verify client-side fallback works.
