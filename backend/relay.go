package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	// termReset resets the terminal to its initial state.
	termReset = "\x1bc"
	// attachWaitTimeout bounds how long Start waits for the dtach socket to appear.
	attachWaitTimeout = 5 * time.Second
	// maxReconnectAttempts is the number of attach reconnect attempts before giving up.
	maxReconnectAttempts = 3
	// viewerWriteTimeout bounds every WebSocket write in the viewer writer.
	viewerWriteTimeout = 10 * time.Second
	// viewerQueueSize is the per-viewer outbound message buffer; a viewer that
	// falls this far behind is evicted instead of blocking the actor.
	viewerQueueSize = 256
	// defaultCols/defaultRows seed the PTY before a viewer reports real dimensions.
	defaultCols = 80
	defaultRows = 24
)

var (
	errRelayStopped = errors.New("relay stopped")
	errNotAttached  = errors.New("attach not connected")
)

// viewerSize is a viewer's last reported terminal dimensions.
type viewerSize struct {
	cols uint16
	rows uint16
}

// viewerMsg is one outbound WebSocket message for a viewer's writer goroutine.
type viewerMsg struct {
	msgType int
	data    []byte
}

// viewerHandle is the actor-owned state for one WebSocket viewer.
type viewerHandle struct {
	conn      *websocket.Conn
	out       chan viewerMsg // closed by the actor to end the writer
	size      viewerSize
	suspended bool
	closeCode int // close frame the writer sends on queue close; 0 = none (abnormal, client reconnects)
}

// relayCmd is a message to the relay actor.
type relayCmd interface{ isRelayCmd() }

type cmdAddViewer struct {
	conn  *websocket.Conn
	reply chan error
}
type cmdRemoveViewer struct{ conn *websocket.Conn }
type cmdInput struct {
	conn  *websocket.Conn
	data  []byte
	reply chan error
}
type cmdResize struct {
	conn       *websocket.Conn
	cols, rows uint16
}
type cmdOutput struct {
	gen  int
	data []byte
}
type cmdAttachEOF struct{ gen int }
type cmdAttachResult struct {
	gen int
	f   *os.File
	cmd *exec.Cmd
}

func (cmdAddViewer) isRelayCmd()    {}
func (cmdRemoveViewer) isRelayCmd() {}
func (cmdInput) isRelayCmd()        {}
func (cmdResize) isRelayCmd()       {}
func (cmdOutput) isRelayCmd()       {}
func (cmdAttachEOF) isRelayCmd()    {}
func (cmdAttachResult) isRelayCmd() {}

// deactivatedMsg tells a non-active viewer its display is frozen and needs a
// clear on next input.
var deactivatedMsg = []byte(`{"type":"deactivated"}`)

// Relay connects a dtach session to WebSocket viewers. All mutable state is
// owned by a single actor goroutine (run); the exported methods are thin
// message senders, so there is no shared-state locking to reason about.
type Relay struct {
	SessionName string
	sockPath    string

	// onExit, when set before Start, is called exactly once from the actor
	// goroutine after teardown completes.
	onExit func()

	cmds     chan relayCmd
	done     chan struct{} // closed by Stop: shutdown requested
	exited   chan struct{} // closed by the actor: teardown complete
	stopOnce sync.Once

	// Actor-owned state. Touched only by the actor goroutine. term is itself
	// mutex-guarded, so tests may probe it from outside the actor.
	viewers     map[*websocket.Conn]*viewerHandle
	lastResizer *websocket.Conn // active viewer: last to type (or first to resize)
	ptmx        *os.File        // current attach PTY master; nil while reconnecting
	attachCmd   *exec.Cmd
	gen         int // attach epoch; stale readLoop messages are dropped
	lastCols    uint16
	lastRows    uint16
	term        *termState
}

// NewRelay creates a relay for the given session.
func NewRelay(sessionName string) *Relay {
	return &Relay{
		SessionName: sessionName,
		sockPath:    sockPath(sessionName),
		cmds:        make(chan relayCmd, 64),
		done:        make(chan struct{}),
		exited:      make(chan struct{}),
		viewers:     make(map[*websocket.Conn]*viewerHandle),
		term:        newTermState(defaultCols, defaultRows),
	}
}

// Start attaches to the session and starts the actor and read loops.
func (r *Relay) Start() error {
	f, cmd, err := r.startAttach()
	if err != nil {
		r.term.Close() // the relay is abandoned; end the drain goroutine
		return err
	}
	r.begin(f, cmd)
	slog.Info("relay started", "session", r.SessionName)
	return nil
}

// begin wires an already-open attach PTY into the relay and starts the actor
// and read loops. Split from Start so tests can inject a file pair.
func (r *Relay) begin(f *os.File, cmd *exec.Cmd) {
	r.ptmx = f
	r.attachCmd = cmd
	go r.run()
	go r.readLoop(f, r.gen)
}

// Stop requests shutdown. Teardown happens asynchronously on the actor.
func (r *Relay) Stop() {
	r.stopOnce.Do(func() { close(r.done) })
}

// IsStopped reports whether shutdown has been requested.
func (r *Relay) IsStopped() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// AddViewer registers a WebSocket connection: the actor replays the ring
// buffer into the viewer's queue and adds it to the broadcast set.
func (r *Relay) AddViewer(conn *websocket.Conn) error {
	return r.request(cmdAddViewer{conn: conn, reply: make(chan error, 1)})
}

// RemoveViewer unregisters a WebSocket connection.
func (r *Relay) RemoveViewer(conn *websocket.Conn) {
	r.send(cmdRemoveViewer{conn: conn})
}

// Input delivers terminal input from a viewer: it unsuspends the viewer, makes
// it the active one (resizing the PTY to its dimensions, tmux "window-size
// latest" style), and writes the bytes to the session.
func (r *Relay) Input(conn *websocket.Conn, data []byte) error {
	return r.request(cmdInput{conn: conn, data: data, reply: make(chan error, 1)})
}

// Resize records a viewer's dimensions; the PTY follows only if that viewer is
// the active one. It does not change who the active viewer is — Input does.
func (r *Relay) Resize(conn *websocket.Conn, cols, rows uint16) {
	r.send(cmdResize{conn: conn, cols: cols, rows: rows})
}

// send delivers a fire-and-forget command; false if the relay has exited.
func (r *Relay) send(c relayCmd) bool {
	select {
	case r.cmds <- c:
		return true
	case <-r.exited:
		return false
	}
}

// request delivers a command carrying a reply channel and waits for the answer.
func (r *Relay) request(c relayCmd) error {
	var reply chan error
	switch c := c.(type) {
	case cmdAddViewer:
		reply = c.reply
	case cmdInput:
		reply = c.reply
	default:
		panic("request: command has no reply channel")
	}
	select {
	case r.cmds <- c:
		select {
		case err := <-reply:
			return err
		case <-r.exited:
			return errRelayStopped
		}
	case <-r.exited:
		return errRelayStopped
	}
}

// run is the actor loop: it owns all relay state and serializes every command.
func (r *Relay) run() {
	defer r.teardown()
	for {
		select {
		case <-r.done:
			return
		case c := <-r.cmds:
			r.handle(c)
		}
	}
}

// handle dispatches one actor command.
func (r *Relay) handle(c relayCmd) {
	switch c := c.(type) {
	case cmdAddViewer:
		r.handleAddViewer(c)
	case cmdRemoveViewer:
		r.dropViewer(c.conn, 0)
	case cmdInput:
		r.handleInput(c)
	case cmdResize:
		r.handleResize(c)
	case cmdOutput:
		r.handleOutput(c)
	case cmdAttachEOF:
		r.handleAttachEOF(c)
	case cmdAttachResult:
		r.handleAttachResult(c)
	}
}

// teardown kills the attach, closes every viewer, cleans the upload dir,
// answers any commands that raced with shutdown, and fires onExit.
func (r *Relay) teardown() {
	if r.attachCmd != nil && r.attachCmd.Process != nil {
		_ = r.attachCmd.Process.Kill()
		go func(cmd *exec.Cmd) { _ = cmd.Wait() }(r.attachCmd)
	}
	if r.ptmx != nil {
		r.ptmx.Close()
	}
	r.term.Close()
	for conn := range r.viewers {
		r.dropViewer(conn, websocket.CloseNormalClosure)
	}

	// Reject commands buffered before shutdown; close(exited) unblocks any
	// sender that enqueues after this drain.
	for {
		select {
		case c := <-r.cmds:
			r.reject(c)
		default:
			close(r.exited)
			slog.Info("relay stopped", "session", r.SessionName)
			if r.onExit != nil {
				r.onExit()
			}
			return
		}
	}
}

// reject answers a command that arrived during shutdown.
func (r *Relay) reject(c relayCmd) {
	switch c := c.(type) {
	case cmdAddViewer:
		c.reply <- errRelayStopped
	case cmdInput:
		c.reply <- errRelayStopped
	case cmdAttachResult:
		c.f.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			go func(cmd *exec.Cmd) { _ = cmd.Wait() }(c.cmd)
		}
	}
}

// handleAddViewer starts the viewer's writer, queues the terminal snapshot
// (reset + scrollback + screen + cursor), and adds it to the broadcast set.
// Replay and registration are one actor step, so no broadcast can interleave
// between them.
func (r *Relay) handleAddViewer(c cmdAddViewer) {
	v := &viewerHandle{conn: c.conn, out: make(chan viewerMsg, viewerQueueSize)}
	go r.viewerWriter(v)

	v.out <- viewerMsg{websocket.BinaryMessage, r.term.Snapshot()}

	r.viewers[c.conn] = v
	c.reply <- nil
}

// dropViewer removes a viewer and ends its writer (which sends the close frame).
func (r *Relay) dropViewer(conn *websocket.Conn, closeCode int) {
	v, ok := r.viewers[conn]
	if !ok {
		return
	}
	delete(r.viewers, conn)
	if r.lastResizer == conn {
		r.lastResizer = nil
	}
	v.closeCode = closeCode
	close(v.out)
}

// handleInput unsuspends the sender, makes it the active viewer, and writes
// the bytes to the attach PTY.
func (r *Relay) handleInput(c cmdInput) {
	if v, ok := r.viewers[c.conn]; ok {
		v.suspended = false
		r.activateViewer(c.conn, v)
	}
	if r.ptmx == nil {
		c.reply <- errNotAttached
		return
	}
	_, err := r.ptmx.Write(c.data)
	c.reply <- err
}

// activateViewer makes conn the active viewer: other viewers are suspended
// (broadcast skips them) and told to clear on next input, and the PTY resizes
// to the new active viewer's dimensions.
func (r *Relay) activateViewer(conn *websocket.Conn, v *viewerHandle) {
	if r.lastResizer == conn || v.size.cols == 0 || v.size.rows == 0 {
		return
	}
	r.lastResizer = conn
	for other, ov := range r.viewers {
		if other == conn {
			continue
		}
		ov.suspended = true
		if !r.enqueue(ov, viewerMsg{websocket.TextMessage, deactivatedMsg}) {
			r.dropViewer(other, 0)
		}
	}
	r.applyResize(v.size.cols, v.size.rows)
}

// handleResize stores a viewer's dimensions and resizes the PTY only when that
// viewer is the active one (the first viewer to resize becomes active).
func (r *Relay) handleResize(c cmdResize) {
	if v, ok := r.viewers[c.conn]; ok {
		v.size = viewerSize{c.cols, c.rows}
	}
	if r.lastResizer == nil {
		r.lastResizer = c.conn
	}
	if r.lastResizer == c.conn {
		r.applyResize(c.cols, c.rows)
	}
}

// applyResize records the dimensions and resizes the attach PTY, which dtach
// forwards to the inner program as SIGWINCH.
func (r *Relay) applyResize(cols, rows uint16) {
	r.lastCols, r.lastRows = cols, rows
	r.term.Resize(cols, rows)
	if r.ptmx != nil {
		if err := pty.Setsize(r.ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
			slog.Debug("resize failed", "session", r.SessionName, "error", err)
		}
	}
}

// handleOutput routes one PTY output chunk: broadcast to non-suspended
// viewers and feed the terminal-state emulator.
func (r *Relay) handleOutput(c cmdOutput) {
	if c.gen != r.gen {
		return
	}

	for conn, v := range r.viewers {
		if v.suspended {
			continue
		}
		if !r.enqueue(v, viewerMsg{websocket.BinaryMessage, c.data}) {
			slog.Debug("viewer queue full, evicting", "session", r.SessionName)
			r.dropViewer(conn, 0)
		}
	}

	r.term.Write(c.data)
}

// handleAttachEOF reacts to the attach PTY dropping: it invalidates the old
// epoch and starts an async reconnect.
func (r *Relay) handleAttachEOF(c cmdAttachEOF) {
	if c.gen != r.gen {
		return
	}
	if r.ptmx != nil {
		r.ptmx.Close()
		r.ptmx = nil
	}
	if r.attachCmd != nil {
		if r.attachCmd.Process != nil {
			_ = r.attachCmd.Process.Kill()
		}
		go func(cmd *exec.Cmd) { _ = cmd.Wait() }(r.attachCmd)
		r.attachCmd = nil
	}
	r.gen++
	go r.reconnectLoop(r.gen)
}

// handleAttachResult installs a freshly reconnected attach PTY and starts its
// read loop.
func (r *Relay) handleAttachResult(c cmdAttachResult) {
	if c.gen != r.gen || r.ptmx != nil {
		r.reject(c)
		return
	}
	r.ptmx = c.f
	r.attachCmd = c.cmd
	// Only impose a size when a browser viewer is present, so the relay doesn't
	// clobber a CLI-owned session's dimensions.
	if len(r.viewers) > 0 {
		cols, rows := r.lastCols, r.lastRows
		if cols == 0 || rows == 0 {
			cols, rows = defaultCols, defaultRows
		}
		r.applyResize(cols, rows)
	}
	slog.Info("relay reconnected", "session", r.SessionName)
	go r.readLoop(c.f, c.gen)
}

// enqueue offers a message to a viewer's writer without blocking the actor.
func (r *Relay) enqueue(v *viewerHandle, m viewerMsg) bool {
	select {
	case v.out <- m:
		return true
	default:
		return false
	}
}

// viewerWriter drains one viewer's outbound queue onto its WebSocket. On a
// write failure it asks the actor to evict the viewer, then drains until the
// actor closes the queue. On queue close it sends the close frame the actor
// chose (session ended) or none (eviction — the client sees an abnormal close
// and reconnects).
func (r *Relay) viewerWriter(v *viewerHandle) {
	conn := v.conn
	for m := range v.out {
		_ = conn.SetWriteDeadline(time.Now().Add(viewerWriteTimeout))
		if err := conn.WriteMessage(m.msgType, m.data); err != nil {
			slog.Debug("viewer write error, evicting", "session", r.SessionName, "error", err)
			r.send(cmdRemoveViewer{conn: conn})
			for range v.out {
			}
			_ = conn.Close()
			return
		}
	}
	// closeCode is set by the actor before close(out), so this read is ordered.
	if v.closeCode != 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(viewerWriteTimeout))
		closeMsg := websocket.FormatCloseMessage(v.closeCode, "session ended")
		_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
	}
	_ = conn.Close()
}

// readLoop reads the attach PTY and forwards chunks to the actor. It exits on
// read error (notifying the actor) or when the relay has exited.
func (r *Relay) readLoop(f *os.File, gen int) {
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			if !r.send(cmdOutput{gen: gen, data: data}) {
				return
			}
		}
		if err != nil {
			slog.Debug("relay read ended", "session", r.SessionName, "error", err)
			r.send(cmdAttachEOF{gen: gen})
			return
		}
	}
}

// waitForSocket blocks until the dtach socket exists or the timeout elapses.
func (r *Relay) waitForSocket() error {
	deadline := time.Now().Add(attachWaitTimeout)
	for {
		if _, err := os.Stat(r.sockPath); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dtach socket %s did not appear", r.sockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// startAttach launches `dtach -a` under a fresh PTY.
func (r *Relay) startAttach() (*os.File, *exec.Cmd, error) {
	if err := r.waitForSocket(); err != nil {
		return nil, nil, err
	}
	// -r winch: dtach nudges the program with SIGWINCH on attach, so it repaints
	// and repopulates the terminal-state emulator after a backend restart.
	cmd := exec.Command("dtach", "-a", r.sockPath, "-E", "-z", "-r", "winch")
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("dtach attach failed: %w", err)
	}
	return f, cmd, nil
}

// reconnectLoop tries to re-establish the attach after a drop, posting the new
// PTY back to the actor. If the session master is gone or attempts are
// exhausted, it stops the relay.
func (r *Relay) reconnectLoop(gen int) {
	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		if r.IsStopped() {
			return
		}
		if !sessionAlive(r.SessionName) {
			slog.Info("session gone, stopping relay", "session", r.SessionName)
			r.Stop()
			return
		}

		slog.Info("relay reconnecting", "session", r.SessionName, "attempt", attempt)
		time.Sleep(time.Duration(attempt) * time.Second)

		f, cmd, err := r.startAttach()
		if err != nil {
			slog.Warn("reconnect attach failed", "session", r.SessionName, "error", err)
			continue
		}
		if !r.send(cmdAttachResult{gen: gen, f: f, cmd: cmd}) {
			f.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			go func() { _ = cmd.Wait() }()
		}
		return
	}

	slog.Error(fmt.Sprintf("relay reconnect failed after %d attempts", maxReconnectAttempts), "session", r.SessionName)
	r.Stop()
}

