FROM debian:bookworm-slim

# Install native dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    git \
    curl \
    socat \
    bubblewrap \
    qrencode \
    npm \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Go 1.26.1 from official tarball
RUN curl -fsSL https://go.dev/dl/go1.26.1.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# Create a non-root user with matching host UID (Claude Code refuses --dangerously-skip-permissions as root)
ARG UID
ARG GID
RUN groupadd -g ${GID} claude && \
    useradd -m -u ${UID} -g claude claude
USER claude

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

# Alias for launching Claude with bypass permissions
RUN echo 'alias claude="claude --dangerously-skip-permissions "' >> /home/claude/.bashrc

WORKDIR /workspace
