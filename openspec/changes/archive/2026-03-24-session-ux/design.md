## Context

The dashboard sidebar shows session cards with a status dot, duration, CWD, and "active"/"stopped" label. All alive sessions look identical — there is no way to distinguish a session actively producing output from one idle for 30 minutes. Sessions are identified only by directory basename, which is ambiguous. The relay architecture (`relay.go`) handles all I/O via pipe-pane + socat — the `broadcast()` method is called on every output chunk, making it the natural instrumentation point for activity tracking.

## Goals / Non-Goals

**Goals:**
- Make it immediately obvious which sessions have recent output activity
- Let users assign meaningful names to sessions
- Keep the implementation simple with no persistence beyond process lifetime

**Non-Goals:**
- Persisting session names to disk or across restarts
- Adding real-time per-second updates to the sidebar (SSE refreshes every 5s via poller; that cadence is sufficient)
- Inline double-click rename (stretch goal, not required)

## Decisions

### D1: Activity tracking via relay broadcast timestamp
**Choice:** Add a `lastActivity time.Time` field to the `Relay` struct, protected by a dedicated `lastActivityMu sync.RWMutex`. Update it with `time.Now()` in `broadcast()` on every output chunk. Add a `GetLastActivity() time.Time` getter for safe reads.

**Rationale:** `broadcast()` runs on every output chunk from the tmux pane and already iterates viewers. Adding `time.Now()` there is essentially free. The timestamp is read by `ListSessions` when building `DisplaySession` objects, which only happens on SSE-triggered refreshes — not a hot path. Activity is tracked even when no browser is connected (broadcast still runs, just no viewers to send to).

**Alternatives considered:**
- Tracking in `processOutput` instead of `broadcast`: functionally equivalent, but `broadcast` is the final step and only fires when there's actual cleaned output (not just alt-screen sequences).
- Using a channel/ticker to debounce: unnecessary; `time.Now()` is cheap.

### D2: Name storage in SessionManager map
**Choice:** Store session names in a `sessionNames map[string]string` on `SessionManager`, protected by the existing `mu sync.RWMutex`. Add `SetSessionName` and `GetSessionName` methods. Names are lost on restart.

**Rationale:** The relay shouldn't own display metadata. SessionManager already manages the session lifecycle. A simple map is consistent with the existing architecture. Sessions are ephemeral (die on container restart), so name persistence provides little value.

### D3: Pulse animation on card border
**Choice:** Add a `pulse-output` CSS class applied to session cards when `LastActivity` is within the last 5 seconds. The animation is a subtle glow on the card's left border, fading between two shades of emerald over ~1.5s. Computed server-side as a `RecentActivity bool` field on `DisplaySession`.

**Rationale:** The existing `pulse-alive` animation is on the status dot (indicates liveness). The output pulse needs to be visually distinct — applying it to the card border gives a larger signal without adding new UI elements. Computing the boolean server-side keeps the template simple (just a class toggle).

### D4: DisplayName field for template simplicity
**Choice:** Compute a `DisplayName string` field in `ListSessions` that resolves to custom name if set, otherwise `DirName`. Templates and JS use `DisplayName` via `data-session` attribute. Tab headers pick it up automatically.

**Rationale:** Avoids scattering conditional logic in templates. The `data-session` attribute already drives tab headers — updating it to `DisplayName` is a single-point change.

### D5: Rename via PUT endpoint
**Choice:** `PUT /api/sessions/{terminalId}/name` accepts `{"name":"..."}`. Empty name clears the custom name. Handler calls `SetSessionName` then publishes SSE event so all clients see the update.

**Rationale:** A REST endpoint is straightforward to implement and test. Decouples the rename action from the UI mechanism — works for a button, modal, or API call.

## Risks / Trade-offs

- **5-second activity granularity**: The poller refreshes every 5s. A session that produced output 4.9s ago shows "4s ago" but the pulse animation may have already expired on the next refresh. Acceptable for the dashboard use case.
- **No persistence**: Names are lost on restart. Acceptable since sessions are also lost on restart.
- **Race on lastActivity**: The relay's broadcast goroutine writes `lastActivity` while `ListSessions` reads it. The dedicated `lastActivityMu` prevents data races with minimal contention.
