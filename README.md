# Claude Sandbox

Docker-based sandbox for running [Claude Code](https://claude.ai/code) in an isolated container with pre-configured MCP servers and plugins.

## Features

- Claude Code with `--dangerously-skip-permissions` by default — the container is the sandbox
- Optional MCP server support via `mcp-config.json`
- Pre-installed plugins: superpowers, skill-creator, claude-api, document-skills, example-skills, cli-anything, cli-anything-go
- Mounts your project workspace at `/workspace`
- Host Claude session/auth files mapped into container

## Prerequisites

- **Docker** and **Docker Compose** installed
- **Claude Code** installed and logged in on the host — the container mounts your auth files (`~/.claude.json` and `~/.claude/.credentials.json`) to authenticate

## Setup

1. Clone the repo:
   ```bash
   git clone git@github.com:Stoica-Mihai/claude-sandbox.git
   cd claude-sandbox
   ```

2. Make sure you've logged into Claude Code on the host at least once so the auth files exist:
   ```bash
   # These files must exist before starting the container:
   # ~/.claude.json
   # ~/.claude/.credentials.json
   ```

3. Build and start:
   ```bash
   make up
   ```
   This auto-generates `.env` with your UID/GID and starts the container.

## Usage

```bash
make shell    # Open a bash shell
make claude   # Run Claude Code
make down     # Stop the container
```

Or use docker directly:
```bash
docker exec -it claude_workspace bash
docker exec -it claude_workspace claude
docker compose down
```

## Configuration

| File | Purpose |
|------|---------|
| `.env` | Host UID/GID and optional settings (gitignored) |
| `docker-compose.yml` | Service definition, volume mounts, env passthrough |
| `container-settings.json` | Claude Code settings (plugins, permissions) |
| `mcp-config.json` | User-provided MCP server definitions (gitignored) |
| `mcp-config.example.json` | Example MCP server config template |

## License

See [LICENSE](LICENSE).
