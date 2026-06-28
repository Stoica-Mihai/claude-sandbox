# Design: Replace tmux with dtach

## Context

Persistence and native-mouse/copy have been in tension throughout this project.
The triangle: a session can survive a dashboard restart, xterm.js can own the
mouse for native selection, and viewers can resize independently — but no single
off-the-shelf tool gives all three for free.

- **Direct PTY** (original): native mouse ✓, persistence ✗ (dashboard owns the
  PTY → restart kills sessions). Abandoned in `20eca5b`.
- **`tmux attach`**: persistence ✓, native mouse ✗ (tmux emulation eats
  selection). Workarounds each broke something else (`db31823`/`b07e9b1`).
- **`pipe-pane`+`socat` over tmux** (current): all three ✓, but high complexity
  and three data races.

`dtach` occupies the corner none of the above could: **persistence without
terminal emulation**. It is a ~500-line detach layer that holds a child's PTY and
relays raw bytes to/from attach clients. No VT parsing, no mouse reporting, no
alternate-screen handling. That is precisely what lets xterm.js keep native
selection while the session outlives the dashboard.

## Spike evidence

Run in the `claude_backend` container (`dtach 0.9-5+b1`), Python PTY harness
owning a `dtach -a` client (mimicking Go owning the relay PTY):

```
TEST 1: persistence across attach-client death
  master alive after client killed: YES | heartbeat advanced: YES
  reattach sees marker: MARK=alive_42        <- session state survived
  resize 50x200 propagated to inner: YES     <- pty.Setsize -> SIGWINCH
TEST 2: raw byte passthrough                  <- ESC sequences round-trip raw
TEST 3: two concurrent attach clients share ONE pty size, last-resize wins
```

Conclusions:
1. Killing the attach client (= dashboard dies/restarts) leaves the inner
   process running and reattach recovers it. The Option-B killer is solved.
2. Raw passthrough means xterm.js native selection / `copyOnSelect` is preserved
   byte-for-byte — same stream the `socat` relay produces today.
3. `pty.Setsize` on the owned PTY propagates size to the inner program.
4. Multiple attach clients share one PTY size with last-resize-wins — the same
   model the dashboard already implements (active typist wins).

Gotcha surfaced: a freshly attached `dtach -a` does **not** auto-apply the new
client's window size; an explicit resize after attach is required. The dashboard
already sends a resize on WebSocket connect, so this must be preserved.

## Goals / Non-Goals

**Goals:**
- Sessions survive dashboard restart (parity with tmux today).
- Native xterm.js selection + `copyOnSelect` preserved (browser highlight-copy).
- Multi-viewer with per-viewer "active typist wins" resize preserved.
- Delete the `pipe-pane`+`socat`+unix-socket machinery and the reconnect dance.
- Eliminate the three relay data races.
- Keep the ring buffer, alt-screen routing, and the entire frontend unchanged.

**Non-Goals:**
- Changing the WebSocket/SSE wire protocol or the relay's method surface used by
  `handlers.go`.
- Preserving CLI-side scrollback / copy-mode (dtach has none; out of scope).
- Building a VT100 parser or any terminal emulation in Go.
- Multi-window / multi-pane layouts.

## Decisions

### Decision 1: Session = detached `dtach` master; relay owns one `dtach -a` PTY

A session is created detached:

```
dtach -n <sockdir>/claude-<hex> -E -z -- bash -c \
  'echo $$ > <metadir>/claude-<hex>.pid; exec claude --dangerously-skip-permissions "$@"'
```

- `-n`: create detached, no initial client.
- `-E`: disable the detach keystroke so the relay's byte stream is never
  intercepted (the relay manages lifecycle, not a keypress).
- `-z`: disable suspend key passthrough handling.
- The inner `bash -c` writes the **claude PID** to a sidecar before exec, so kill
  has a target (dtach has no `kill-session`).

The relay then owns exactly one attach client per session:

```go
cmd := exec.Command("dtach", "-a", sockPath, "-E", "-z", "-r", "none")
ptmx, err := pty.Start(cmd)   // Go owns the master side of the attach PTY
```

The relay reads `ptmx` (pane output) and writes `ptmx` (pane input), broadcasts
to all WebSocket viewers, and maintains the ring buffer — the same fan-out shape
as today, with `ptmx` replacing `socatConn`.

**Rationale:** one attach client shared by N viewers mirrors the current
single-relay/N-viewer design. Owning the PTY directly removes the `socat` process
and the unix-socket listen/accept, and removes the `socatConn` reassignment race
because the PTY lives for the relay's lifetime.

**Alternatives considered:**
- `abduco` instead of `dtach` — preferred API (`-A` attach-or-create) but not in
  the configured apt sources; would require build-from-source. dtach is packaged.
- One `dtach -a` per WebSocket viewer — N attach processes, N ring buffers, no
  single source of truth for scrollback. Rejected; breaks the shared-history
  model.
- Keep tmux, swap only `socat`→`dtach` as transport — keeps two persistence
  layers for no benefit. Rejected.

### Decision 2: Discovery via socket directory + metadata sidecar

tmux's `list-sessions` is replaced by scanning `<sockdir>/claude-*`. dtach has no
working-directory concept, so the spawner writes a JSON sidecar
`<metadir>/claude-<hex>.json` containing `{ "cwd": ..., "created": <unix> }`.
Discovery joins socket file + sidecar:

- name: socket basename
- created: sidecar `created` (fallback: socket mtime)
- cwd: sidecar `cwd` (fallback: empty / `unknown`)

**Liveness:** a socket file existing does not prove the master is alive (a crash
leaves a stale socket). Discovery probes liveness via the PID sidecar (a `signal
0` check on the inner process), falling back to socket-file existence when the
sidecar is absent; a session that fails the probe is treated as dead and its
socket and sidecars are unlinked.

**Rationale:** preserves the dashboard's existing `DisplaySession` shape (name,
cwd, created, duration) without tmux. The poll loop, 2s cache, and SSE publish
logic are unchanged — only `rawTmuxOutput`/`parseTmuxOutput` are replaced by a
directory scan.

### Decision 3: Resize via `pty.Setsize`, with forced resize on attach

Resize calls `pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})` on the
relay's attach PTY, which dtach forwards as `SIGWINCH` to the inner program. The
per-viewer "active typist wins" logic in `relay.go` is unchanged — only the
mechanism under `resizeTmux` changes from a `tmux resize-window` shell-out to a
syscall.

Because a fresh attach does not auto-apply size (spike gotcha), the relay issues
an explicit `pty.Setsize` immediately after `pty.Start` and on every viewer
(re)connect.

### Decision 4: Kill via PID sidecar, signal the process group

There is no `dtach kill-session`. Kill reads `<metadir>/claude-<hex>.pid` and
sends `SIGTERM` to the process group, then `SIGKILL` after a short grace period.
When the inner process exits, dtach exits and removes its socket; the relay's
`ptmx` read returns EOF and the relay stops. If the PID sidecar is absent
(externally created session), kill is best-effort: close the attach and unlink
the socket.

**Rationale:** signalling the inner PID is the only reliable way to terminate the
claude process when the multiplexer offers no kill verb. The process group
ensures child processes (e.g. tools claude spawned) are also reaped.

### Decision 5: Reconnect re-execs `dtach -a` under lock

If the relay's `ptmx` returns EOF while the session socket still exists (e.g. the
attach process died but the master is alive), the relay re-execs `dtach -a` and
swaps `ptmx` **while holding the relay mutex**, then restarts a single read loop.
A generation counter guards against overlapping read loops. If the socket is gone
(master exited), the relay stops — same terminal condition as today.

**Rationale:** keeps the resilience the current `reconnect()` provides, but
without the unsynchronised `socatConn` reassignment that is the current race.

### Decision 6: Race elimination

- `socatConn` field → `ptmx` owned for the relay lifetime, reassigned only under
  the relay mutex during reconnect (Decision 5). Race removed structurally.
- `lastInputAt` / `lastResizeAt` → `atomic.Int64` (unix-nano), read/written
  without the larger lock.
- `inAltScreen` / `partial` → these are only touched by the single read loop
  except for `inAltScreen` read in `AddViewer`; move the `AddViewer` read under
  the relay mutex (or make `inAltScreen` an `atomic.Bool`).

This change is the natural place to fix the races because the transport rewrite
already rebuilds the read/reconnect paths.

### Decision 7: Dockerfile, tmux.conf, shell function

- `Dockerfile.backend`: remove `tmux` and `socat` from the apt install; add
  `dtach`. Remove the `COPY tmux.conf` line. Replace the `claude` shell function
  with a stub that disables direct CLI sessions:

  ```bash
  claude() {
    cat >&2 <<'MSG'
  Direct `claude` sessions are disabled in this container.
  Create and open sessions from the dashboard.
  MSG
    return 1
  }
  ```

  All sessions are created from the dashboard, which execs the real claude binary
  directly (not this function). Because the relay attaches at spawn, every
  session is captured from its first byte and is fully controllable in the
  browser — there is no CLI terminal to contend with for the single PTY size, so
  no origin/view-only handling is needed.

- `tmux.conf`: deleted.

### Socket and metadata locations

Use a writable, non-shared directory rather than bare `/tmp` (the current
`/tmp/relay-*.sock` and `/tmp/claude-session-names.json` are world-readable in a
shared tmp). Default `CLAUDE_SOCK_DIR=$XDG_RUNTIME_DIR/claude/sock` (fallback
`/home/claude/.local/state/claude/sock`), `CLAUDE_META_DIR=.../claude/meta`,
mode `0700`. This also addresses a medium-severity finding from the audit.

## Risks / Trade-offs

- **Direct CLI claude removed** — the `claude` shell function now refuses to
  start a session and points at the dashboard. Accepted by design: the dashboard
  is the only session surface, which removes CLI/browser size contention and the
  loss of CLI scrollback is moot.
- **Stale sockets** — mitigated by liveness probing + unlink during discovery
  (Decision 2).
- **Kill depends on sidecar** — best-effort fallback when absent (Decision 4).
- **`creack/pty` dependency** — small, widely used; the original direct-PTY design
  already used it (`pty.StartWithSize`), so it is reintroducing a known dep.

## Verification strategy

- `go build` / `go vet` / `go test -race ./...` pass (the race fixes make `-race`
  meaningful for the relay for the first time).
- Manual: spawn from dashboard → interactive; restart dashboard → session still
  listed and attachable with state intact; browser highlight-and-copy works on
  scrollback and (with Shift) on live TUI lines; resize on connect; two viewers
  both see output and can type; kill removes the session; CLI `claude` is visible
  in the dashboard and detach/reattach works.
