# Claude Code Automation Recommendations

## Context

This sandbox runs Claude Code inside a Docker container with Go, Python/uv, Node.js, and 8 plugins pre-installed. MCP servers for GitLab and Jira exist but are workspace-level (not mounted via docker-compose). The host and container have **zero hooks, zero custom subagents, and zero custom skills** — significant untapped potential. The user works primarily with Go and Python in an enterprise GitLab/Jira environment.

---

## 1. MCP Servers

### 1a. Context7 — Live Documentation Lookup (HIGH)

Provides version-specific docs for Go stdlib, Python libraries, and any npm/pip packages. Eliminates outdated API suggestions.

**Container** — add to `container-settings.json` under `mcpServers`:
```json
"context7": {
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"]
}
```

**Dockerfile** — pre-install to avoid npx download delay:
```dockerfile
RUN npm install -g @upstash/context7-mcp
```

### 1b. Mount mcp-config.json into Container (HIGH)

Currently `mcp-config.json` (GitLab/Jira) is NOT volume-mounted. The container can't see these MCP servers unless they're loaded via a workspace `.mcp.json`.

**docker-compose.yml** — add volume:
```yaml
- ./mcp-config.json:/home/claude/.claude/mcp.json:ro
```

---

## 2. Plugins to Enable

### 2a. commit-commands (HIGH) — Host + Container

Adds `/commit` and `/commit-push-pr` slash commands for structured git workflows.

**Dockerfile** — add install line:
```dockerfile
claude plugin install commit-commands@claude-plugins-official && \
```

**container-settings.json** — add to `enabledPlugins`:
```json
"commit-commands@claude-plugins-official": true
```

**Host**:
```bash
claude plugin install commit-commands@claude-plugins-official
```

### 2b. gopls-lsp (MEDIUM) — Container

Gives Claude real Go compiler diagnostics and type info instead of best-effort inference.

**Dockerfile** — install gopls binary + plugin:
```dockerfile
# After Go install, before USER switch (or use go install as claude user)
RUN go install golang.org/x/tools/gopls@latest
ENV PATH="/home/claude/go/bin:${PATH}"
```

```dockerfile
claude plugin install gopls-lsp@claude-plugins-official && \
```

**container-settings.json**:
```json
"gopls-lsp@claude-plugins-official": true
```

### 2c. security-guidance (MEDIUM) — Host + Container

Flags security issues (injection, XSS, credential exposure) as Claude writes code. Low-friction safety net.

```bash
claude plugin install security-guidance@claude-plugins-official
```

---

## 3. Hooks

### 3a. Auto-format Go files with gofmt (HIGH)

Runs `gofmt -w` after any `.go` file edit. Zero-config Go formatting.

**Where**: `container-settings.json` (container) and/or `~/.claude/settings.json` (host)

```json
"hooks": {
  "PostToolUse": [
    {
      "matcher": "Edit|Write",
      "hooks": [
        {
          "type": "command",
          "command": "file=$(echo \"$CLAUDE_TOOL_INPUT\" | python3 -c \"import sys,json; print(json.load(sys.stdin).get('file_path',''))\"); [ \"${file##*.}\" = 'go' ] && gofmt -w \"$file\" 2>/dev/null || true",
          "timeout": 10000
        }
      ]
    }
  ]
}
```

### 3b. Auto-lint Python files with ruff (MEDIUM)

Runs `ruff check --fix` after `.py` file edits.

Add to the same `PostToolUse` array:
```json
{
  "matcher": "Edit|Write",
  "hooks": [
    {
      "type": "command",
      "command": "file=$(echo \"$CLAUDE_TOOL_INPUT\" | python3 -c \"import sys,json; print(json.load(sys.stdin).get('file_path',''))\"); [ \"${file##*.}\" = 'py' ] && ruff check --fix \"$file\" 2>/dev/null || true",
      "timeout": 10000
    }
  ]
}
```

**Dockerfile** — ensure ruff is available:
```dockerfile
RUN /home/claude/.local/bin/uv tool install ruff
```

### 3c. Block edits to secret/credential files (HIGH)

Prevents accidental edits to `.env`, credentials, keys, and `mcp-config.json`.

Add to `hooks` in settings:
```json
"PreToolUse": [
  {
    "matcher": "Edit|Write",
    "hooks": [
      {
        "type": "command",
        "command": "file=$(echo \"$CLAUDE_TOOL_INPUT\" | python3 -c \"import sys,json; print(json.load(sys.stdin).get('file_path',''))\"); echo \"$file\" | grep -qE '(\\.env$|\\.env\\.|credentials\\.json|\\.credentials|\\.pem$|\\.key$|mcp-config\\.json)' && echo 'BLOCKED: This file contains secrets. Ask user to edit manually.' && exit 2 || exit 0",
        "timeout": 5000
      }
    ]
  }
]
```

---

## 4. Custom Subagents

### 4a. Code Reviewer (HIGH)

Tailored to Go + Python stack. Run after completing implementation tasks.

**Where**: `~/.claude/agents/code-reviewer.md` (host) or copy into container via Dockerfile

```markdown
---
name: code-reviewer
description: Reviews code for bugs, security issues, and Go/Python best practices
tools: Glob, Grep, Read, Bash
model: sonnet
---

Review the specified code changes for:

**Go**: error handling (no ignored errors), goroutine safety, context usage, defer cleanup, gofmt compliance
**Python**: type hints, exception handling (no bare except), resource cleanup, import ordering
**Both**: hardcoded secrets, injection vectors, logging of sensitive data, missing input validation

Output each issue as: file:line | severity (Critical/High/Medium/Low) | description | suggested fix
Only report issues with >= 80% confidence.
```

### 4b. Docker Security Reviewer (MEDIUM)

Reviews Dockerfiles and compose files for security and optimization.

**Where**: `~/.claude/agents/docker-reviewer.md`

```markdown
---
name: docker-reviewer
description: Reviews Dockerfiles and compose configs for security and optimization
tools: Glob, Grep, Read
model: sonnet
---

Review Docker configurations for:
- Running as root, unnecessary privileges
- Secrets in build args or ENV
- Missing cleanup (apt lists, caches)
- Layer ordering and image size optimization
- Security_opt settings, volume mount permissions
- Health checks and signal handling

Output findings with severity, location, and fix.
```

---

## 5. Custom Skills

### 5a. MCP Server Development Skill (HIGH)

The user builds MCP servers. Codify the patterns from existing GitLab/Jira servers.

**Where**: `~/.claude/skills/mcp-dev/SKILL.md`

```markdown
---
name: mcp-dev
description: Build Python MCP servers using FastMCP and uv, following project patterns from GitLab/Jira integrations
---

## Project MCP Server Patterns
- Location: /workspace/Utilities/ai-skills/mcp/<name>/
- Runtime: Python via `uv run --project <dir> <dir>/main.py`
- Framework: FastMCP
- Registration: Add entry to mcp-config.json

## Development Steps
1. `mkdir -p /workspace/Utilities/ai-skills/mcp/<name>`
2. `cd` there, `uv init`, add dependencies (fastmcp, httpx, etc.)
3. Create `main.py` following existing server patterns
4. Test: `uv run --project . main.py`
5. Register in mcp-config.json
```

---

## Implementation Priority

| # | Item | Effort | Impact | Files to Modify |
|---|------|--------|--------|-----------------|
| 1 | Block secret file edits hook (3c) | Low | Critical safety | `container-settings.json`, `~/.claude/settings.json` |
| 2 | Context7 MCP server (1a) | Low | High | `container-settings.json`, `Dockerfile` |
| 3 | Mount mcp-config.json (1b) | Low | High | `docker-compose.yml` |
| 4 | gofmt hook (3a) | Low | High | `container-settings.json` |
| 5 | commit-commands plugin (2a) | Low | High | `Dockerfile`, `container-settings.json` |
| 6 | Code reviewer subagent (4a) | Low | High | Create `~/.claude/agents/code-reviewer.md` |
| 7 | MCP dev skill (5a) | Low | Medium | Create `~/.claude/skills/mcp-dev/SKILL.md` |
| 8 | ruff hook + install (3b) | Low | Medium | `container-settings.json`, `Dockerfile` |
| 9 | gopls-lsp plugin (2b) | Medium | Medium | `Dockerfile`, `container-settings.json` |
| 10 | security-guidance plugin (2c) | Low | Medium | `container-settings.json` |
| 11 | Docker reviewer subagent (4b) | Low | Medium | Create `~/.claude/agents/docker-reviewer.md` |

---

## Verification

After implementation:
1. `make rebuild && make claude` — verify container starts with new plugins visible (`/plugins list`)
2. Edit a `.go` file — verify gofmt runs automatically
3. Try to edit `.env` — verify hook blocks the edit
4. Run `/commit` — verify commit-commands plugin works
5. Use Context7 MCP — ask Claude to look up Go stdlib docs
6. Invoke code-reviewer subagent — verify it launches and reviews code
7. Check `mcp-config.json` MCP servers load — verify GitLab/Jira tools available in container
