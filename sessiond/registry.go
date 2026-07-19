package main

import (
	"crypto/rand"
	"encoding/hex"
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
	// termType is the TERM environment variable for new sessions.
	termType = "xterm-256color"
)

// setPTYSize resizes a PTY master.
func setPTYSize(f *os.File, cols, rows uint16) error {
	return pty.Setsize(f, &pty.Winsize{Cols: cols, Rows: rows})
}

// registry owns the live session set and serves the control-socket ops.
type registry struct {
	mu       sync.Mutex
	sessions map[string]*session
	sockDir  string
	// newCommand builds the session command; swapped by tests.
	newCommand func(cwd, uuid string, resume bool) *exec.Cmd
}

func newRegistry(sockDir string) *registry {
	return &registry{
		sessions:   make(map[string]*session),
		sockDir:    sockDir,
		newCommand: claudeCommand,
	}
}

// claudeCommand builds the real claude invocation.
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

// generateSessionName creates a session name like "claude-a1b2c3d4".
func generateSessionName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return sessionPrefix + hex.EncodeToString(b)
}

// spawn starts a claude session under an owned PTY and its attach listener.
func (r *registry) spawn(cwd, uuid string, resume bool) (string, error) {
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

		cmd := r.newCommand(cwd, uuid, resume)
		ptmx, err := pty.Start(cmd)
		if err != nil {
			_ = ln.Close()
			_ = os.Remove(sockPath)
			return "", fmt.Errorf("starting claude: %w", err)
		}

		s := newSession(name, cwd, uuid, time.Now())
		s.onExit = func() {
			_ = os.Remove(sockPath)
			r.mu.Lock()
			delete(r.sessions, name)
			r.mu.Unlock()
		}
		r.mu.Lock()
		r.sessions[name] = s
		r.mu.Unlock()

		s.begin(ptmx, cmd)
		s.serve(ln)
		slog.Info("spawned session", "session", name, "cwd", cwd, "uuid", uuid, "pid", cmd.Process.Pid)
		return name, nil
	}
	return "", fmt.Errorf("failed to create session after %d attempts", maxSpawnRetries)
}

// list returns every live session.
func (r *registry) list() []protocol.SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]protocol.SessionInfo, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, protocol.SessionInfo{
			Name:    s.name,
			CWD:     s.cwd,
			UUID:    s.uuid,
			Created: s.created.Unix(),
		})
	}
	return out
}

// kill terminates a session by name.
func (r *registry) kill(name string) error {
	r.mu.Lock()
	s, ok := r.sessions[name]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found: %s", name)
	}
	s.Kill()
	return nil
}

// shutdown kills every session and waits (bounded) for their teardown.
func (r *registry) shutdown(timeout time.Duration) {
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
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
			return
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
	if err := jsonUnmarshal(payload, &req); err != nil {
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
		name, err := r.spawn(req.CWD, req.UUID, req.Resume)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Name = name
		}
	case protocol.OpKill:
		if err := r.kill(req.Name); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	default:
		resp.Error = "unknown op: " + req.Op
	}
	if err := protocol.WriteJSONFrame(conn, protocol.FrameResponse, resp); err != nil {
		slog.Debug("control response write failed", "error", err)
	}
}
