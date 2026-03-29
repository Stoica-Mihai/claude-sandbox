package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
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
	writeMu   sync.Mutex  // serializes all WebSocket writes to this connection
	size      viewerSize
	suspended atomic.Bool // when true, broadcast skips this viewer
}

// Relay manages bidirectional I/O between WebSocket viewers and a tmux
// session via pipe-pane + socat over a unix socket.
type Relay struct {
	SessionName string
	socketPath  string
	ringBuf     *RingBuffer

	mu          sync.RWMutex
	viewers     map[*websocket.Conn]*viewer
	lastResizer *websocket.Conn // viewer that last triggered a resize

	listener  net.Listener
	socatConn net.Conn

	lastActivity   time.Time
	lastActivityMu sync.RWMutex
	lastResizeAt time.Time // suppress activity stamping after resize redraws
	lastInputAt  time.Time // suppress activity stamping for keystroke echoes

	inAltScreen bool   // true when Claude Code is in alternate screen mode
	partial     []byte // partial escape sequence from previous read chunk

	done     chan struct{} // closed when relay is stopped
	stopOnce sync.Once
}

// NewRelay creates a relay for the given tmux session.
func NewRelay(sessionName string) *Relay {
	return &Relay{
		SessionName: sessionName,
		socketPath:  fmt.Sprintf("/tmp/relay-%s.sock", sessionName),
		ringBuf:     NewRingBuffer(defaultBufferCapacity),
		viewers:     make(map[*websocket.Conn]*viewer),
		done:        make(chan struct{}),
	}
}

// Start sets up the unix socket, starts pipe-pane with socat, and begins
// reading output into the ring buffer.
func (r *Relay) Start() error {
	// Clean up any stale socket file.
	os.Remove(r.socketPath)

	var err error
	r.listener, err = net.Listen("unix", r.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", r.socketPath, err)
	}

	// Start pipe-pane with socat in the background.
	if err := r.startPipePaneCmd(); err != nil {
		r.listener.Close()
		os.Remove(r.socketPath)
		return fmt.Errorf("failed to start pipe-pane: %w", err)
	}

	// Accept the socat connection (with timeout).
	r.listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Second))
	r.socatConn, err = r.listener.Accept()
	if err != nil {
		r.stopPipePaneCmd()
		r.listener.Close()
		os.Remove(r.socketPath)
		return fmt.Errorf("socat did not connect: %w", err)
	}
	// Clear deadline for normal operation.
	r.listener.(*net.UnixListener).SetDeadline(time.Time{})

	slog.Info("relay started", "session", r.SessionName)

	// Start the output reader goroutine.
	go r.readLoop()

	return nil
}

// Stop tears down the relay: stops pipe-pane, closes connections, removes socket.
func (r *Relay) Stop() {
	r.stopOnce.Do(func() {
		close(r.done)

		r.stopPipePaneCmd()

		if r.socatConn != nil {
			r.socatConn.Close()
		}
		if r.listener != nil {
			r.listener.Close()
		}
		os.Remove(r.socketPath)

		// Close all viewer WebSockets.
		r.mu.Lock()
		for conn, v := range r.viewers {
			v.writeMu.Lock()
			closeMsg := websocket.FormatCloseMessage(
				websocket.CloseNormalClosure,
				"session ended",
			)
			_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
			_ = conn.Close()
			v.writeMu.Unlock()
		}
		r.viewers = make(map[*websocket.Conn]*viewer)
		r.mu.Unlock()

		// Clean up uploaded files for this session.
		uploadPath := filepath.Join(uploadDir, r.SessionName)
		if err := os.RemoveAll(uploadPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to clean upload dir", "path", uploadPath, "error", err)
		}

		slog.Info("relay stopped", "session", r.SessionName)
	})
}

// AddViewer registers a WebSocket connection, replays the ring buffer,
// and adds it to the broadcast list.
func (r *Relay) AddViewer(conn *websocket.Conn) {
	// Send terminal reset — safe to write directly since the viewer
	// isn't in the map yet (broadcast can't reach it).
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("\x1bc")); err != nil {
		slog.Debug("failed to send reset to viewer", "session", r.SessionName, "error", err)
		return
	}

	// Replay ring buffer (normal-mode conversation history).
	scrollback := r.ringBuf.Bytes()
	if len(scrollback) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, scrollback); err != nil {
			slog.Debug("failed to replay scrollback", "session", r.SessionName, "error", err)
			return
		}
	}

	// If currently in alt screen (Claude Code TUI), tell the viewer to
	// enter alt screen so live TUI output renders in the correct buffer.
	if r.inAltScreen {
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\x1b[?1049h"))
	}

	// Add to viewers.
	r.mu.Lock()
	r.viewers[conn] = &viewer{}
	r.mu.Unlock()
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

// SendInput writes user input bytes to the tmux pane via the socat connection.
func (r *Relay) SendInput(data []byte) error {
	if r.socatConn == nil {
		return fmt.Errorf("socat not connected")
	}
	r.lastInputAt = time.Now()
	_, err := r.socatConn.Write(data)
	return err
}

// Resize stores a viewer's terminal dimensions and resizes tmux only if this
// viewer is the active one (last to type). It does NOT change who the active
// viewer is — only ResizeToViewer (input-triggered) does that.
func (r *Relay) Resize(conn *websocket.Conn, cols, rows uint16) {
	r.mu.Lock()
	if v, ok := r.viewers[conn]; ok {
		v.size = viewerSize{cols, rows}
	}
	// First viewer to connect becomes the active one.
	if r.lastResizer == nil {
		r.lastResizer = conn
	}
	isActive := r.lastResizer == conn
	r.mu.Unlock()

	if isActive {
		r.resizeTmux(cols, rows)
	}
}

// deactivatedMsg is sent to non-active viewers so their client knows
// the display is frozen and needs a clear on next input.
var deactivatedMsg = []byte(`{"type":"deactivated"}`)

// ResizeToViewer resizes the tmux window to match the given viewer's dimensions
// if that viewer is not already the last resizer. This mimics tmux's
// "window-size latest" behavior — the active typist's size wins.
// Non-active viewers are suspended (broadcast skips them) and receive a
// "deactivated" text message so the client clears on next input.
func (r *Relay) ResizeToViewer(conn *websocket.Conn) {
	r.mu.RLock()
	if r.lastResizer == conn {
		r.mu.RUnlock()
		return
	}
	v, ok := r.viewers[conn]
	r.mu.RUnlock()

	if !ok || v.size.cols == 0 || v.size.rows == 0 {
		return
	}

	r.mu.Lock()
	r.lastResizer = conn
	r.mu.Unlock()

	// Suspend non-active viewers and notify them. Suspended viewers
	// don't receive broadcast data, so their display freezes at the
	// last correct state instead of showing garbled content.
	r.mu.RLock()
	for c, vw := range r.viewers {
		if c == conn {
			continue
		}
		vw.suspended.Store(true)
		vw.writeMu.Lock()
		_ = c.WriteMessage(websocket.TextMessage, deactivatedMsg)
		vw.writeMu.Unlock()
	}
	r.mu.RUnlock()

	r.resizeTmux(v.size.cols, v.size.rows)
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

// resizeTmux runs the tmux resize-window command.
func (r *Relay) resizeTmux(cols, rows uint16) {
	r.lastResizeAt = time.Now()
	cmd := exec.Command("tmux", "resize-window",
		"-t", r.SessionName,
		"-x", fmt.Sprintf("%d", cols),
		"-y", fmt.Sprintf("%d", rows),
	)
	if err := cmd.Run(); err != nil {
		slog.Debug("resize failed", "session", r.SessionName, "error", err)
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

// readLoop continuously reads from the socat connection, processes alternate
// screen tracking, writes to ring buffer, and broadcasts to viewers.
func (r *Relay) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := r.socatConn.Read(buf)
		if n > 0 {
			r.processOutput(buf[:n])
		}
		if err != nil {
			slog.Debug("relay read ended", "session", r.SessionName, "error", err)
			// Attempt reconnect if relay hasn't been stopped.
			if !r.IsStopped() {
				r.reconnect()
			}
			return
		}
	}
}

// processOutput handles alternate screen tracking, ring buffer writes,
// and viewer broadcast for a chunk of output data.
func (r *Relay) processOutput(data []byte) {
	// Track alt screen state and extract normal-mode segments for ring buffer.
	_, segments := r.trackAltScreen(data)

	// Broadcast raw data to viewers (including alt-screen sequences so
	// xterm.js handles alternate screen properly — no viewport jumps).
	r.broadcast(data)

	// Stamp activity only when there's real content (normal-mode segments),
	// not cursor blinks, and not resize-triggered redraws (which are just
	// tmux re-rendering existing content, not new output).
	if len(segments) > 0 && time.Since(r.lastResizeAt) > 2*time.Second && time.Since(r.lastInputAt) > 500*time.Millisecond {
		r.lastActivityMu.Lock()
		r.lastActivity = time.Now()
		r.lastActivityMu.Unlock()
	}

	// Write only normal-mode segments to the ring buffer.
	for _, seg := range segments {
		r.ringBuf.Write(seg)
	}
}

// trackAltScreen scans data for alternate screen sequences, strips them,
// toggles the inAltScreen flag, and returns:
//   - cleaned: the data with alt screen sequences removed (for viewers)
//   - normalSegments: slices of data that were in normal screen mode (for ring buffer)
func (r *Relay) trackAltScreen(data []byte) (cleaned []byte, normalSegments [][]byte) {
	// Prepend any partial sequence from previous chunk.
	if len(r.partial) > 0 {
		data = append(r.partial, data...)
		r.partial = nil
	}

	cleaned = make([]byte, 0, len(data))
	var currentNormal []byte
	if !r.inAltScreen {
		currentNormal = make([]byte, 0, len(data))
	}

	i := 0
	for i < len(data) {
		// Check if we're at the start of an escape sequence.
		if data[i] == '\x1b' {
			// Check for partial match at end of buffer.
			remaining := data[i:]
			matched := false

			for _, seq := range allAltScreenSeqs {
				if len(remaining) < len(seq) {
					// Could be a partial match — check prefix.
					if bytes.HasPrefix(seq, remaining) {
						r.partial = make([]byte, len(remaining))
						copy(r.partial, remaining)
						// Flush current normal segment.
						if !r.inAltScreen && len(currentNormal) > 0 {
							normalSegments = append(normalSegments, currentNormal)
						}
						return cleaned, normalSegments
					}
					continue
				}

				if bytes.Equal(remaining[:len(seq)], seq) {
					// Found a full match — strip it and toggle state.
					isEnter := false
					for _, enterSeq := range altScreenEnter {
						if bytes.Equal(seq, enterSeq) {
							isEnter = true
							break
						}
					}

					if isEnter {
						// Flush normal segment before entering alt screen.
						if !r.inAltScreen && len(currentNormal) > 0 {
							normalSegments = append(normalSegments, currentNormal)
							currentNormal = nil
						}
						r.inAltScreen = true
					} else {
						r.inAltScreen = false
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

		// Regular byte — add to cleaned output and conditionally to normal buffer.
		cleaned = append(cleaned, data[i])
		if !r.inAltScreen {
			currentNormal = append(currentNormal, data[i])
		}
		i++
	}

	// Flush remaining normal segment.
	if !r.inAltScreen && len(currentNormal) > 0 {
		normalSegments = append(normalSegments, currentNormal)
	}

	return cleaned, normalSegments
}

// broadcast sends data to all connected, non-suspended WebSocket viewers.
func (r *Relay) broadcast(data []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for conn, v := range r.viewers {
		if v.suspended.Load() {
			continue
		}
		v.writeMu.Lock()
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			slog.Debug("viewer write error", "session", r.SessionName, "error", err)
		}
		v.writeMu.Unlock()
	}
}

// startPipePaneCmd runs tmux pipe-pane to connect the pane's I/O to socat.
func (r *Relay) startPipePaneCmd() error {
	cmd := exec.Command("tmux", "pipe-pane", "-IO", "-t", r.SessionName,
		fmt.Sprintf("socat - UNIX-CONNECT:%s", r.socketPath),
	)
	return cmd.Run()
}

// stopPipePaneCmd disconnects pipe-pane from the session.
func (r *Relay) stopPipePaneCmd() {
	cmd := exec.Command("tmux", "pipe-pane", "-t", r.SessionName)
	_ = cmd.Run()
}

// reconnect attempts to re-establish the socat connection after a drop.
func (r *Relay) reconnect() {
	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-r.done:
			return
		default:
		}

		slog.Info("relay reconnecting", "session", r.SessionName, "attempt", attempt)
		time.Sleep(time.Duration(attempt) * time.Second)

		// Check if the session still exists.
		check := exec.Command("tmux", "has-session", "-t", r.SessionName)
		if check.Run() != nil {
			slog.Info("session gone, stopping relay", "session", r.SessionName)
			r.Stop()
			return
		}

		// Clean up old connection.
		if r.socatConn != nil {
			r.socatConn.Close()
		}

		// Restart pipe-pane.
		if err := r.startPipePaneCmd(); err != nil {
			slog.Warn("reconnect pipe-pane failed", "session", r.SessionName, "error", err)
			continue
		}

		// Accept new socat connection.
		r.listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Second))
		conn, err := r.listener.Accept()
		if err != nil {
			slog.Warn("reconnect accept failed", "session", r.SessionName, "error", err)
			continue
		}
		r.listener.(*net.UnixListener).SetDeadline(time.Time{})
		r.socatConn = conn

		slog.Info("relay reconnected", "session", r.SessionName)

		// Restart read loop.
		go r.readLoop()
		return
	}

	slog.Error("relay reconnect failed after 3 attempts", "session", r.SessionName)
	r.Stop()
}
