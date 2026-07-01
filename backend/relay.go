package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	// termReset resets the terminal to its initial state.
	termReset = "\x1bc"
	// attachWaitTimeout bounds how long Start waits for the dtach socket to appear.
	attachWaitTimeout = 5 * time.Second
	// resizeActivityWindow suppresses activity stamping after resize redraws.
	resizeActivityWindow = 2 * time.Second
	// inputActivityWindow suppresses activity stamping for keystroke echoes.
	inputActivityWindow = 500 * time.Millisecond
	// maxReconnectAttempts is the number of attach reconnect attempts before giving up.
	maxReconnectAttempts = 3
	// viewerWriteTimeout bounds every WebSocket write. Without it a stalled
	// client (full TCP buffer) blocks the broadcast loop — and with it the read
	// loop — freezing output for every other viewer.
	viewerWriteTimeout = 10 * time.Second
	// defaultCols/defaultRows seed the PTY before a viewer reports real dimensions.
	defaultCols = 80
	defaultRows = 24
)

// Alternate screen sequences to detect and strip.
var altScreenEnter = [][]byte{
	[]byte("\x1b[?1049h"),
	[]byte("\x1b[?47h"),
}

var altScreenExit = [][]byte{
	[]byte("\x1b[?1049l"),
	[]byte("\x1b[?47l"),
}

// allAltScreenSeqs is the combined list for stripping.
var allAltScreenSeqs [][]byte

func init() {
	allAltScreenSeqs = append(allAltScreenSeqs, altScreenEnter...)
	allAltScreenSeqs = append(allAltScreenSeqs, altScreenExit...)
}

// viewerSize tracks a viewer's last reported terminal dimensions.
type viewerSize struct {
	cols uint16
	rows uint16
}

// viewer holds per-connection state for a WebSocket viewer.
type viewer struct {
	writeMu   sync.Mutex // serializes all WebSocket writes to this connection
	size      viewerSize
	suspended atomic.Bool // when true, broadcast skips this viewer
}

// Relay manages bidirectional I/O between WebSocket viewers and a session via a
// directly-owned `dtach -a` attach PTY.
type Relay struct {
	SessionName string
	sockPath    string
	ringBuf     *RingBuffer

	mu          sync.RWMutex
	viewers     map[*websocket.Conn]*viewer
	lastResizer *websocket.Conn // viewer that last triggered a resize

	ptmx       atomic.Pointer[os.File] // current attach PTY master (swapped on reconnect)
	attachCmd  atomic.Pointer[exec.Cmd]
	attachMu   sync.Mutex   // serializes reconnect/attach lifecycle
	generation atomic.Int64 // identifies the live read loop; stale loops exit

	lastActivity   time.Time
	lastActivityMu sync.RWMutex
	lastResizeAt   atomic.Int64 // unix-nano; suppress activity stamping after resizes
	lastInputAt    atomic.Int64 // unix-nano; suppress activity stamping for echoes
	lastCols       atomic.Uint32
	lastRows       atomic.Uint32

	inAltScreen atomic.Bool // true when Claude Code is in alternate screen mode
	partial     []byte      // partial escape sequence from previous read chunk (readLoop only)

	done     chan struct{} // closed when relay is stopped
	stopOnce sync.Once
}

// NewRelay creates a relay for the given session.
func NewRelay(sessionName string) *Relay {
	return &Relay{
		SessionName: sessionName,
		sockPath:    sockPath(sessionName),
		ringBuf:     NewRingBuffer(defaultBufferCapacity),
		viewers:     make(map[*websocket.Conn]*viewer),
		done:        make(chan struct{}),
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

// startAttach launches `dtach -a` under a new PTY the relay owns and applies the
// last known window size. Callers hold attachMu (or call before any read loop).
func (r *Relay) startAttach() (*os.File, *exec.Cmd, error) {
	if err := r.waitForSocket(); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command("dtach", "-a", r.sockPath, "-E", "-z", "-r", "none")
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("dtach attach failed: %w", err)
	}
	// Only impose a size when a browser viewer is present. Otherwise leave the
	// PTY at whatever an interactive CLI client set, so the relay doesn't
	// clobber a CLI-owned session's dimensions.
	r.mu.RLock()
	hasViewers := len(r.viewers) > 0
	r.mu.RUnlock()
	if hasViewers {
		r.applySize(f)
	}
	return f, cmd, nil
}

// applySize sets the PTY window size to the last reported dimensions (or the
// default seed). dtach does not adopt a fresh client's size automatically, so
// this must run on every attach.
func (r *Relay) applySize(f *os.File) {
	cols := uint16(r.lastCols.Load())
	rows := uint16(r.lastRows.Load())
	if cols == 0 || rows == 0 {
		cols, rows = defaultCols, defaultRows
	}
	_ = pty.Setsize(f, &pty.Winsize{Rows: rows, Cols: cols})
}

// Start attaches to the session and begins reading output into the ring buffer.
func (r *Relay) Start() error {
	f, cmd, err := r.startAttach()
	if err != nil {
		return err
	}
	r.ptmx.Store(f)
	r.attachCmd.Store(cmd)

	slog.Info("relay started", "session", r.SessionName)

	gen := r.generation.Add(1)
	go r.readLoop(gen)
	return nil
}

// Stop tears down the relay's attach and closes all viewers. It does NOT remove
// the dtach socket — the master owns it and removes it on exit.
func (r *Relay) Stop() {
	r.stopOnce.Do(func() {
		close(r.done)

		if cmd := r.attachCmd.Load(); cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if f := r.ptmx.Load(); f != nil {
			f.Close()
		}

		r.mu.Lock()
		for conn, v := range r.viewers {
			v.writeMu.Lock()
			closeMsg := websocket.FormatCloseMessage(
				websocket.CloseNormalClosure,
				"session ended",
			)
			_ = writeMessage(conn, websocket.CloseMessage, closeMsg)
			_ = conn.Close()
			v.writeMu.Unlock()
		}
		r.viewers = make(map[*websocket.Conn]*viewer)
		r.mu.Unlock()

		uploadPath := filepath.Join(uploadDir, r.SessionName)
		if err := os.RemoveAll(uploadPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to clean upload dir", "path", uploadPath, "error", err)
		}

		slog.Info("relay stopped", "session", r.SessionName)
	})
}

// writeMessage writes to a viewer connection with a deadline. Caller holds the
// viewer's writeMu (or the connection is not yet shared).
func writeMessage(conn *websocket.Conn, msgType int, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(viewerWriteTimeout))
	return conn.WriteMessage(msgType, data)
}

// AddViewer registers a WebSocket connection, replays the ring buffer, and adds
// it to the broadcast list. Replay and registration happen under the write lock
// so no broadcast can interleave between them — otherwise output produced in
// that window would be missing from the new viewer's stream.
func (r *Relay) AddViewer(conn *websocket.Conn) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := writeMessage(conn, websocket.BinaryMessage, []byte(termReset)); err != nil {
		return fmt.Errorf("send reset: %w", err)
	}

	scrollback := r.ringBuf.Bytes()
	if len(scrollback) > 0 {
		if err := writeMessage(conn, websocket.BinaryMessage, scrollback); err != nil {
			return fmt.Errorf("replay scrollback: %w", err)
		}
	}

	if r.inAltScreen.Load() {
		_ = writeMessage(conn, websocket.BinaryMessage, altScreenEnter[0])
	}

	r.viewers[conn] = &viewer{}
	return nil
}

// RemoveViewer unregisters a WebSocket connection.
func (r *Relay) RemoveViewer(conn *websocket.Conn) {
	r.mu.Lock()
	delete(r.viewers, conn)
	if r.lastResizer == conn {
		r.lastResizer = nil
	}
	r.mu.Unlock()
}

// SendInput writes user input bytes to the session via the attach PTY.
func (r *Relay) SendInput(data []byte) error {
	f := r.ptmx.Load()
	if f == nil {
		return fmt.Errorf("attach not connected")
	}
	r.lastInputAt.Store(time.Now().UnixNano())
	_, err := f.Write(data)
	return err
}

// Resize stores a viewer's terminal dimensions and resizes the PTY only if this
// viewer is the active one (last to type). It does NOT change who the active
// viewer is — only ResizeToViewer (input-triggered) does that.
func (r *Relay) Resize(conn *websocket.Conn, cols, rows uint16) {
	r.mu.Lock()
	if v, ok := r.viewers[conn]; ok {
		v.size = viewerSize{cols, rows}
	}
	if r.lastResizer == nil {
		r.lastResizer = conn
	}
	isActive := r.lastResizer == conn
	r.mu.Unlock()

	if isActive {
		r.applyResize(cols, rows)
	}
}

// deactivatedMsg tells a non-active viewer its display is frozen and needs a
// clear on next input.
var deactivatedMsg = []byte(`{"type":"deactivated"}`)

// ResizeToViewer resizes the PTY to match the given viewer's dimensions if that
// viewer is not already the last resizer. Mimics tmux "window-size latest" — the
// active typist's size wins. Non-active viewers are suspended (broadcast skips
// them) and receive a "deactivated" message so the client clears on next input.
func (r *Relay) ResizeToViewer(conn *websocket.Conn) {
	// Check + takeover + suspension marking happen in ONE critical section:
	// with the old check-RUnlock-Lock split, two concurrently-first typists
	// could both pass the check and each suspend the other, freezing both
	// until their next keystroke unsuspended them.
	r.mu.Lock()
	if r.lastResizer == conn {
		r.mu.Unlock()
		return
	}
	v, ok := r.viewers[conn]
	if !ok || v.size.cols == 0 || v.size.rows == 0 {
		r.mu.Unlock()
		return
	}
	r.lastResizer = conn
	type target struct {
		conn *websocket.Conn
		vw   *viewer
	}
	var others []target
	for c, vw := range r.viewers {
		if c == conn {
			continue
		}
		vw.suspended.Store(true)
		others = append(others, target{c, vw})
	}
	size := v.size
	r.mu.Unlock()

	// Network writes happen outside the lock.
	for _, t := range others {
		t.vw.writeMu.Lock()
		_ = writeMessage(t.conn, websocket.TextMessage, deactivatedMsg)
		t.vw.writeMu.Unlock()
	}

	r.applyResize(size.cols, size.rows)
}

// UnsuspendViewer resumes broadcast delivery for a viewer.
func (r *Relay) UnsuspendViewer(conn *websocket.Conn) {
	r.mu.RLock()
	v, ok := r.viewers[conn]
	r.mu.RUnlock()
	if ok {
		v.suspended.Store(false)
	}
}

// applyResize records the dimensions and resizes the attach PTY, which dtach
// forwards to the inner program as SIGWINCH.
func (r *Relay) applyResize(cols, rows uint16) {
	r.lastCols.Store(uint32(cols))
	r.lastRows.Store(uint32(rows))
	r.lastResizeAt.Store(time.Now().UnixNano())
	if f := r.ptmx.Load(); f != nil {
		if err := pty.Setsize(f, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
			slog.Debug("resize failed", "session", r.SessionName, "error", err)
		}
	}
}

// IsStopped returns true if the relay has been stopped.
func (r *Relay) IsStopped() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// GetLastActivity returns the time of the last broadcast (output activity).
func (r *Relay) GetLastActivity() time.Time {
	r.lastActivityMu.RLock()
	defer r.lastActivityMu.RUnlock()
	return r.lastActivity
}

// readLoop reads from the attach PTY, tracks alternate screen state, writes to
// the ring buffer, and broadcasts to viewers. It exits when the relay stops or a
// newer generation supersedes it.
func (r *Relay) readLoop(gen int64) {
	buf := make([]byte, 4096)
	for {
		f := r.ptmx.Load()
		if f == nil {
			return
		}
		n, err := f.Read(buf)
		if n > 0 {
			r.processOutput(buf[:n])
		}
		if err != nil {
			slog.Debug("relay read ended", "session", r.SessionName, "error", err)
			if r.IsStopped() || gen != r.generation.Load() {
				return
			}
			r.reconnect()
			return
		}
	}
}

// processOutput handles alternate screen tracking, ring buffer writes, and
// viewer broadcast for a chunk of output data.
func (r *Relay) processOutput(data []byte) {
	segments := r.trackAltScreen(data)

	r.broadcast(data)

	// Stamp activity only for real content, not resize redraws or input echoes.
	if len(segments) > 0 &&
		time.Since(time.Unix(0, r.lastResizeAt.Load())) > resizeActivityWindow &&
		time.Since(time.Unix(0, r.lastInputAt.Load())) > inputActivityWindow {
		r.lastActivityMu.Lock()
		r.lastActivity = time.Now()
		r.lastActivityMu.Unlock()
	}

	for _, seg := range segments {
		r.ringBuf.Write(seg)
	}
}

// trackAltScreen scans data for alternate screen sequences, toggles the
// inAltScreen flag, and returns the normal-mode segments (for the ring buffer).
// Viewers receive the raw stream directly, so no cleaned copy is built here.
func (r *Relay) trackAltScreen(data []byte) (normalSegments [][]byte) {
	if len(r.partial) > 0 {
		data = append(r.partial, data...)
		r.partial = nil
	}

	var currentNormal []byte
	if !r.inAltScreen.Load() {
		currentNormal = make([]byte, 0, len(data))
	}

	i := 0
	for i < len(data) {
		if data[i] == '\x1b' {
			remaining := data[i:]
			matched := false

			for _, seq := range allAltScreenSeqs {
				if len(remaining) < len(seq) {
					if bytes.HasPrefix(seq, remaining) {
						r.partial = make([]byte, len(remaining))
						copy(r.partial, remaining)
						if !r.inAltScreen.Load() && len(currentNormal) > 0 {
							normalSegments = append(normalSegments, currentNormal)
						}
						return normalSegments
					}
					continue
				}

				if bytes.Equal(remaining[:len(seq)], seq) {
					isEnter := false
					for _, enterSeq := range altScreenEnter {
						if bytes.Equal(seq, enterSeq) {
							isEnter = true
							break
						}
					}

					if isEnter {
						if !r.inAltScreen.Load() && len(currentNormal) > 0 {
							normalSegments = append(normalSegments, currentNormal)
							currentNormal = nil
						}
						r.inAltScreen.Store(true)
					} else {
						r.inAltScreen.Store(false)
						currentNormal = make([]byte, 0, len(data)-i)
					}

					i += len(seq)
					matched = true
					break
				}
			}

			if matched {
				continue
			}
		}

		if !r.inAltScreen.Load() {
			currentNormal = append(currentNormal, data[i])
		}
		i++
	}

	if !r.inAltScreen.Load() && len(currentNormal) > 0 {
		normalSegments = append(normalSegments, currentNormal)
	}

	return normalSegments
}

// broadcast sends data to all connected, non-suspended WebSocket viewers.
// Writes carry a deadline; a viewer whose write fails (stalled/dead client) is
// evicted and closed so it can't wedge future broadcasts. Its own read loop in
// handleWebSocket then errors out and finishes the cleanup.
func (r *Relay) broadcast(data []byte) {
	var failed []*websocket.Conn

	r.mu.RLock()
	for conn, v := range r.viewers {
		if v.suspended.Load() {
			continue
		}
		v.writeMu.Lock()
		err := writeMessage(conn, websocket.BinaryMessage, data)
		v.writeMu.Unlock()
		if err != nil {
			slog.Debug("viewer write error, evicting", "session", r.SessionName, "error", err)
			failed = append(failed, conn)
		}
	}
	r.mu.RUnlock()

	for _, conn := range failed {
		r.RemoveViewer(conn)
		_ = conn.Close()
	}
}

// reconnect re-establishes the attach PTY after a drop, swapping it under
// attachMu and starting a fresh read loop. If the session master is gone, it
// stops the relay.
func (r *Relay) reconnect() {
	r.attachMu.Lock()
	defer r.attachMu.Unlock()

	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		select {
		case <-r.done:
			return
		default:
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

		if r.IsStopped() {
			f.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return
		}

		if old := r.ptmx.Swap(f); old != nil {
			old.Close()
		}
		r.attachCmd.Store(cmd)

		slog.Info("relay reconnected", "session", r.SessionName)

		gen := r.generation.Add(1)
		go r.readLoop(gen)
		return
	}

	slog.Error(fmt.Sprintf("relay reconnect failed after %d attempts", maxReconnectAttempts), "session", r.SessionName)
	r.Stop()
}
