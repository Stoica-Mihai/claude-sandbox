# ── Builder stage: compile the Go dashboard binary ──
FROM debian:bookworm-slim AS builder

# Install Go 1.26.1 and build toolchain
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    gcc \
    libc6-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://go.dev/dl/go1.26.1.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

COPY dashboard/ /src/dashboard/
RUN cd /src/dashboard && go build -o /dashboard-server .

# ── Runtime stage ──
FROM debian:bookworm-slim

# Runtime packages — gcc/libc6-dev needed for Go race detector (go test -race uses CGO)
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    git \
    curl \
    socat \
    tmux \
    bubblewrap \
    qrencode \
    npm \
    ca-certificates \
    gcc \
    make \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain — needed for dev workflow (make dev) and go test -race
RUN curl -fsSL https://go.dev/dl/go1.26.1.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# UTF-8 locale for Unicode rendering (Claude Code banner, box-drawing chars)
ENV LANG=C.UTF-8

# Create a non-root user with matching host UID (Claude Code refuses --dangerously-skip-permissions as root)
ARG UID
ARG GID
RUN groupadd -g ${GID} claude && \
    useradd -m -u ${UID} -g claude claude
USER claude

# tmux configuration (no status bar, large scrollback, latest window-size for multi-client)
COPY --chown=claude:claude tmux.conf /home/claude/.tmux.conf

# Install Claude Code as the non-root user
RUN curl -fsSL https://claude.ai/install.sh | bash

# Install UV
RUN curl -LsSf https://astral.sh/uv/install.sh | sh

# Install OpenSpec (spec-driven planning layer for coding agents)
RUN npm install -g --prefix /home/claude/.local @fission-ai/openspec@latest

ENV PATH="/home/claude/.local/bin:${PATH}"
# Openspec telemetry disable
ENV DO_NOT_TRACK=1

# Enable plugin auto-updates even when the main auto-updater is disabled
ENV FORCE_AUTOUPDATE_PLUGINS=true

# Pre-install Context7 MCP server (registered via host .claude.json mount)
RUN npm install -g --prefix /home/claude/.local @upstash/context7-mcp

# Register marketplaces and install plugins
RUN claude plugin marketplace add anthropics/claude-plugins-official && \
    claude plugin marketplace add anthropics/skills && \
    claude plugin marketplace add HKUDS/CLI-Anything && \
    claude plugin marketplace add Stoica-Mihai/claude-skills && \
    claude plugin install superpowers@claude-plugins-official && \
    claude plugin install skill-creator@claude-plugins-official && \
    claude plugin install claude-api@anthropic-agent-skills && \
    claude plugin install document-skills@anthropic-agent-skills && \
    claude plugin install example-skills@anthropic-agent-skills && \
    claude plugin install cli-anything@cli-anything && \
    claude plugin install cli-anything-go@claude-skills && \
    claude plugin install opsx-ext@claude-skills

# Shell function: wraps every `claude` invocation in a tmux session for dashboard visibility
RUN cat >> /home/claude/.bashrc <<'BASHEOF'
claude() {
  local claude_bin
  claude_bin=$(command -v claude)
  local session_name="claude-$(od -An -tx1 -N4 /dev/urandom | tr -d ' \n')"
  tmux new-session -d -s "$session_name" -- "$claude_bin" --dangerously-skip-permissions "$@"
  TMUX= tmux attach -t "$session_name"
}
BASHEOF

# Copy the pre-built dashboard binary from the builder stage
COPY --from=builder /dashboard-server /home/claude/dashboard-server

WORKDIR /workspace
