# Spec: tmux-sessions

**Spec Path:** `specs/tmux-sessions/spec.md`
**Change Type:** REMOVED

---

The entire `tmux-sessions` capability is removed and superseded by
`dtach-sessions`. tmux and socat are removed from the image; the `pipe-pane` +
`socat` transport and `tmux.conf` no longer exist.

## REMOVED Requirements

### Requirement: Spawn Claude Code sessions inside tmux
**Reason:** Sessions are now spawned as detached `dtach` masters. See
`dtach-sessions` → "Spawn Claude Code sessions as detached dtach masters".

### Requirement: Discover sessions via tmux list-sessions
**Reason:** Discovery now scans the socket directory and metadata sidecars. See
`dtach-sessions` → "Discover sessions via socket directory and metadata sidecar".

### Requirement: Kill sessions via tmux kill-session
**Reason:** dtach has no `kill-session`; termination signals the inner process
group via the PID sidecar. See `dtach-sessions` → "Kill sessions via the PID
sidecar".

### Requirement: Periodic session list polling
**Reason:** Polling now scans the socket directory instead of `tmux
list-sessions`. See `dtach-sessions` → "Periodic session list polling".

### Requirement: Session persistence across dashboard restarts
**Reason:** Persistence is now provided by the dtach master. See `dtach-sessions`
→ "Session persistence across dashboard restarts".

### Requirement: CLI sessions are automatically tmux-managed
**Reason:** The `claude` shell function now wraps invocations in dtach. See
`dtach-sessions` → "CLI sessions are automatically dtach-managed".

### Requirement: tmux configuration
**Reason:** `tmux.conf` is deleted; dtach has no configuration file and no
terminal emulation, history-limit, mouse, or window-size settings.
