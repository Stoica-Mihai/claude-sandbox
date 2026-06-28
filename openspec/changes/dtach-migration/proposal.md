# Proposal: Replace tmux with dtach

## Why

The current architecture stacks a custom relay (`tmux pipe-pane` → `socat` → unix
socket → Go) on top of tmux. tmux was adopted purely for **session persistence**
(sessions survive a dashboard restart) and CLI access. But tmux is a full
terminal multiplexer: its terminal-emulation layer eats native xterm.js mouse
selection, which is exactly why the `socat` relay had to be built to bypass
`tmux attach`. The project history shows the full fight:

- `20eca5b` adopted tmux for persistence after the original direct-PTY design
  lost every session on dashboard restart.
- `db31823` → `b07e9b1` tried and **reverted** disabling tmux alternate screen
  (garbled reattach, banner duplication).
- `27b5e25` added the `pipe-pane`+`socat` relay to claw native mouse back while
  keeping tmux underneath.

The relay is earned complexity, but it carries three data races
(`socatConn` reassignment on reconnect, unsynchronised activity timestamps,
unsynchronised alt-screen flag) and a fragile reconnect path.

`dtach` breaks the bind that forced all of this: it provides session
persistence **without** being a terminal emulator. It is a thin detach/attach
layer that passes raw PTY bytes in both directions — no mouse munging, no
alternate-screen rewriting. A spike in the running container confirmed:

- Persistence: killing the attach client leaves the inner process running;
  reattach recovers full session state (env, scrollback in the dashboard ring
  buffer). This is the exact failure that killed the original direct-PTY design.
- Native mouse / copy: raw bytes round-trip unchanged, so xterm.js keeps native
  selection and `copyOnSelect` — browser highlight-and-copy is preserved.
- Resize: `pty.Setsize` on the owned attach PTY propagates a `SIGWINCH` to the
  inner program.

## What Changes

- `dtach` replaces tmux as the session-persistence mechanism. tmux and socat are
  removed from the image.
- Each session is a detached `dtach` master holding `claude`. The relay owns a
  single `dtach -a` attach process per session via a directly-owned PTY
  (`creack/pty`), reads its output, and broadcasts to all WebSocket viewers —
  identical viewer fan-out to today, one fewer process hop.
- Session input is written to the attach PTY (no `socat`). Resize calls
  `pty.Setsize` instead of shelling out `tmux resize-window`.
- Session discovery scans a socket directory plus a per-session metadata sidecar
  (cwd, created-at, inner PID) instead of `tmux list-sessions`. dtach has no
  notion of working directory, so the spawner records it.
- Kill reads the sidecar PID file and signals the inner process group; dtach has
  no `kill-session` equivalent.
- Direct CLI `claude` is disabled: the shell function prints a "use the
  dashboard" message and exits non-zero. The dashboard is the only way to create
  a session (it execs the real binary), so every session is relay-captured from
  byte 0 and fully controllable in the browser.
- The ring buffer, alternate-screen tracking, multi-viewer per-viewer resize, and
  the entire xterm.js frontend are **unchanged** — only the byte source changes.
- The three relay data races are eliminated: the `socatConn` reassignment race
  vanishes structurally (the PTY is owned for the relay's lifetime; a dead
  attach restarts the relay under lock), and the timestamp / alt-screen fields
  move to `sync/atomic` or mutex protection.
- **BREAKING**: tmux is gone, and direct CLI `claude` no longer starts a session
  (use the dashboard). tmux `attach`/copy-mode scrollback are removed; the
  dashboard's ring buffer is the dashboard scrollback source.

## Capabilities

### New Capabilities
- `dtach-sessions`: session lifecycle on dtach — spawn a detached master,
  discover sessions via socket dir + metadata sidecar, kill via PID sidecar,
  persistence across dashboard restart, and CLI dtach wrapping.

### Modified Capabilities
- `pty-relay`: the relay reads/writes a directly-owned `dtach -a` PTY instead of
  a `socat` unix socket; resize uses `pty.Setsize`; reconnect re-execs `dtach -a`.
  Ring buffer, alt-screen routing, and viewer broadcast are unchanged.

### Removed Capabilities
- `tmux-sessions`: all tmux-based spawn / discovery / kill / polling / config
  requirements are removed and superseded by `dtach-sessions`.

## Impact

| File | Change |
|------|--------|
| `backend/relay.go` | Rewrite transport: own `dtach -a` PTY, drop `startPipePaneCmd`/`stopPipePaneCmd`/socat/unix-socket listen+accept; `SendInput`→PTY write; `resizeTmux`→`pty.Setsize`; fix timestamp + alt-screen races |
| `backend/session.go` | Discovery via socket dir + metadata sidecar; spawn via `dtach -n` writing sidecar; kill via PID sidecar; keep poll/cache/SSE shape |
| `backend/go.mod` | Add `github.com/creack/pty` |
| `Dockerfile.backend` | Drop `tmux`, `socat`; add `dtach`; remove tmux.conf copy; replace `claude` shell function with a disabled stub |
| `tmux.conf` | Deleted |
| `CLAUDE.md` | Update architecture notes (tmux → dtach) |

No frontend changes. No change to the WebSocket / SSE protocol or the relay's
public method surface consumed by `handlers.go`.

## Risks

- **No direct CLI sessions.** Running `claude` in the container shell no longer
  starts a session — it points the user at the dashboard. Accepted by design:
  the dashboard is the only session surface, which removes CLI/browser size
  contention entirely.
- **Stale sockets.** A crashed dtach leaves a socket file. Discovery keys off the
  PID sidecar and unlinks dead sessions' sidecars + socket.
- **Kill is indirect.** No `kill-session`; relies on the PID sidecar written at
  spawn. If the sidecar is missing, kill falls back to closing the attach and is
  best-effort.
- **dtach packaging.** `dtach` is in Debian bookworm (`0.9-5+b1`); `abduco` was
  not available in the configured apt sources, so `dtach` is the pick.

## Decision

Proceed to implementation on the `dtach-migration` branch after review.
