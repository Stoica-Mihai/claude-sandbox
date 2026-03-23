## Context

The dashboard currently manages two categories of sessions: "managed" sessions spawned via the dashboard (with a direct PTY) and "detected/external" sessions discovered from `~/.claude/sessions/*.json` files. Managed sessions are fully interactive (WebSocket relay to PTY). External sessions are display-only — they appear in the sidebar but cannot be attached to because the dashboard has no PTY file descriptor for them.

This two-tier model means CLI-started sessions are second-class citizens, and all managed sessions are lost on dashboard restart since the PTY is owned by the dashboard process.

The container runs as a single non-root user (`claude`) with tmux available as a system package.

## Goals / Non-Goals

**Goals:**
- Every Claude Code session (dashboard or CLI) is interactive from the dashboard
- Sessions survive dashboard restarts
- Uniform session model — no managed vs external distinction
- CLI users get automatic tmux wrapping with zero friction

**Non-Goals:**
- Multi-pane or multi-window tmux layouts (one claude process per tmux session)
- tmux control mode (`-CC`) integration
- Authentication or multi-user session isolation
- Preserving backward compatibility with the `~/.claude/sessions/*.json` discovery mechanism

## Decisions

### Decision 1: tmux as universal session owner

All Claude Code sessions run inside tmux sessions. The dashboard never owns a PTY directly — it always attaches via `tmux attach`.

**Rationale:** This gives a single code path for all sessions. tmux provides persistence, scrollback management, and native multi-client support. The alternative (direct PTY for dashboard sessions, tmux only for external) creates two code paths and doesn't solve the dashboard restart problem.

**Alternatives considered:**
- `reptyr`/ptrace to steal PTYs from external processes — fragile, requires `CAP_SYS_PTRACE`
- Unix socket relay — reinvents tmux's multiplexing, no CLI attach without custom client
- Hybrid (direct PTY + tmux for external only) — two code paths, dashboard sessions still lost on restart

### Decision 2: Ephemeral attach PTY per WebSocket connection

When a WebSocket connects, the server spawns `tmux attach -t <name>` with a PTY via `pty.StartWithSize`. This PTY is destroyed when the WebSocket disconnects. The tmux session continues.

**Rationale:** This reuses the existing WebSocket ↔ PTY relay code with minimal changes. The attach process is lightweight. tmux natively handles multiple attach clients, so concurrent viewers work automatically.

**Alternatives considered:**
- Persistent attach process that outlives WebSocket connections — more complex lifecycle management for no benefit since tmux attach is cheap to spawn
- tmux control mode — would require writing a Go protocol parser for tmux's structured output format

### Decision 3: Session discovery via `tmux list-sessions`

Replace `~/.claude/sessions/*.json` file scanning with `tmux list-sessions -F` using a custom format string. Sessions are identified by a `claude-` name prefix.

**Rationale:** tmux is now the source of truth. File-based discovery becomes redundant and could show stale data. The `claude-` prefix convention separates dashboard-relevant sessions from any other tmux sessions the user might have.

### Decision 4: CLI alias wraps claude in tmux

The `claude` bash alias is replaced with a shell function that creates a tmux session with a random name and attaches interactively. Arguments pass through to the claude command.

**Rationale:** Zero friction for CLI users. Every `claude` invocation becomes visible in the dashboard. The user can detach with `Ctrl+B d` and reattach from the dashboard.

### Decision 5: Drop the RingBuffer for scrollback

tmux manages scrollback with configurable `history-limit` (set to 50000 lines). On `tmux attach`, tmux replays the current visible pane content. The dashboard's `RingBuffer` is removed.

**Rationale:** Maintaining a separate scrollback buffer is redundant when tmux already does this better (larger capacity, per-pane, survives detach/reattach). The RingBuffer was necessary when the dashboard owned the PTY; with tmux, it's not.

### Decision 6: window-size latest for multi-client resize

tmux.conf sets `window-size latest` so that when multiple clients attach with different terminal sizes, the most recently active client's dimensions are used.

**Rationale:** Without this, tmux defaults to the smallest attached client's size, which would constrain the dashboard viewer when a CLI user with a smaller terminal is also attached (or vice versa).

## Risks / Trade-offs

- **tmux is a hard dependency** → Acceptable since this is a container environment where we control the image. Added to Dockerfile apt-get.
- **Sessions started without tmux are invisible** → By design. The alias ensures all CLI sessions go through tmux. If someone bypasses the alias, they get the old behavior (invisible to dashboard). This is documented as a breaking change.
- **`tmux list-sessions` overhead** → Mitigated with a short cache (~2 seconds) in the SessionManager. Invalidated on spawn/kill.
- **tmux server not running on first use** → `tmux new-session` auto-starts the server. `tmux list-sessions` when no server exists returns exit code 1 — treated as zero sessions.
- **Session name collision** → 8 random hex chars = 4 billion possibilities. If `tmux new-session` fails with "duplicate session", retry with a new name (max 3 attempts).
- **Nested tmux from host** → If the user connects from a tmux session on the host, the CLI function would hit a nesting warning. Mitigated by unsetting `$TMUX` before `tmux attach` in the bash function.
- **Multi-client resize conflict** → Multiple viewers with different sizes would constrain to the smallest. Mitigated with `set -g window-size latest` in tmux.conf.
- **Dashboard shutdown must NOT kill tmux sessions** → `Shutdown()` only closes WebSocket connections and kills attach processes. tmux sessions persist for reconnection.
