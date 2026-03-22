## Context

The Claude sandbox container currently runs Claude Code sessions accessible only via `docker exec -it`. There is no web-based interface, no port exposure, and no session management beyond manual process tracking. The container uses `sleep infinity` as its entrypoint and relies entirely on shell access.

Claude Code tracks active sessions in `~/.claude/sessions/*.json` (containing PID, session ID, cwd, timestamp). It supports full TUI features (slash commands, autocomplete, colored output) through a proper TTY. The container already runs with `--dangerously-skip-permissions` aliased.

The dashboard will sit behind an external auth proxy — no authentication is needed in the app itself.

Go 1.26.1 is already installed in the container for other tooling. The sibling project `tunnel-hub` uses the same stack (Go + HTMX + Tailwind + DaisyUI + SSE) successfully — this dashboard follows the same patterns.

## Goals / Non-Goals

**Goals:**
- Serve a web dashboard from inside the container for viewing and managing Claude Code sessions
- Provide full interactive terminal access via the browser (identical to native terminal experience)
- Allow spawning new sessions in any directory under `/workspace`
- Support session detach/reattach (survive browser disconnects)
- Keep the solution simple — single Go binary with embedded assets, server-rendered HTML with HTMX
- Match the UI quality and patterns of tunnel-hub (Tailwind + DaisyUI, dark/light theme, SSE real-time updates)

**Non-Goals:**
- Authentication or authorization (handled by external proxy)
- Multi-container orchestration or clustering
- Session persistence across container restarts (sessions are ephemeral)
- Modifying Claude Code itself or its configuration at runtime
- File editing or IDE features — this is a session manager, not a code editor
- JavaScript SPA framework — HTMX handles all dynamic behavior

## Decisions

### D1: Go + standard library for the web server
**Choice:** Go `net/http` with `gorilla/websocket` for WebSocket support and `creack/pty` for PTY management. All static assets embedded into the binary via `go:embed`.

**Rationale:** Go is already installed in the container (1.26.1). A single compiled binary with embedded assets is the simplest deployment model — no runtime dependencies, no `node_modules`, no package manager. `creack/pty` is the standard Go PTY library. `gorilla/websocket` is mature and widely used. The standard `net/http` router is sufficient for the handful of endpoints needed. This matches the tunnel-hub architecture exactly.

**Alternatives considered:**
- Node.js + Express: Would work (Node is also installed), but adds runtime dependency management. A Go binary is self-contained and lighter at runtime.
- Chi/Echo/Gin framework: Unnecessary for ~5 routes. Standard `net/http` with `http.ServeMux` is enough.

### D2: HTMX + SSE for frontend interactivity
**Choice:** Server-rendered HTML templates with HTMX attributes for dynamic updates. Session list updates pushed via Server-Sent Events (SSE) using HTMX SSE extension — when a session is spawned, killed, or changes state, the server pushes an SSE event that triggers HTMX to fetch updated HTML fragments. Session spawn/kill via `hx-post`/`hx-delete`, directory picker via `hx-get` with `hx-target` swaps.

**Rationale:** SSE is simpler than WebSocket for one-directional server→client updates and matches the tunnel-hub pattern exactly. HTMX keeps all logic server-side in Go templates. No JavaScript build step, no frontend state management, no API serialization layer. The server returns HTML fragments and HTMX swaps them into the DOM.

**Alternatives considered:**
- HTMX polling (`hx-trigger="every 2s"`): Simpler but wasteful — sends requests even when nothing changed. SSE pushes only when state changes.
- React/Vue SPA: Massive overkill for a session list and terminal.

### D3: Tailwind CSS + DaisyUI v4 for styling
**Choice:** Tailwind CSS and DaisyUI v4 loaded from CDN. DaisyUI provides pre-built components (cards, badges, buttons, modals) and a dark/light theme system via `data-theme` attribute with localStorage persistence.

**Rationale:** Matches the tunnel-hub UI stack exactly — consistent look and feel across projects. DaisyUI eliminates the need to write custom component CSS. The theme toggle is trivial with DaisyUI's `data-theme` attribute. CDN loading avoids a Tailwind build step.

**Alternatives considered:**
- Custom CSS: More work, inconsistent with tunnel-hub.
- Pico CSS: Simpler but less flexible, no component library.
- Vendored Tailwind: Requires a build step (PostCSS/CLI) to purge unused classes. CDN is acceptable since the container has internet access for Claude API calls anyway.

### D4: xterm.js for the browser terminal
**Choice:** xterm.js with `xterm-addon-fit` for auto-resizing and `xterm-addon-web-links` for clickable URLs. Connected to the Go backend via native WebSocket (not HTMX's SSE or WebSocket extension).

**Rationale:** xterm.js is the industry standard browser terminal emulator (used by VS Code, GitHub Codespaces, Jupyter). It handles all escape sequences, colors, and TUI rendering that Claude Code relies on. HTMX's SSE/WebSocket extensions are designed for HTML fragment swapping, not raw binary terminal data — xterm.js needs its own direct WebSocket connection.

**Alternatives considered:**
- HTMX WebSocket extension: Not suitable for raw terminal I/O, only HTML fragment swaps.
- hterm: Less actively maintained, smaller ecosystem.

### D5: PTY-per-session with detach support
**Choice:** Each spawned session gets its own `os/exec.Cmd` + `creack/pty` instance managed by a Go session manager. PTY processes survive WebSocket disconnects. A scrollback ring buffer (last 10,000 lines) is kept in memory per session for reattach replay.

**Rationale:** Users will switch between sessions and may lose browser connections. Killing the Claude session on disconnect would be destructive. The ring buffer ensures reattached clients see recent context without querying Claude Code's JSONL transcripts.

**Alternatives considered:**
- tmux wrapper: Adds complexity and another dependency. Direct PTY management is cleaner.
- No reattach: Simpler but frustrating UX when connections drop.

### D6: Embedded static assets via go:embed
**Choice:** Vendor htmx.js and xterm.js (+addons) into the Go project's `static/` directory. Embed them into the binary using `//go:embed static/*`. Serve via `http.FileServer`. Tailwind CSS and DaisyUI loaded from CDN (same as tunnel-hub). HTMX SSE extension loaded from CDN.

**Rationale:** Core interactivity (HTMX, xterm.js) is vendored for reliability. Styling (Tailwind/DaisyUI) from CDN avoids a build step and is acceptable since the container needs internet for Claude API calls. This matches tunnel-hub's approach: vendor HTMX, CDN for Tailwind/DaisyUI.

**Alternatives considered:**
- Vendor everything: Larger binary, more maintenance for CSS framework updates.
- CDN for everything: Risks breakage if CDN is down during HTMX load.

### D7: Entrypoint modification
**Choice:** Replace `command: sleep infinity` with the Go dashboard binary. The HTTP server's `ListenAndServe` keeps the container alive.

**Rationale:** Simpler than running two processes. If the dashboard dies, the container stops — correct behavior since the dashboard is now the primary interface. `docker exec` still works for direct shell access.

**Alternatives considered:**
- Process manager (supervisord, s6): Heavier dependency for managing just one process.
- Background the server + keep sleep: Fragile, harder to get logs from.

### D8: Session discovery — global scope via ~/.claude/sessions/
**Choice:** Discover all active sessions by reading `~/.claude/sessions/*.json`. This is a global directory where Claude Code registers every running session regardless of project/directory scope. Each file (`<pid>.json`) contains `{pid, sessionId, cwd, startedAt}`. The `cwd` field tells us which directory the session belongs to — no need to scan project-specific paths under `~/.claude/projects/` (those are historical transcripts, not active indicators).

Combine with a second source — managed sessions (PTYs spawned through the dashboard, tracked in-memory with full terminal access). Merge by PID: when a dashboard-spawned session also appears in `~/.claude/sessions/`, they become one entry with both terminal access and Claude's session metadata.

**Rationale:** `~/.claude/sessions/` is the single source of truth for active sessions across all directories. Users may still use `docker exec` to start sessions — these appear as "external" (detected-only, no terminal access). The merge-by-PID approach avoids duplicates and gives the richest metadata for dashboard-spawned sessions.

**Key insight:** Claude Code session files are NOT project-scoped for active session tracking. The `~/.claude/sessions/` directory is global. The `cwd` field in each file identifies the directory, so the dashboard can display sessions from all `/workspace` subdirectories in a single list.

**Alternatives considered:**
- Scanning `~/.claude/projects/*/` for session JSONL files: These are transcripts of past conversations, not indicators of running sessions. Would require parsing timestamps and cross-referencing PIDs anyway.
- Using `ps aux | grep claude`: Less reliable, no session metadata (UUID, cwd), and parsing process arguments is fragile.

### D10: Three view modes — client-side layout switching
**Choice:** Three view modes (single, split, grid) toggled via header buttons, managed entirely in client-side JS. View mode persists in localStorage. The sidebar and SSE connection remain constant across all modes — only the main content area changes.

- **Single**: One xterm.js terminal at full width, tab bar for switching between open sessions.
- **Split**: Two xterm.js terminals side by side with a draggable divider. Left pane assigned by click, right pane by Shift+click or right-click. Each pane has its own WebSocket connection.
- **Grid**: Session cards with mini terminal previews in a responsive 2-column grid. Clicking a card switches to single mode and opens that terminal.

**Rationale:** View modes are purely a layout concern — no server involvement needed. The server serves the same session data regardless of view mode. Keeping this client-side avoids unnecessary round-trips and keeps the HTMX fragments simple (they only render the sidebar session list, not the terminal layout).

**Alternatives considered:**
- Server-rendered view modes: Would require the server to know the current layout, adding complexity for no benefit.
- Tabs only (no split): Simpler but limits productivity when monitoring multiple sessions.

### D11: SSE pub/sub for session state changes
**Choice:** Go session manager publishes state change events (session spawned, killed, exited) to a pub/sub channel. Each SSE client subscribes to this channel. On state change, the server sends an SSE `update` event, which triggers HTMX to re-fetch the session list fragment. Matches the tunnel-hub pattern: `state.Subscribe()` / `state.Unsubscribe()` with buffered channels.

**Rationale:** Decouples session lifecycle from HTTP handlers. Multiple browser tabs receive updates simultaneously. Buffered channel (size 1) coalesces rapid state changes to avoid flooding clients.

**Alternatives considered:**
- Polling: Simpler but wasteful — sends requests on a timer even when nothing changed.
- Full WebSocket for UI: Overkill when SSE + HTMX handles it declaratively.

## Risks / Trade-offs

- **[Go binary build time]** Building the Go binary adds time to the Docker image build. → Mitigation: Go build is fast (~5-10s for a small project). The Dockerfile already installs Go, so no additional tooling needed.

- **[Memory usage]** Each PTY scrollback buffer (10K lines) uses ~1-5MB. With many concurrent sessions, memory could grow. → Mitigation: Cap at 10K lines, configurable via env var. Sessions are expected to be few (< 10 concurrent).

- **[PTY orphans]** If the dashboard crashes, managed PTY processes become orphans. → Mitigation: Container uses `init: true` in docker-compose (tini), which reaps orphaned processes. On restart, the dashboard shows detected sessions from `~/.claude/sessions/` for any survivors.

- **[No HTTPS]** The dashboard serves over plain HTTP. → Mitigation: Sits behind an external auth proxy that handles TLS termination.

- **[CDN dependency for styling]** Tailwind and DaisyUI from CDN won't load in air-gapped environments. → Mitigation: Acceptable trade-off — the container requires internet access for Claude API calls anyway. If air-gap support is needed later, vendor the CSS.

- **[xterm.js still requires JS]** While HTMX eliminates the need for a JS framework, xterm.js is still JavaScript. This is unavoidable — there is no HTML-only terminal emulator. The JS is minimal: initialize xterm, connect WebSocket, handle resize.

- **[xterm.js font measurement]** xterm.js uses hidden DOM elements to measure character widths for its monospace grid. Any CSS rule using the `*` universal selector that sets `font-family` will override these measurement elements, causing misaligned character rendering. The UI font (Outfit) MUST be applied via `body` selector, not `*`. Terminal fonts MUST be system monospace fonts (Menlo, Consolas, etc.) — web fonts loaded asynchronously from Google Fonts cause grid measurement mismatches because xterm.js measures before the font loads.

- **[WebSocket message types]** xterm.js `onData()` returns strings. Sending them via `ws.send(string)` produces a WebSocket TextMessage, but the Go handler reserves TextMessage for JSON control messages (resize). Terminal input MUST be encoded to binary via `TextEncoder` before sending, so it arrives as a BinaryMessage that the server writes directly to the PTY.

## Open Questions

- Should the scrollback buffer size be configurable via environment variable or is 10K lines sufficient as a fixed default?
