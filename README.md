# Claude Sandbox

Docker-based sandbox for running [Claude Code](https://claude.ai/code) in an isolated container with pre-configured MCP servers, plugins, and a web dashboard for session management.

## Features

- **Debian bookworm-slim** base with Go, uv (Python), npm, and common CLI tools
- Claude Code with `--dangerously-skip-permissions` by default — the container is the sandbox
- **Web dashboard** at `:8080` for managing sessions from a browser
  - View, spawn, resume, and kill Claude Code sessions
  - Full interactive terminal via xterm.js, bridged over WebSocket to the session host
  - One terminal at a time, switched from the sidebar session list (no tab strip)
  - Detach/reattach with an exact terminal snapshot (scrollback, colors, modes)
  - Self-contained Futurism design system (no CSS-framework CDN); dark/light theme toggle + 7-color accent picker
  - Responsive mobile layout with touch input bar
  - Real-time session updates via Server-Sent Events
- **Sessions survive backend rebuilds** — claude processes live in a dedicated `sessions` container (`sessiond`); editing dashboard/API code under `make watch` never kills your running sessions
- Pre-configured for **Opus 1M** with high effort, always-thinking, and agent teams enabled
- Optional MCP server support via `mcp-config.json`
- [OpenSpec](https://openspec.dev/) for spec-driven planning
- Pre-installed plugins: superpowers, skill-creator, claude-api, document-skills, example-skills, cli-anything, cli-anything-go, opsx-ext, caveman
- Mounts your project workspace at `/workspace` (configurable via `WORKSPACE_PATH`)
- Isolated Claude auth/config in `~/.claude-sandbox` on the host — your real `~/.claude` is never mounted

## Prerequisites

- **Docker** and **Docker Compose** installed

The sandbox keeps its own Claude auth/config in `~/.claude-sandbox` on the host, isolated from your real `~/.claude`. You authenticate once inside the sandbox (below) — no host login is mounted.

## Setup

1. Clone the repo:
   ```bash
   git clone git@github.com:Stoica-Mihai/claude-sandbox.git
   cd claude-sandbox
   ```

2. Build and start:
   ```bash
   make up
   ```
   This auto-generates `.env` with your UID/GID, creates `~/.claude-sandbox`, builds all services (sessions, backend, frontend, holesail), and starts them.

3. Open the dashboard at `http://localhost:8080`, spawn a session, and **log in once** when Claude prompts (open the OAuth URL, paste the code back). The token persists in `~/.claude-sandbox` for this sandbox only.

## Usage

```bash
make up               # Auto-generate .env, build and start all services
make shell            # Open a bash shell in the sessions container
make watch            # Rebuild the touched service on source change
make down             # Stop the containers
make rebuild          # Full rebuild with no cache
make restart-sessions # Rebuild the session host (ENDS running sessions)
make restart-backend  # Rebuild the API (sessions keep running)
make restart-frontend # Rebuild the dashboard UI
make restart-holesail # Rebuild the share-tunnel sidecar
```

The `Makefile` is the authoritative list of targets.

Create and manage Claude Code sessions from the dashboard — direct CLI `claude`
inside the container is disabled.

Or use docker directly:
```bash
docker exec -it claude_sessions bash
docker compose down
```

To mount a different workspace directory, set `WORKSPACE_PATH` in `.env`:
```bash
WORKSPACE_PATH=/path/to/your/project
```

## Dashboard

The web dashboard is available at `http://localhost:8080` after starting the container. It provides:

- **Session list** — sidebar showing all running Claude Code sessions (managed and external)
- **Terminal** — full xterm.js terminal with WebSocket relay for interactive sessions
- **Sidebar-driven session switching** — the sidebar lists every running session; click one to show it in the single terminal view (kill/rename inline)
- **Directory picker** — browse `/workspace` and spawn sessions in any subdirectory; a "+ NEW PROJECT…" row creates a folder at the current browse depth (optional `git init`, on by default) and immediately launches a session in it
- **Mobile support** — responsive layout with slide-out drawer and touch input bar
- **Theme toggle** — dark/light mode with localStorage persistence, plus a separate accent-color picker (7 colors, persisted)
- **Settings editor** — header gear opens a modal to edit the container's Claude prefs (model, effort, thinking, language, advisor) — saved to `container-settings.json`, applied to new sessions
- **Real-time updates** — SSE pushes session changes to all connected clients

The dashboard has no built-in authentication — it's designed to sit behind an auth proxy.

## Share tunnel (remote access)

The **Sharing** tab in the settings modal (header gear) toggles a secure [Holesail](https://holesail.io) peer-to-peer tunnel to the dashboard — no port forwarding, no static IP:

1. Open Settings → **Sharing** → **GO PUBLIC**. After a few seconds the panel shows a QR code and an `hs://` connection string (the logo glows while you're public).
2. On a phone, scan the QR with the **Holesail Go** app (iOS/Android) — the dashboard opens over the tunnel, terminals included.
3. On another machine: `npx holesail <connection-string>`, then browse the local port it prints.

Security model:

- **The connection string is the only credential.** Anyone who has it gets full dashboard access — treat it like a password. **REGENERATE** (↻ in the panel) rotates the key; the old string stops working immediately.
- The string is stable across restarts (the key lives in the `holesail-share-key` volume), but the tunnel always **boots private** — a host reboot or container restart un-shares until you toggle again.
- Share controls are rejected over the tunnel itself, so a remote visitor can't rotate or kill your tunnel.

## Configuration

| File | Purpose |
|------|---------|
| `.env` | Host UID/GID and `WORKSPACE_PATH` (gitignored, auto-generated) |
| `docker-compose.yml` | Service definition, volume mounts, port 8080, env passthrough |
| `container-settings.json` | Claude Code settings (model, plugins, permissions) — gitignored, seeded from the example by setup |
| `container-settings.example.json` | Baseline container settings template (committed) |
| `mcp-config.json` | User-provided MCP server definitions (gitignored) |
| `mcp-config.example.json` | Example MCP server config template |
| `sessiond/` | Go session host — owns each claude PTY + terminal state, serves a unix-socket protocol; sessions survive backend restarts |
| `backend/` | Go API + WebSocket↔sessiond bridge (holds no terminal state) |
| `frontend/` | Go dashboard UI + reverse proxy to the backend |
| `holesail/` | Node share-tunnel sidecar (P2P remote access, control API) |

## License

See [LICENSE](LICENSE).
