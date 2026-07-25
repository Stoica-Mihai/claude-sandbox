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

# Start the virtual X display for GUI-app testing (DISPLAY=:99). Best-effort:
# a `set -e` failure here must never stop sessiond, so the whole block is
# guarded and only runs when Xvfb is present.
start_display() {
  command -v Xvfb >/dev/null 2>&1 || return 0
  mkdir -p /tmp/.X11-unix "${XDG_RUNTIME_DIR:-/tmp/xdg-runtime}"
  chmod 700 "${XDG_RUNTIME_DIR:-/tmp/xdg-runtime}" 2>/dev/null || true
  Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
  # Wait for the display, then start a minimal WM so window mapping/focus works.
  for _ in $(seq 1 25); do
    xdpyinfo -display :99 >/dev/null 2>&1 && break
    sleep 0.2
  done
  fluxbox -display :99 >/tmp/fluxbox.log 2>&1 &
}
start_display || true

exec "$@"
