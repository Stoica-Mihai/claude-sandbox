package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// chatViewer is the actor-owned state for one attached chat viewer connection.
// Unlike the terminal viewer, there is no size or suspended state — every
// registered viewer is always live and gets every broadcast line (pure
// broadcast, per chat-session-host).
type chatViewer struct {
	conn net.Conn
	out  chan viewerMsg // closed by the actor to end the writer
}

// chatCmd is a message to the chat session actor.
type chatCmd interface{ isChatCmd() }

type chatCmdAttach struct{ conn net.Conn }
type chatCmdDetach struct{ conn net.Conn }
type chatCmdInput struct {
	conn net.Conn
	data []byte
}
type chatCmdOutput struct{ data []byte }
type chatCmdEnd struct{ reason string }
type chatCmdKill struct{}

func (chatCmdAttach) isChatCmd() {}
func (chatCmdDetach) isChatCmd() {}
func (chatCmdInput) isChatCmd()  {}
func (chatCmdOutput) isChatCmd() {}
func (chatCmdEnd) isChatCmd()    {}
func (chatCmdKill) isChatCmd()   {}

// chatSession hosts one claude pipe-child in stream-json mode: it owns
// stdin/stdout and fans out each stdout line to every attached viewer
// verbatim, in submission order for stdin writes. It deliberately has none of
// the terminal actor's machinery — no PTY, no emulator, no snapshot, no
// active-viewer arbitration, no JSON interpretation — see the
// chat-session-host capability.
type chatSession struct {
	name    string
	cwd     string
	uuid    string
	created time.Time

	// onExit, when set before begin, runs once after teardown completes.
	onExit func()

	cmds   chan chatCmd
	exited chan struct{}

	// Actor-owned state.
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	waited     chan struct{} // closed when cmd.Wait returns
	ln         net.Listener
	viewers    map[net.Conn]*chatViewer
	killReason string // set when a kill op initiated teardown
}

func newChatSession(name, cwd, uuid string, created time.Time) *chatSession {
	return &chatSession{
		name:    name,
		cwd:     cwd,
		uuid:    uuid,
		created: created,
		cmds:    make(chan chatCmd, 64),
		exited:  make(chan struct{}),
		waited:  make(chan struct{}),
		viewers: make(map[net.Conn]*chatViewer),
	}
}

// begin starts cmd as a pipe-child (stdin/stdout piped, no PTY) and starts the
// actor, the stdout read loop, and the child watcher. cmd.SysProcAttr is set
// so the child is its own process group leader (Setpgid), matching the
// terminal kind's process-group kill semantics (SIGTERM/SIGKILL to -pid) even
// though there is no controlling terminal to make that happen implicitly.
func (s *chatSession) begin(cmd *exec.Cmd) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("chat stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("chat stdout pipe: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting claude (chat): %w", err)
	}
	s.cmd = cmd
	s.stdin = stdin
	go s.run()
	go s.readLoop(stdout)
	go func() {
		_ = cmd.Wait()
		close(s.waited)
		s.send(chatCmdEnd{reason: protocol.CloseEnded})
	}()
	return nil
}

// serve accepts attach connections on the session socket.
func (s *chatSession) serve(ln net.Listener) {
	s.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Warn("chat session accept error, retrying", "session", s.name, "error", err)
				time.Sleep(10 * time.Millisecond)
				continue
			}
			go s.readConn(conn)
		}
	}()
}

// Kill asks the actor to terminate the session (SIGTERM → grace → SIGKILL).
func (s *chatSession) Kill() { s.send(chatCmdKill{}) }

// Exited reports actor teardown completion.
func (s *chatSession) Exited() <-chan struct{} { return s.exited }

// info reports this session's registry-visible metadata.
func (s *chatSession) info() protocol.SessionInfo {
	return protocol.SessionInfo{
		Name:    s.name,
		CWD:     s.cwd,
		UUID:    s.uuid,
		Created: s.created.Unix(),
		Kind:    protocol.KindChat,
	}
}

// send delivers a command; false if the session has exited.
func (s *chatSession) send(c chatCmd) bool {
	select {
	case <-s.exited:
		return false
	default:
	}
	select {
	case s.cmds <- c:
		return true
	case <-s.exited:
		return false
	}
}

// run is the actor loop: it owns all chat session state and serializes
// commands, exactly like the terminal actor's run loop.
func (s *chatSession) run() {
	for c := range s.cmds {
		if s.handle(c) {
			s.teardown()
			return
		}
	}
}

// handle dispatches one actor command; true means tear down.
func (s *chatSession) handle(c chatCmd) bool {
	switch c := c.(type) {
	case chatCmdAttach:
		s.handleAttach(c)
	case chatCmdDetach:
		s.dropViewer(c.conn)
	case chatCmdInput:
		s.handleInput(c)
	case chatCmdOutput:
		s.handleOutput(c.data)
	case chatCmdKill:
		return s.handleKill()
	case chatCmdEnd:
		if s.killReason == "" {
			s.killReason = c.reason
		}
		return true
	}
	return false
}

// handleKill starts termination: mark the reason and signal the child.
func (s *chatSession) handleKill() bool {
	if s.killReason == "" {
		s.killReason = protocol.CloseKilled
	}
	if !s.processAlive() {
		return true
	}
	s.terminateChild()
	return false
}

// terminateChild signals the process group SIGTERM and schedules a SIGKILL
// escalation after the grace period, mirroring the terminal actor.
func (s *chatSession) terminateChild() {
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(killGracePeriod, func() {
		if s.processAlive() {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
}

// processAlive reports whether the session's child process is still running.
func (s *chatSession) processAlive() bool {
	if s.cmd == nil || s.cmd.Process == nil {
		return false
	}
	select {
	case <-s.waited:
		return false
	default:
		return true
	}
}

// teardown closes the listener, notifies viewers, releases stdin, drains
// racing commands, and fires onExit — mirroring the terminal actor's teardown.
func (s *chatSession) teardown() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	if s.processAlive() {
		s.terminateChild()
	}

	reason := s.killReason
	if reason == "" {
		reason = protocol.CloseEnded
	}
	closePayload, _ := json.Marshal(protocol.Close{Reason: reason})
	for conn, v := range s.viewers {
		s.enqueue(v, viewerMsg{protocol.FrameClose, closePayload})
		delete(s.viewers, conn)
		close(v.out)
	}

	if s.stdin != nil {
		_ = s.stdin.Close()
	}

	for {
		select {
		case <-s.cmds:
		default:
			close(s.exited)
			slog.Info("chat session ended", "session", s.name, "reason", reason)
			if s.onExit != nil {
				s.onExit()
			}
			return
		}
	}
}

// handleAttach registers a viewer. No snapshot is sent (see chat-session-host)
// — the viewer starts receiving only events broadcast from this point onward.
func (s *chatSession) handleAttach(c chatCmdAttach) {
	v := &chatViewer{conn: c.conn, out: make(chan viewerMsg, viewerQueueSize)}
	go s.viewerWriter(v)
	s.viewers[c.conn] = v
}

// dropViewer removes a viewer and ends its writer.
func (s *chatSession) dropViewer(conn net.Conn) {
	v, ok := s.viewers[conn]
	if !ok {
		return
	}
	delete(s.viewers, conn)
	close(v.out)
}

// handleInput writes one input line to the child's stdin, in the order
// received (the single actor goroutine already serializes this — no separate
// queue is needed for the "queue while running" guarantee).
func (s *chatSession) handleInput(c chatCmdInput) {
	if s.stdin == nil {
		return
	}
	line := append(append([]byte(nil), c.data...), '\n')
	if _, err := s.stdin.Write(line); err != nil {
		slog.Warn("chat input write failed", "session", s.name, "error", err)
		if v, ok := s.viewers[c.conn]; ok {
			msg, _ := json.Marshal(protocol.Control{Type: protocol.ControlError, Message: "input write failed: " + err.Error()})
			if !s.enqueue(v, viewerMsg{protocol.FrameControl, msg}) {
				s.dropViewer(c.conn)
			}
		}
		return
	}
	// Mirror the accepted input to every OTHER viewer: the engine never echoes
	// user turns on its stream, and the sender already rendered its own
	// message locally — without this, co-viewers never see the user side of
	// the conversation.
	for conn, v := range s.viewers {
		if conn == c.conn {
			continue
		}
		if !s.enqueue(v, viewerMsg{protocol.FrameData, c.data}) {
			slog.Debug("chat viewer queue full, evicting", "session", s.name)
			s.dropViewer(conn)
		}
	}
}

// handleOutput broadcasts one stdout line to every viewer — pure broadcast,
// no suspension, no active-viewer concept.
func (s *chatSession) handleOutput(data []byte) {
	for conn, v := range s.viewers {
		if !s.enqueue(v, viewerMsg{protocol.FrameData, data}) {
			slog.Debug("chat viewer queue full, evicting", "session", s.name)
			s.dropViewer(conn)
		}
	}
}

// enqueue offers a message to a viewer's writer without blocking the actor.
func (s *chatSession) enqueue(v *chatViewer, m viewerMsg) bool {
	select {
	case v.out <- m:
		return true
	default:
		return false
	}
}

// viewerWriter drains one viewer's outbound queue onto its connection,
// mirroring the terminal actor's viewer writer.
func (s *chatSession) viewerWriter(v *chatViewer) {
	conn := v.conn
	for m := range v.out {
		_ = conn.SetWriteDeadline(time.Now().Add(viewerWriteTimeout))
		if err := protocol.WriteFrame(conn, m.typ, m.data); err != nil {
			slog.Debug("chat viewer write error, evicting", "session", s.name, "error", err)
			s.send(chatCmdDetach{conn: conn})
			for range v.out {
			}
			_ = conn.Close()
			return
		}
	}
	_ = conn.Close()
}

// readConn handles one attach connection: an ATTACH handshake (dimensions are
// not required for chat — see chat-session-host), then input DATA frames.
// Any other frame type is ignored: chat sessions have no resize/reactivate
// concept.
func (s *chatSession) readConn(conn net.Conn) {
	br := bufio.NewReader(conn)
	typ, _, err := protocol.ReadFrame(br)
	if err != nil || typ != protocol.FrameAttach {
		_ = conn.Close()
		return
	}
	if !s.send(chatCmdAttach{conn: conn}) {
		_ = conn.Close()
		return
	}

	for {
		typ, payload, err := protocol.ReadFrame(br)
		if err != nil {
			s.send(chatCmdDetach{conn: conn})
			return
		}
		if typ == protocol.FrameData {
			if !s.send(chatCmdInput{conn: conn, data: payload}) {
				return
			}
		}
	}
}

// readLoop reads the child's stdout line-by-line and forwards each complete
// line to the actor verbatim — sessiond never parses the JSON (see
// chat-session-host's "no JSON interpretation" invariant). A read error or
// EOF means the child side is gone; the actor tears down (idempotent with
// cmd.Wait). An oversized line is still forwarded to the actor; WriteFrame's
// existing MaxFrame check is what ultimately bounds it per viewer.
func (s *chatSession) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadString('\n')
		if trimmed := strings.TrimSuffix(line, "\n"); trimmed != "" {
			data := []byte(trimmed)
			if !s.send(chatCmdOutput{data: data}) {
				return
			}
		}
		if err != nil {
			s.send(chatCmdEnd{reason: protocol.CloseEnded})
			return
		}
	}
}
