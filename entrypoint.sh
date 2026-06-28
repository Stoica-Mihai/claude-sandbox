#!/bin/bash
# Seed the scoped Claude config dir (CLAUDE_CONFIG_DIR — host-mounted and
# persistent, isolated from the host's real ~/.claude) from the image's baked
# plugins + registration on first run, so pre-installed plugins work without
# exposing the host config. Auth/sessions then persist in the scoped dir.
set -e

CONF="${CLAUDE_CONFIG_DIR:-/home/claude/.claude-sandbox}"
mkdir -p "$CONF"

# First run = no plugins yet: seed plugins + registration from the image
# (cp -n won't clobber any existing state on later runs).
if [ ! -e "$CONF/plugins" ]; then
  cp -an /home/claude/.claude/.    "$CONF/"             2>/dev/null || true
  cp -an /home/claude/.claude.json "$CONF/.claude.json" 2>/dev/null || true
fi

# Always refresh settings from the repo's container-settings.json (authoritative).
if [ -f /home/claude/container-settings.json ]; then
  cp -f /home/claude/container-settings.json "$CONF/settings.json"
fi

exec "$@"
