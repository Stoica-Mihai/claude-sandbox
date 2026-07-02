# Claude Sandbox

Docker-based sandbox for running [Claude Code](https://claude.ai/code) in an isolated container with pre-configured MCP servers, plugins, and a web dashboard for session management.

## Features

- **Debian bookworm-slim** base with Go, uv (Python), npm, and common CLI tools
- Claude Code with `--dangerously-skip-permissions` by default — the container is the sandbox
- **Web dashboard** at `:8080` for managing sessions from a browser
  - View, spawn, and kill Claude Code sessions
  - Full interactive terminal via xterm.js with WebSocket relay
  - Tabbed terminals — open multiple sessions as tabs
  - Session detach/reattach with scrollback replay
  - Self-contained Futurism design system (no CSS-framework CDN); dark/light theme toggle + 7-color accent picker
  - Responsive mobile layout with touch input bar
  - Real-time session updates via Server-Sent Events
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
   This auto-generates `.env` with your UID/GID, creates `~/.claude-sandbox`, builds backend + frontend, and starts them.

3. Open the dashboard at `http://localhost:8080`, spawn a session, and **log in once** when Claude prompts (open the OAuth URL, paste the code back). The token persists in `~/.claude-sandbox` for this sandbox only.

## Usage

```bash
make up        # Auto-generate .env, build and start backend + frontend
make shell     # Open a bash shell in the backend container
make watch     # Rebuild/sync on source change
make down      # Stop the containers
make rebuild   # Full rebuild with no cache
```

Create and manage Claude Code sessions from the dashboard — direct CLI `claude`
inside the container is disabled.

Or use docker directly:
```bash
docker exec -it claude_backend bash
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
- **Tabbed terminals** — open multiple sessions as tabs, switch between them
- **Directory picker** — browse `/workspace` and spawn sessions in any subdirectory; a "+ NEW PROJECT…" row creates a folder at the current browse depth (optional `git init`, on by default) and immediately launches a session in it
- **Mobile support** — responsive layout with slide-out drawer and touch input bar
- **Theme toggle** — dark/light mode with localStorage persistence, plus a separate accent-color picker (7 colors, persisted)
- **Settings editor** — header gear opens a modal to edit the container's Claude prefs (model, effort, thinking, language, advisor) — saved to `container-settings.json`, applied to new sessions
- **Real-time updates** — SSE pushes session changes to all connected clients

The dashboard has no built-in authentication — it's designed to sit behind an auth proxy.

## Configuration

| File | Purpose |
|------|---------|
| `.env` | Host UID/GID and `WORKSPACE_PATH` (gitignored, auto-generated) |
| `docker-compose.yml` | Service definition, volume mounts, port 8080, env passthrough |
| `container-settings.json` | Claude Code settings (model, plugins, permissions) — gitignored, seeded from the example by setup |
| `container-settings.example.json` | Baseline container settings template (committed) |
| `mcp-config.json` | User-provided MCP server definitions (gitignored) |
| `mcp-config.example.json` | Example MCP server config template |
| `backend/` | Go API + session manager (dtach sessions, relay, WebSocket) |
| `frontend/` | Go dashboard UI + reverse proxy to the backend |

## License

See [LICENSE](LICENSE).
