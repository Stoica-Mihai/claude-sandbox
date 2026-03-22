## Context

The dashboard sidebar shows session cards with a status dot, PID, duration, CWD, and a text label ("active", "external", "stopped"). All alive managed sessions look identical — there is no way to distinguish a session that is actively producing output from one that has been idle for 30 minutes. Sessions are identified only by their directory basename, which is ambiguous when multiple sessions target the same directory.

## Goals / Non-Goals

**Goals:**
- Make it immediately obvious which sessions have recent output activity
- Let users assign meaningful names to sessions
- Keep the implementation simple with no persistence beyond process lifetime

**Non-Goals:**
- Persisting session names to disk or across restarts
- Tracking activity for external (non-managed) sessions
- Adding real-time per-second updates to the sidebar (SSE already refreshes on state changes; that cadence is sufficient)

## Decisions

### D1: Activity tracking via PTY read timestamp
**Choice:** Update a `LastActivity time.Time` field on `ManagedSession` inside the existing `readPTY` goroutine, on every successful read (inside the `if n > 0` block). The field is protected by a new `sync.RWMutex` or by reusing the existing `wsMu` lock.

**Rationale:** The `readPTY` goroutine already runs for the lifetime of each session and processes every byte of output. Adding a `time.Now()` call there is essentially free. There is no need for a separate polling goroutine or heartbeat mechanism. The timestamp is read when `ListSessions` builds `DisplaySession` objects, which only happens on SSE-triggered refreshes — not on a hot path.

**Alternatives considered:**
- Tracking activity via WebSocket message counts — rejected because activity should be tracked even when no browser is connected.
- Using a channel/ticker to debounce timestamp updates — rejected as unnecessary; `time.Now()` is cheap and the mutex contention is negligible given the read frequency.

### D2: Name storage in-memory only
**Choice:** Store the session name as a `Name string` field on `ManagedSession`. No file persistence, no database. Names are lost when the dashboard process restarts.

**Rationale:** The dashboard already stores all managed session state in-memory (the `managed map[string]*ManagedSession`). Adding a string field is consistent with the existing architecture. Persistence would require either a file or a database, adding complexity for a feature that is cosmetic. Sessions themselves are ephemeral (they die on container restart), so name persistence provides little value.

### D3: Pulse animation on card border
**Choice:** Add a `pulse-output` CSS class applied to session cards when `LastActivity` is within the last 5 seconds. The animation is a subtle glow on the card's left border (the existing accent border), fading from bright emerald to the normal border color over ~1.5 seconds. This is applied server-side in the template using a computed boolean field (`RecentActivity bool`) on `DisplaySession`.

**Rationale:** The existing `pulse-alive` animation is on the small status dot and indicates process liveness. The output pulse needs to be visually distinct — applying it to the card border gives a larger, more noticeable signal without adding new UI elements. Computing the "recent" boolean server-side in `ListSessions` keeps the template simple (just a class toggle) and avoids client-side timers.

**CSS approach:**
```css
@keyframes pulse-output {
    0%, 100% { border-left-color: oklch(0.7 0.15 160); }
    50% { border-left-color: oklch(0.8 0.2 160); box-shadow: -2px 0 8px oklch(0.7 0.15 160 / 0.3); }
}
.pulse-output {
    animation: pulse-output 1.5s ease-in-out infinite;
}
```

### D4: DisplayName field for template simplicity
**Choice:** Compute a `DisplayName string` field in `ListSessions` that resolves to `Name` if set, otherwise `DirName`. Templates and JS use `DisplayName` everywhere instead of conditional logic.

**Rationale:** Avoids scattering `{{if .Name}}{{.Name}}{{else}}{{.DirName}}{{end}}` throughout templates and JS. The `data-session` attribute on cards already drives tab bar and pane header labels — switching it to use `DisplayName` is a single-point change.

### D5: Rename via PUT endpoint, not inline edit
**Choice:** Expose a `PUT /api/sessions/:terminalId/name` endpoint. The initial UI trigger can be a simple button or link on the card that opens a small prompt/modal. Inline double-click editing is a stretch goal.

**Rationale:** A REST endpoint is straightforward to implement and test. It decouples the rename action from the UI mechanism — the same endpoint works for a modal, an inline edit, or even a CLI/API call. The SSE publish after rename ensures all connected clients see the update.
