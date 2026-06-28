# Tasks: Replace tmux with dtach

## 1. Image and shell

- [x] 1.1 `Dockerfile.backend`: remove `tmux` and `socat` from the apt install list; add `dtach`
- [x] 1.2 `Dockerfile.backend`: remove the `COPY tmux.conf /home/claude/.tmux.conf` line
- [x] 1.3 Delete `tmux.conf`
- [x] 1.4 `Dockerfile.backend`: replace the `claude` shell function with a stub that prints a "use the dashboard" message and exits non-zero (direct CLI sessions disabled; the dashboard spawns the real binary)
- [x] 1.5 `Dockerfile.backend`: export `CLAUDE_SOCK_DIR` and `CLAUDE_META_DIR` defaults for the `claude` user

## 2. Session storage

- [x] 2.1 Add socket/metadata dir resolution in `session.go` (`$XDG_RUNTIME_DIR/claude/{sock,meta}` with `/home/claude/.local/state/claude/{sock,meta}` fallback), created `0700`
- [x] 2.2 Define metadata sidecar struct `{cwd, created}` with JSON read/write helpers; define PID sidecar path helper

## 3. Spawn / discover / kill (session.go)

- [x] 3.1 Rewrite `Spawn` to create a detached master via `dtach -n <sock> -E -z -- bash -c 'echo $$ > <pid>; exec claude --dangerously-skip-permissions "$@"'`, writing the metadata sidecar; keep the `/workspace` guard (fix the prefix check to require a separator) and the 3-retry collision loop
- [x] 3.2 Replace `rawTmuxOutput`/`parseTmuxOutput`/`discoverTmuxSessions` with a socket-directory scan joined to metadata sidecars, producing `DisplaySession`
- [x] 3.3 Implement liveness probe (PID `signal 0` check, socket-stat fallback); unlink dead sockets and their sidecars during discovery
- [x] 3.4 Rewrite `Kill` to read the PID sidecar and signal the process group (`SIGTERM` then `SIGKILL` after grace); best-effort fallback (close attach + unlink) when the sidecar is absent
- [x] 3.5 Rewrite `sessionExists` to check for a live socket
- [x] 3.6 Keep the 2s cache, poll loop, SSE publish, and `enrichSessions` logic; repoint them at the new discovery
- [x] 3.7 Move session-name persistence off `/tmp/claude-session-names.json` into `CLAUDE_META_DIR` (mode `0600`)

## 4. Relay transport (relay.go)

- [x] 4.1 Add `github.com/creack/pty` to `go.mod`
- [x] 4.2 Replace `socatConn`/`listener`/`socketPath` with an owned attach PTY (`*os.File`) plus the dtach socket path
- [x] 4.3 Rewrite `Start` to `pty.Start(exec.Command("dtach","-a",sock,"-E","-z","-r","none"))`, issue an initial `pty.Setsize`, start `readLoop`
- [x] 4.4 Remove `startPipePaneCmd`, `stopPipePaneCmd`, and the unix-socket listen/accept code
- [x] 4.5 Rewrite `SendInput` to write the attach PTY
- [x] 4.6 Rewrite `resizeTmux` → `pty.Setsize` on the attach PTY; force `pty.Setsize` on viewer (re)connect
- [x] 4.7 Rewrite `reconnect` to re-exec `dtach -a`, swap the PTY under the relay mutex, guard read loops with a generation counter; stop when the socket is gone
- [x] 4.8 Update `Stop` to kill the attach process and close the PTY (no socket/socat teardown)

## 5. Race fixes (relay.go)

- [x] 5.1 Reassign the attach PTY only under the relay mutex
- [x] 5.2 Convert `lastInputAt`/`lastResizeAt` to `atomic.Int64` (unix-nano)
- [x] 5.3 Make `inAltScreen` access in `AddViewer` safe (relay mutex or `atomic.Bool`)
- [x] 5.4 Keep `trackAltScreen`, ring buffer, `broadcast`, and per-viewer resize logic unchanged

## 6. Cleanup and docs

- [x] 6.1 Remove any remaining tmux references in `session.go`/`relay.go`/comments
- [ ] 6.2 Update `CLAUDE.md` architecture section (tmux → dtach; socat removed)
- [ ] 6.3 Update `README.md` if it references tmux

## 7. Verification

- [x] 7.1 `go build ./...` and `go vet ./...` pass in `backend/`
- [x] 7.2 `go test -race ./...` passes (add a relay reconnect/race smoke test)
- [ ] 7.3 `Dockerfile.backend` builds; `dtach` present, `tmux`/`socat` absent
- [ ] 7.4 Spawn from dashboard → session is interactive
- [ ] 7.5 Browser highlight-and-copy works on scrollback (no modifier) and on live TUI lines (Shift)
- [ ] 7.6 Resize: terminal adopts the connecting viewer's size on connect
- [ ] 7.7 Restart the backend container → session still listed and attachable with state intact
- [ ] 7.8 Two viewers on one session: both see output, active typist's size wins, non-active not garbled
- [ ] 7.9 Kill from dashboard removes the session and closes viewers
- [ ] 7.10 CLI `claude` appears in the dashboard with correct cwd; detach/reattach works
- [ ] 7.11 Crash a master (kill -9) → discovery unlinks the stale socket within one poll cycle
