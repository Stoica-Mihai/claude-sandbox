## Why

The Claude sandbox container is currently accessible only via `docker exec`, making it impossible to manage multiple Claude Code sessions from a browser or integrate with other web-based tools. A web dashboard would allow users to view all running sessions at a glance, spawn new sessions targeting specific directories inside the container, and interact with them through a full terminal — all behind an existing auth proxy.

## What Changes

- Add a Go web server inside the container that exposes a session management dashboard
- Expose a port (8080) in `docker-compose.yml` for the web UI
- Serve Go HTML templates styled with Tailwind CSS + DaisyUI, using HTMX + SSE for real-time session list updates — no JavaScript framework
- Provide WebSocket-based interactive terminals (xterm.js + Go PTY) for full Claude Code CLI access including slash commands, autocomplete, and TUI features
- Add a server-rendered dashboard UI with dark/light theme toggle showing session status, directory, and duration
- Modify the container entrypoint to start the web server alongside the existing `sleep infinity`

## Capabilities

### New Capabilities
- `session-api`: HTTP handlers for listing, spawning, and managing Claude Code sessions inside the container. Reads `~/.claude/sessions/*.json` for active session discovery, spawns new sessions via PTY in user-specified directories.
- `web-terminal`: WebSocket-backed interactive terminal using xterm.js and Go `creack/pty`. Provides full PTY emulation so Claude Code's TUI, slash commands, and autocomplete work identically to a real terminal.
- `dashboard-ui`: Server-rendered HTMX-powered frontend styled with Tailwind CSS + DaisyUI for viewing all running sessions (PID, directory, duration, status) and spawning new ones with a directory picker scoped to `/workspace`. Session list auto-updates via SSE push. Dark/light theme toggle.

### Modified Capabilities

## Impact

- **Dockerfile**: Build the Go dashboard binary during image build. Embed static assets (HTMX, xterm.js) into the binary via `go:embed`.
- **docker-compose.yml**: Add port mapping (`8080:8080`). Modify entrypoint to run the dashboard server.
- **container-settings.json**: No changes needed — existing `--dangerously-skip-permissions` alias covers spawned sessions.
- **Security**: No auth in the dashboard itself (sits behind an external auth proxy). Should bind to `0.0.0.0` to be reachable from the host network.
- **Dependencies**: Go modules — `github.com/creack/pty`, `github.com/gorilla/websocket`. Frontend — `htmx.js` (vendored), HTMX SSE extension (CDN), `xterm.js` + addons (vendored), Tailwind CSS + DaisyUI v4 (CDN).
