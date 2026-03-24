## Context

The dashboard currently has no resilience against WebSocket disconnections and no infrastructure-level health monitoring. Terminals go dead silently on network hiccups, and Docker cannot detect when the dashboard process is unhealthy.

The relay architecture already supports reconnection well: tmux sessions survive WebSocket disconnects, relays persist, and `AddViewer` replays the ring buffer to new connections. The missing piece is client-side — the browser needs to reconnect instead of giving up.

## Goals / Non-Goals

**Goals:**
- Automatically recover from temporary WebSocket disconnections without user intervention
- Give the user clear visual feedback when a connection is being re-established
- Enable Docker to detect and restart unhealthy containers

**Non-Goals:**
- Buffering user input during disconnection
- Persisting session state across container restarts (sessions are ephemeral)
- Adding WebSocket ping/pong keep-alive (TCP keep-alive and browser behavior are sufficient)

## Decisions

### D1: Exponential backoff — 1s base, 30s cap, 10 retries
**Choice:** Start at 1s, double on each failure (1s, 2s, 4s, 8s, 16s, 30s, 30s...), cap at 30s, give up after 10 consecutive failures. Only reconnect on abnormal close (code != 1000). Normal close (code 1000, "session ended") shows "[Session ended]" as before.

**Rationale:** 1s initial delay recovers quickly from brief glitches. Doubling prevents hammering during extended outages. 30s cap keeps it responsive. 10 retries spans ~2.5 minutes — enough for a container restart. Distinguishing normal vs abnormal close avoids reconnecting to a session that has genuinely ended.

**Alternatives considered:**
- Fixed interval: Doesn't adapt to outage duration.
- Infinite retries: Hammers a server that will never come back.
- Reconnect on all closes: Would repeatedly try to reconnect to ended sessions.

### D2: Health check — GET /healthz + Docker healthcheck
**Choice:** Add `GET /healthz` returning `{"status":"ok"}` with HTTP 200. Docker healthcheck: `curl -f http://localhost:${DASHBOARD_PORT:-8080}/healthz` with 10s interval, 5s timeout, 3 retries, 15s start period.

**Rationale:** Lightweight dedicated endpoint is the standard pattern. JSON body makes it debuggable with curl. `curl` is already in the container image. The `DASHBOARD_PORT` variable ensures the healthcheck uses the configured port.

**Alternatives considered:**
- Check `/` (dashboard page): Heavier — renders templates, queries sessions.
- TCP check (`nc -z`): Only verifies port is open, not that HTTP is serving.

### D3: Reconnection indicator — inline terminal text
**Choice:** Write status messages into xterm.js buffer: dim gray "Reconnecting... (attempt N)" on each retry, green "Reconnected" on success, red "Connection lost" after all retries exhausted.

**Rationale:** Uses existing `term.write()` API, no extra DOM elements. Matches the existing "[Session ended]" and "[Connection error]" patterns. Messages appear inline in the terminal context.

## Risks / Trade-offs

- **[Scrollback duplication]** On reconnect, the server replays the full ring buffer via `AddViewer`. The terminal already has content from before disconnect, so the user sees duplicated output. Acceptable — tracking what was already sent would add significant ring buffer complexity.
- **[Health check port]** The healthcheck uses `${DASHBOARD_PORT:-8080}`. If the port variable and the actual listen port diverge, the health check fails. Mitigated by the port configuration design (env var is the single source of truth).
