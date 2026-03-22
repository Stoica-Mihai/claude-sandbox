## Context

The dashboard currently has no resilience against WebSocket disconnections, no infrastructure-level health monitoring, and no cleanup of stale session files. These gaps accumulate into a degraded experience: terminals go dead silently, stale sessions clutter the sidebar, and Docker cannot detect when the dashboard process is unhealthy.

The existing server-side architecture already supports reconnection well: PTY processes survive WebSocket disconnects (detach mode), and the scrollback ring buffer is replayed to new connections. The missing piece is entirely client-side — the browser needs to know to reconnect instead of giving up.

## Goals / Non-Goals

**Goals:**
- Automatically recover from temporary WebSocket disconnections without user intervention
- Give the user clear visual feedback when a connection is being re-established
- Enable Docker to detect and restart unhealthy containers
- Keep the session list clean by removing files for dead processes

**Non-Goals:**
- Buffering user input during disconnection (too complex for the benefit, and the reconnect is typically fast)
- Persisting session state across container restarts (sessions are ephemeral by design)
- Adding WebSocket ping/pong keep-alive (the existing TCP keep-alive and browser behavior are sufficient)
- Monitoring PTY health separately from WebSocket health

## Decisions

### D1: Exponential backoff strategy — 1s base, 30s cap, 10 retries
**Choice:** Start reconnection attempts at 1 second delay, double on each failure (1s, 2s, 4s, 8s, 16s, 30s, 30s, ...), cap at 30 seconds, give up after 10 consecutive failures.

**Rationale:** A 1-second initial delay is fast enough to recover from brief network glitches without the user noticing much downtime. The doubling prevents hammering the server during extended outages. The 30-second cap ensures the user does not wait unreasonably long between attempts. 10 retries at this schedule spans approximately 2.5 minutes of total retry time — long enough to survive a container restart or brief network partition, short enough that the user is not left waiting indefinitely with false hope.

**Alternatives considered:**
- Fixed interval (e.g., 5s): Simple but either too aggressive for long outages or too slow for brief blips. Exponential backoff adapts to both.
- No cap: Could lead to minute-long delays between attempts, which feels unresponsive.
- Infinite retries: Could keep hammering a server that will never come back. A finite limit lets the user know and take action.

### D2: Health check endpoint — GET /healthz returning JSON
**Choice:** Add a `GET /healthz` endpoint to the Go HTTP server that returns `{"status": "ok"}` with HTTP 200. Configure docker-compose with `healthcheck: test: ["CMD", "curl", "-f", "http://localhost:8080/healthz"]` using a 10-second interval, 5-second timeout, 3 retries, and 15-second start period.

**Rationale:** A dedicated health endpoint is the standard pattern for container health checks. Returning JSON (not just an empty 200) makes it debuggable with `curl` during troubleshooting. Using `curl` in the health check command is straightforward and `curl` is already installed in the container image. The 15-second start period gives the Go binary time to start up and begin serving before the first check.

**Alternatives considered:**
- Checking `/` (the dashboard page): Works but is heavier — it renders templates, queries sessions, etc. A lightweight endpoint is more appropriate for a health probe.
- Using `wget` instead of `curl`: Either works. `curl` is already in the container and more commonly used in health checks.
- TCP-only check (`test: ["CMD", "nc", "-z", "localhost", "8080"]`): Only verifies the port is open, not that the HTTP server is actually processing requests.

### D3: Stale session cleanup in discoverSessions() — inline deletion
**Choice:** Delete stale session files inline during `discoverSessions()` when the PID is confirmed dead. No background goroutine; cleanup happens on every call to `ListSessions()`.

**Rationale:** `discoverSessions()` already reads every session file and checks PID liveness on every call (triggered by page loads and SSE-driven fragment refreshes). Adding an `os.Remove()` call for dead PIDs is negligible overhead on top of the existing filesystem reads and `syscall.Kill` checks. This approach is simple, requires no timers or goroutines, and naturally cleans up stale files whenever the session list is viewed.

**Alternatives considered:**
- Background goroutine with ticker: Adds concurrency complexity (mutex around file deletion, shutdown coordination) for no benefit. The inline approach already runs frequently enough.
- Cleanup on server startup only: Would miss stale files created during the server's lifetime (e.g., an external `claude` session that exits).
- Keep stale files but filter from display: Stale files would accumulate indefinitely, slowing down `ReadDir` over time and requiring the UI to always handle dead entries.

### D4: Reconnection indicator — xterm.js inline text, not DOM overlay
**Choice:** Write reconnection status messages directly into the xterm.js terminal buffer using ANSI escape codes (dim gray for "Reconnecting...", green for "Reconnected", red for "Connection lost"). No separate DOM overlay element.

**Rationale:** Writing to the terminal buffer is the simplest approach — it uses the existing `term.write()` API and requires no additional HTML/CSS. The messages appear inline with the terminal output, which is the natural place for connection status in a terminal context. This matches the existing pattern used for "[Session ended]" and "[Connection error]" messages.

**Alternatives considered:**
- DOM overlay (absolutely positioned div): More visually prominent but requires additional HTML structure, CSS positioning, and z-index management that interacts with xterm.js's own overlay system.
- Toast notification: Too subtle and disappears — the user needs to see the status persistently while reconnection is in progress.

## Risks / Trade-offs

- **[Scrollback duplication on reconnect]** When the WebSocket reconnects, the server replays the full scrollback buffer. If the terminal already has content from before the disconnect, the user will see duplicated output. This is the existing behavior for session reattach and is acceptable — the alternative (tracking what was already sent) adds significant complexity to the ring buffer.

- **[Race between cleanup and process startup]** If a new Claude Code process reuses a PID that was just cleaned up, the session file could be deleted before the new process writes its own. This is extremely unlikely (PID reuse in Linux cycles through the full PID space first) and harmless if it occurs (Claude Code will rewrite the file).

- **[Health check adds curl dependency]** The docker-compose health check uses `curl`, which must be installed in the container image. The Dockerfile already installs `curl` for other purposes, so this is not a new dependency.
