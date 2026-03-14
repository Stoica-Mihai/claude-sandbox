# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is a Docker-based sandbox for running Claude Code inside a container with pre-configured MCP servers, plugins, and permissions.

## Architecture

- **Dockerfile** — Alpine-based image with bash, git, curl, socat, bubblewrap, Go, uv (Python package manager), and Claude Code + plugins pre-installed. Runs as non-root user `claude`.
- **docker-compose.yml** — Defines the `claude-env` service. Maps host Claude session/auth files into the container and injects container-specific settings and MCP config as read-only volumes. Mounts the project workspace at `/workspace`.
- **container-settings.json** — Claude Code settings for the container environment (plugins, MCP servers, permissions). Mounted read-only at `/home/claude/.claude/settings.json`.
- **mcp-config.json** — User-provided MCP server definitions (gitignored). See `mcp-config.example.json`.
- **.env** — Host UID/GID for Docker build args (file permission compatibility).
- **Makefile** — Convenience targets for build, start, stop, and shell access.
- **generate-env.sh** — Creates `.env` from `.env.example` with auto-detected UID/GID.
- **.dockerignore** — Excludes non-build files from the Docker context.

## Common Commands

```bash
make up       # Auto-generate .env, build and start the container
make shell    # Open a bash shell in the container
make claude   # Run Claude Code in the container
make down     # Stop the container
```

The `claude` alias (defined in Dockerfile) expands to `claude --dangerously-skip-permissions` — the container itself is the sandbox.

## MCP Servers

MCP servers are configured via `mcp-config.json` (gitignored). Copy `mcp-config.example.json` to get started.

## Installed Plugins

The container comes with these Claude Code plugins pre-installed:
- `superpowers`, `skill-creator` (from claude-plugins-official)
- `claude-api`, `document-skills`, `example-skills` (from anthropic-agent-skills)
- `cli-anything` (from CLI-Anything)

## Notes

- The container creates a non-root user `claude` with the host UID/GID (from `.env`) to avoid file permission issues with mounted volumes.
- `container-settings.json` enables experimental agent teams (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`).
- Tokens in `mcp-config.json` and `container-settings.json` are plaintext — do not commit these to a shared remote.
