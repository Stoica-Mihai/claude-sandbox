FROM alpine:3.23

# Install native dependencies for Alpine/musl compatibility
RUN apk add --no-cache \
    bash \
    git \
    curl \
    socat \
    bubblewrap \
    libqrencode-tools \
    libstdc++ \
    libgcc \
    go \
    npm

# Create a non-root user with matching host UID (Claude Code refuses --dangerously-skip-permissions as root)
ARG UID
ARG GID
RUN addgroup -g ${GID} claude && \
    adduser -D -u ${UID} -G claude claude
USER claude

# Install Claude Code as the non-root user
RUN curl -fsSL https://claude.ai/install.sh | bash

# Install UV
RUN curl -LsSf https://astral.sh/uv/install.sh | sh

# Install OpenSpec (spec-driven planning layer for coding agents)
RUN npm install -g --prefix /home/claude/.local @fission-ai/openspec@latest

ENV PATH="/home/claude/.local/bin:${PATH}"

# Copy local plugin
COPY --chown=claude:claude plugins/cli-anything-go /home/claude/.claude/plugins/marketplaces/cli-anything-go

# Register marketplaces, install plugins, then clean up git history
RUN claude plugin marketplace add anthropics/claude-plugins-official && \
    claude plugin marketplace add anthropics/skills && \
    claude plugin marketplace add HKUDS/CLI-Anything && \
    claude plugin marketplace add /home/claude/.claude/plugins/marketplaces/cli-anything-go && \
    claude plugin install superpowers@claude-plugins-official && \
    claude plugin install skill-creator@claude-plugins-official && \
    claude plugin install claude-api@anthropic-agent-skills && \
    claude plugin install document-skills@anthropic-agent-skills && \
    claude plugin install example-skills@anthropic-agent-skills && \
    claude plugin install cli-anything@cli-anything && \
    claude plugin install cli-anything-go@cli-anything-go && \
    find ~/.claude/plugins -name ".git" -type d -exec rm -rf {} + 2>/dev/null; true

# Alias for launching Claude with bypass permissions
RUN echo 'alias claude="claude --dangerously-skip-permissions "' >> /home/claude/.bashrc

WORKDIR /workspace
