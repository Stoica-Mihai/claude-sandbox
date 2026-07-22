package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"

	"claude-sandbox-sessiond/protocol"
)

const (
	// sessionPrefix identifies dashboard sessions.
	sessionPrefix = "claude-"
	// maxSpawnRetries is the number of retries on session-name collision.
	maxSpawnRetries = 3
	// termType is the TERM environment variable for new terminal sessions.
	termType = "xterm-256color"
)

// liveSession is the registry's minimal view of a live session: the surface it
// needs for LIST/KILL/shutdown regardless of kind, so that logic is written
// once against the interface instead of duplicated per kind. Both *session
// (terminal, PTY) and *chatSession (chat, stream-json pipe) satisfy it.
type liveSession interface {
	Kill()
	Exited() <-chan struct{}
	serve(ln net.Listener)
	info() protocol.SessionInfo
}

// registry owns the live session set and serves the control-socket ops.
type registry struct {
	mu       sync.Mutex
	sessions map[string]liveSession
	sockDir  string
	// newCommand/newChatCommand build the session command per kind; swapped by tests.
	newCommand     func(cwd, uuid string, resume bool) *exec.Cmd
	newChatCommand func(cwd, uuid string, resume bool) *exec.Cmd
}

func newRegistry(sockDir string) *registry {
	return &registry{
		sessions:       make(map[string]liveSession),
		sockDir:        sockDir,
		newCommand:     claudeCommand,
		newChatCommand: claudeChatCommand,
	}
}

// claudeCommand builds the real claude invocation for a terminal (PTY) session.
func claudeCommand(cwd, uuid string, resume bool) *exec.Cmd {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}
	flag := "--session-id"
	if resume {
		flag = "--resume"
	}
	cmd := exec.Command(claudePath, flag, uuid, "--dangerously-skip-permissions")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM="+termType)
	return cmd
}

// claudeChatCommand builds the real claude invocation for a chat (stream-json
// pipe) session. --verbose is required: --print with
// --output-format=stream-json refuses to run without it (verified against the
// pinned engine). No PTY, no TERM — this is a plain-pipe headless mode.
func claudeChatCommand(cwd, uuid string, resume bool) *exec.Cmd {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--dangerously-skip-permissions",
	}
	if resume {
		args = append(args, "--resume", uuid)
	} else {
		args = append(args, "--session-id", uuid)
	}
	cmd := exec.Command(claudePath, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd
}

// generateSessionName creates a session name like "claude-a1b2c3d4".
func generateSessionName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return sessionPrefix + hex.EncodeToString(b)
}

// spawn starts a claude session — under an owned PTY for kind=="terminal" (or
// empty, the default), or as a stream-json pipe-child for kind=="chat" — and
// its attach listener. Dispatch is the only kind-aware branch; the rest of
// this method (name/socket allocation, registration, retry) is shared.
func (r *registry) spawn(cwd, uuid string, resume bool, kind string) (string, error) {
	for i := 0; i < maxSpawnRetries; i++ {
		name := generateSessionName()

		r.mu.Lock()
		_, taken := r.sessions[name]
		r.mu.Unlock()
		if taken {
			continue
		}

		sockPath := protocol.SessionSock(r.sockDir, name)
		_ = os.Remove(sockPath) // stale leftover from a crashed run
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			slog.Warn("session socket listen failed, retrying", "session", name, "error", err)
			continue
		}

		onExit := func() {
			_ = os.Remove(sockPath)
			r.mu.Lock()
			delete(r.sessions, name)
			r.mu.Unlock()
		}

		var s liveSession
		var pid int
		switch kind {
		case protocol.KindChat:
			cmd := r.newChatCommand(cwd, uuid, resume)
			cs := newChatSession(name, cwd, uuid, time.Now())
			cs.onExit = onExit
			if err := cs.begin(cmd); err != nil {
				_ = ln.Close()
				_ = os.Remove(sockPath)
				return "", err
			}
			s, pid = cs, cmd.Process.Pid
		default:
			cmd := r.newCommand(cwd, uuid, resume)
			ptmx, err := pty.Start(cmd)
			if err != nil {
				_ = ln.Close()
				_ = os.Remove(sockPath)
				return "", fmt.Errorf("starting claude: %w", err)
			}
			ts := newSession(name, cwd, uuid, time.Now())
			ts.onExit = onExit
			ts.begin(ptmx, cmd)
			s, pid = ts, cmd.Process.Pid
		}

		r.mu.Lock()
		r.sessions[name] = s
		r.mu.Unlock()

		s.serve(ln)
		slog.Info("spawned session", "session", name, "cwd", cwd, "uuid", uuid, "kind", kind, "pid", pid)
		return name, nil
	}
	return "", fmt.Errorf("failed to create session after %d attempts", maxSpawnRetries)
}

// list returns every live session's registry-visible metadata.
func (r *registry) list() []protocol.SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]protocol.SessionInfo, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s.info())
	}
	return out
}

// kill terminates a session by name; ok=false means the name is unknown.
func (r *registry) kill(name string) (ok bool) {
	r.mu.Lock()
	s, found := r.sessions[name]
	r.mu.Unlock()
	if !found {
		return false
	}
	s.Kill()
	return true
}

// shutdown kills every session and waits (bounded) for their teardown.
func (r *registry) shutdown(timeout time.Duration) {
	r.mu.Lock()
	sessions := make([]liveSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()

	for _, s := range sessions {
		s.Kill()
	}
	deadline := time.After(timeout)
	for _, s := range sessions {
		select {
		case <-s.Exited():
		case <-deadline:
			return
		}
	}
}

// serveControl answers request/response ops on the control socket.
func (r *registry) serveControl(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Closed listener = shutdown; anything else is transient (EMFILE),
			// where returning would kill the control socket for the daemon's life.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Warn("control accept error, retrying", "error", err)
			time.Sleep(10 * time.Millisecond)
			continue
		}
		go r.handleControlConn(conn)
	}
}

// handleControlConn serves one request/response exchange.
func (r *registry) handleControlConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	typ, payload, err := protocol.ReadFrame(conn)
	if err != nil || typ != protocol.FrameRequest {
		return
	}
	var req protocol.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		_ = protocol.WriteJSONFrame(conn, protocol.FrameResponse, protocol.Response{Error: "bad request"})
		return
	}

	var resp protocol.Response
	switch req.Op {
	case protocol.OpPing:
		resp.OK = true
	case protocol.OpList:
		resp.OK = true
		resp.Sessions = r.list()
	case protocol.OpSpawn:
		name, err := r.spawn(req.CWD, req.UUID, req.Resume, req.Kind)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Name = name
		}
	case protocol.OpKill:
		if r.kill(req.Name) {
			resp.OK = true
		} else {
			resp.NotFound = true
			resp.Error = "session not found: " + req.Name
		}
	default:
		resp.Error = "unknown op: " + req.Op
	}
	if err := protocol.WriteJSONFrame(conn, protocol.FrameResponse, resp); err != nil {
		slog.Debug("control response write failed", "error", err)
	}
}
