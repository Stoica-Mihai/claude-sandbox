package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"

	"claude-sandbox-sessiond/protocol"
)

const (
	// viewerWriteTimeout bounds every frame write in the viewer writer.
	viewerWriteTimeout = 10 * time.Second
	// viewerQueueSize is the per-viewer outbound buffer; a viewer that falls
	// this far behind is evicted instead of blocking the actor.
	viewerQueueSize = 256
	// killGracePeriod is how long a kill waits after SIGTERM before SIGKILL.
	killGracePeriod = 2 * time.Second
	// defaultCols/defaultRows seed the PTY before a viewer reports dimensions.
	defaultCols = 80
	defaultRows = 24
)

// viewerSize is a viewer's last reported terminal dimensions.
type viewerSize struct {
	cols uint16
	rows uint16
}

// viewerMsg is one outbound frame for a viewer's writer goroutine.
type viewerMsg struct {
	typ  byte
	data []byte
}

// viewer is the actor-owned state for one attached connection.
type viewer struct {
	conn      net.Conn
	out       chan viewerMsg // closed by the actor to end the writer
	size      viewerSize
	suspended bool
}

// sessCmd is a message to the session actor.
type sessCmd interface{ isSessCmd() }

type cmdAttach struct {
	conn       net.Conn
	cols, rows uint16
}
type cmdDetach struct{ conn net.Conn }
type cmdInput struct {
	conn net.Conn
	data []byte
}
type cmdResize struct {
	conn       net.Conn
	cols, rows uint16
}
type cmdOutput struct{ data []byte }
type cmdEnd struct{ reason string }
type cmdKill struct{}

func (cmdAttach) isSessCmd() {}
func (cmdDetach) isSessCmd() {}
func (cmdInput) isSessCmd()  {}
func (cmdResize) isSessCmd() {}
func (cmdOutput) isSessCmd() {}
func (cmdEnd) isSessCmd()    {}
func (cmdKill) isSessCmd()   {}

var deactivatedMsg, _ = json.Marshal(protocol.Control{Type: protocol.ControlDeactivated})

// inputWriteTimeout bounds a PTY input write so a program that stops reading
// stdin can't freeze the session actor. Variable so tests can shorten it.
var inputWriteTimeout = 2 * time.Second

// session hosts one claude process: it owns the PTY, mirrors output into the
// emulator, and fans out to attached viewers. All mutable state is owned by a
// single actor goroutine; exported methods are thin message senders.
type session struct {
	name    string
	cwd     string
	uuid    string
	created time.Time

	// onExit, when set before begin, runs once after teardown completes.
	onExit func()

	cmds   chan sessCmd
	exited chan struct{}

	// Actor-owned state. term is itself mutex-guarded, so tests may probe it.
	ptmx       *os.File
	cmd        *exec.Cmd
	waited     chan struct{} // closed when cmd.Wait returns
	term       *termState
	ln         net.Listener
	viewers    map[net.Conn]*viewer
	active     net.Conn // active viewer: the one the PTY size follows
	killReason string   // set when a kill op initiated teardown
}

func newSession(name, cwd, uuid string, created time.Time) *session {
	return &session{
		name:    name,
		cwd:     cwd,
		uuid:    uuid,
		created: created,
		cmds:    make(chan sessCmd, 64),
		exited:  make(chan struct{}),
		waited:  make(chan struct{}),
		viewers: make(map[net.Conn]*viewer),
		term:    newTermState(defaultCols, defaultRows),
	}
}

// pollableMaster rewraps an fd-backed file so os.File deadlines work: dup the
// fd, set it non-blocking, hand it to os.NewFile (which registers non-blocking
// fds with the runtime poller), and close the original blocking wrapper.
// Without this, SetWriteDeadline is a silent no-op and a program that stops
// reading stdin blocks the session actor in a raw write syscall.
func pollableMaster(f *os.File) (*os.File, error) {
	dupFd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		return nil, fmt.Errorf("dup pty master: %w", err)
	}
	if err := syscall.SetNonblock(dupFd, true); err != nil {
		_ = syscall.Close(dupFd)
		return nil, fmt.Errorf("set pty master non-blocking: %w", err)
	}
	nf := os.NewFile(uintptr(dupFd), f.Name())
	_ = f.Close()
	return nf, nil
}

// begin wires an open PTY (and optionally its command) into the session and
// starts the actor, read loop, and child watcher. It owns the pollability
// invariant: whatever fd arrives is rewrapped so write deadlines are real.
// Split from spawn so tests can inject a file pair with a nil cmd.
func (s *session) begin(ptmx *os.File, cmd *exec.Cmd) {
	if nf, err := pollableMaster(ptmx); err == nil {
		ptmx = nf
	} else {
		slog.Warn("pty master not pollable; write deadlines degraded", "session", s.name, "error", err)
	}
	s.ptmx = ptmx
	s.cmd = cmd
	s.applyResize(defaultCols, defaultRows)
	go s.run()
	go s.readLoop(ptmx)
	if cmd != nil {
		go func() {
			_ = cmd.Wait()
			close(s.waited)
			s.send(cmdEnd{reason: protocol.CloseEnded})
		}()
	}
}

// serve accepts attach connections on the session socket.
func (s *session) serve(ln net.Listener) {
	s.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.readConn(conn)
		}
	}()
}

// Kill asks the actor to terminate the session (SIGTERM → grace → SIGKILL).
func (s *session) Kill() { s.send(cmdKill{}) }

// Exited reports actor teardown completion.
func (s *session) Exited() <-chan struct{} { return s.exited }

// send delivers a command; false if the session has exited. The exited
// pre-check keeps the answer deterministic once Exited() is observed closed —
// a buffered channel send and a closed-channel receive are otherwise both
// ready and select picks randomly.
func (s *session) send(c sessCmd) bool {
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

// run is the actor loop: it owns all session state and serializes commands.
func (s *session) run() {
	for c := range s.cmds {
		if s.handle(c) {
			s.teardown()
			return
		}
	}
}

// handle dispatches one actor command; true means tear down.
func (s *session) handle(c sessCmd) bool {
	switch c := c.(type) {
	case cmdAttach:
		s.handleAttach(c)
	case cmdDetach:
		s.dropViewer(c.conn)
	case cmdInput:
		s.handleInput(c)
	case cmdResize:
		s.handleResize(c)
	case cmdOutput:
		s.handleOutput(c.data)
	case cmdKill:
		return s.handleKill()
	case cmdEnd:
		if s.killReason == "" {
			s.killReason = c.reason
		}
		return true
	}
	return false
}

// handleKill starts termination: mark the reason and signal the child.
// Teardown happens when the child-exit watcher fires; with no process (tests)
// teardown is immediate.
func (s *session) handleKill() bool {
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
// escalation after the grace period. Safe off the actor: cmd is immutable
// after begin and processAlive reads only the waited channel.
func (s *session) terminateChild() {
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(killGracePeriod, func() {
		if s.processAlive() {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
}

// processAlive reports whether the session's child process is still running.
func (s *session) processAlive() bool {
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

// teardown closes the listener, notifies viewers, releases the PTY and
// emulator, drains racing commands, and fires onExit.
func (s *session) teardown() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	if s.processAlive() {
		// PTY died or daemon is stopping while the child lives: best-effort kill;
		// the watcher goroutine reaps it.
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

	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	s.term.Close()

	// Reject commands buffered before shutdown; close(exited) unblocks any
	// sender that enqueues after this drain.
	for {
		select {
		case <-s.cmds:
		default:
			close(s.exited)
			slog.Info("session ended", "session", s.name, "reason", reason)
			if s.onExit != nil {
				s.onExit()
			}
			return
		}
	}
}

// handleAttach registers a viewer, makes it the active one, and sends the
// snapshot rendered at its dimensions (the resize happened first).
func (s *session) handleAttach(c cmdAttach) {
	v := &viewer{conn: c.conn, out: make(chan viewerMsg, viewerQueueSize), size: viewerSize{c.cols, c.rows}}
	go s.viewerWriter(v)
	s.viewers[c.conn] = v
	s.activateViewer(c.conn, v)
	if !s.enqueue(v, viewerMsg{protocol.FrameSnapshot, s.term.Snapshot()}) {
		s.dropViewer(c.conn)
	}
}

// dropViewer removes a viewer and ends its writer.
func (s *session) dropViewer(conn net.Conn) {
	v, ok := s.viewers[conn]
	if !ok {
		return
	}
	delete(s.viewers, conn)
	if s.active == conn {
		s.active = nil
	}
	close(v.out)
}

// handleInput unsuspends the sender, makes it the active viewer, and writes
// the bytes to the PTY under a write deadline.
func (s *session) handleInput(c cmdInput) {
	v, ok := s.viewers[c.conn]
	if ok {
		v.suspended = false
		s.activateViewer(c.conn, v)
	}
	if s.ptmx == nil {
		return
	}
	if err := s.ptmx.SetWriteDeadline(time.Now().Add(inputWriteTimeout)); err == nil {
		defer func() { _ = s.ptmx.SetWriteDeadline(time.Time{}) }()
	} else {
		// Deadline unsupported means the fd bypassed pollableMaster — a stalled
		// program could then wedge this actor; make that visible.
		slog.Warn("pty write deadline unsupported", "session", s.name, "error", err)
	}
	if _, err := s.ptmx.Write(c.data); err != nil {
		slog.Warn("input write failed", "session", s.name, "error", err)
		if ok {
			msg, _ := json.Marshal(protocol.Control{Type: protocol.ControlError, Message: "input write failed: " + err.Error()})
			if !s.enqueue(v, viewerMsg{protocol.FrameControl, msg}) {
				s.dropViewer(c.conn)
			}
		}
	}
}

// activateViewer makes conn the active viewer: others are suspended (broadcast
// skips them) and told to clear on next input, and the PTY resizes to the new
// active viewer's dimensions.
func (s *session) activateViewer(conn net.Conn, v *viewer) {
	if s.active == conn || v.size.cols == 0 || v.size.rows == 0 {
		return
	}
	s.active = conn
	for other, ov := range s.viewers {
		if other == conn {
			continue
		}
		ov.suspended = true
		if !s.enqueue(ov, viewerMsg{protocol.FrameControl, deactivatedMsg}) {
			s.dropViewer(other)
		}
	}
	s.applyResize(v.size.cols, v.size.rows)
}

// handleResize stores a viewer's dimensions; the PTY follows only when that
// viewer is the active one. The active slot is only ever assigned to a
// registered viewer, so a resize racing an eviction cannot pin the size to a
// dead connection.
func (s *session) handleResize(c cmdResize) {
	v, ok := s.viewers[c.conn]
	if !ok {
		return
	}
	v.size = viewerSize{c.cols, c.rows}
	if s.active == nil {
		s.active = c.conn
	}
	if s.active == c.conn {
		s.applyResize(c.cols, c.rows)
	}
}

// applyResize resizes the emulator and PTY (the kernel delivers SIGWINCH to
// the foreground process group).
func (s *session) applyResize(cols, rows uint16) {
	s.term.Resize(cols, rows)
	if s.ptmx != nil {
		if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
			slog.Debug("resize failed", "session", s.name, "error", err)
		}
	}
}

// handleOutput broadcasts one PTY chunk to non-suspended viewers and feeds the
// emulator.
func (s *session) handleOutput(data []byte) {
	for conn, v := range s.viewers {
		if v.suspended {
			continue
		}
		if !s.enqueue(v, viewerMsg{protocol.FrameData, data}) {
			slog.Debug("viewer queue full, evicting", "session", s.name)
			s.dropViewer(conn)
		}
	}
	s.term.Write(data)
}

// enqueue offers a message to a viewer's writer without blocking the actor.
func (s *session) enqueue(v *viewer, m viewerMsg) bool {
	select {
	case v.out <- m:
		return true
	default:
		return false
	}
}

// viewerWriter drains one viewer's outbound queue onto its connection. On a
// write failure it asks the actor to drop the viewer, then drains until the
// actor closes the queue.
func (s *session) viewerWriter(v *viewer) {
	conn := v.conn
	for m := range v.out {
		_ = conn.SetWriteDeadline(time.Now().Add(viewerWriteTimeout))
		if err := protocol.WriteFrame(conn, m.typ, m.data); err != nil {
			slog.Debug("viewer write error, evicting", "session", s.name, "error", err)
			s.send(cmdDetach{conn: conn})
			for range v.out {
			}
			_ = conn.Close()
			return
		}
	}
	_ = conn.Close()
}

// readConn handles one attach connection: an ATTACH handshake with real
// dimensions, then input DATA and resize CONTROL frames.
func (s *session) readConn(conn net.Conn) {
	// One buffered reader per connection: header+payload (and adjacent small
	// frames) collapse into single reads on this per-keystroke path.
	br := bufio.NewReader(conn)
	typ, payload, err := protocol.ReadFrame(br)
	if err != nil || typ != protocol.FrameAttach {
		_ = conn.Close()
		return
	}
	var att protocol.Attach
	if json.Unmarshal(payload, &att) != nil || att.Cols == 0 || att.Rows == 0 {
		_ = conn.Close()
		return
	}
	if !s.send(cmdAttach{conn: conn, cols: att.Cols, rows: att.Rows}) {
		_ = conn.Close()
		return
	}

	for {
		typ, payload, err := protocol.ReadFrame(br)
		if err != nil {
			s.send(cmdDetach{conn: conn})
			return
		}
		switch typ {
		case protocol.FrameData:
			if !s.send(cmdInput{conn: conn, data: payload}) {
				return
			}
		case protocol.FrameControl:
			var ctl protocol.Control
			if json.Unmarshal(payload, &ctl) != nil {
				continue
			}
			if ctl.Type == protocol.ControlResize && ctl.Cols > 0 && ctl.Rows > 0 {
				if !s.send(cmdResize{conn: conn, cols: ctl.Cols, rows: ctl.Rows}) {
					return
				}
			}
		}
	}
}

// readLoop reads the PTY and forwards chunks to the actor. A read error means
// the child side is gone; the actor tears down (idempotent with cmd.Wait).
func (s *session) readLoop(f *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			if !s.send(cmdOutput{data: data}) {
				return
			}
		}
		if err != nil {
			s.send(cmdEnd{reason: protocol.CloseEnded})
			return
		}
	}
}
