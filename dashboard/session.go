package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	// scrollbackCapacity is the number of bytes retained for terminal replay.
	scrollbackCapacity = 10000
	// workspaceRoot is the base directory users may spawn sessions in.
	workspaceRoot = "/workspace"
)

// ManagedSession represents a Claude Code session spawned by the dashboard
// with a PTY attached.
type ManagedSession struct {
	TerminalID string
	PID        int
	Cmd        *exec.Cmd
	PTY        *os.File
	CWD        string
	StartedAt  time.Time
	Scrollback *RingBuffer

	wsConn *websocket.Conn
	wsMu   sync.Mutex

	done chan struct{} // closed when process exits
}

// DetectedSession represents a Claude Code session discovered from the
// ~/.claude/sessions/ directory.
type DetectedSession struct {
	PID       int
	SessionID string
	CWD       string
	StartedAt time.Time
	Alive     bool
}

// DisplaySession is the merged view used by templates. It combines managed
// (dashboard-spawned) and detected (file-discovered) session information.
type DisplaySession struct {
	TerminalID string
	PID        int
	SessionID  string
	CWD        string
	DirName    string
	StartedAt  time.Time
	Duration   string
	Alive      bool
	Managed    bool
	External   bool
}

// SessionManager tracks all managed PTY sessions and discovers external
// sessions from ~/.claude/sessions/*.json files.
type SessionManager struct {
	mu      sync.RWMutex
	managed map[string]*ManagedSession
	broker  *Broker
}

// NewSessionManager creates a SessionManager wired to the given SSE broker.
func NewSessionManager(broker *Broker) *SessionManager {
	return &SessionManager{
		managed: make(map[string]*ManagedSession),
		broker:  broker,
	}
}

// sessionFileData matches the JSON structure written by Claude Code.
type sessionFileData struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"` // milliseconds since epoch
}

// ListSessions merges managed sessions with those detected from disk and
// returns a unified list sorted by StartedAt descending (newest first).
func (sm *SessionManager) ListSessions() []DisplaySession {
	sm.mu.RLock()
	managedCopy := make(map[string]*ManagedSession, len(sm.managed))
	for k, v := range sm.managed {
		managedCopy[k] = v
	}
	sm.mu.RUnlock()

	// Build a set of managed PIDs for dedup.
	managedPIDs := make(map[int]string) // pid → terminalId
	for tid, ms := range managedCopy {
		managedPIDs[ms.PID] = tid
	}

	// Read detected sessions from disk.
	detected := discoverSessions()

	// Merge: detected sessions that match a managed PID get merged.
	detectedByPID := make(map[int]*DetectedSession, len(detected))
	for i := range detected {
		detectedByPID[detected[i].PID] = &detected[i]
	}

	var sessions []DisplaySession
	now := time.Now()

	// Add all managed sessions.
	for _, ms := range managedCopy {
		ds := DisplaySession{
			TerminalID: ms.TerminalID,
			PID:        ms.PID,
			CWD:        ms.CWD,
			DirName:    filepath.Base(ms.CWD),
			StartedAt:  ms.StartedAt,
			Duration:   humanDuration(now.Sub(ms.StartedAt)),
			Alive:      isProcessAlive(ms.PID),
			Managed:    true,
			External:   false,
		}
		// Merge sessionId from detected file if available.
		if det, ok := detectedByPID[ms.PID]; ok {
			ds.SessionID = det.SessionID
		}
		sessions = append(sessions, ds)
	}

	// Add detected sessions that are NOT managed.
	for _, det := range detected {
		if _, managed := managedPIDs[det.PID]; managed {
			continue // already merged above
		}
		sessions = append(sessions, DisplaySession{
			PID:       det.PID,
			SessionID: det.SessionID,
			CWD:       det.CWD,
			DirName:   filepath.Base(det.CWD),
			StartedAt: det.StartedAt,
			Duration:  humanDuration(now.Sub(det.StartedAt)),
			Alive:     det.Alive,
			Managed:   false,
			External:  true,
		})
	}

	// Sort by StartedAt descending.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})

	return sessions
}

// Spawn creates a new Claude Code session in the given working directory.
// It allocates a PTY, starts the process, and begins reading output into
// the scrollback ring buffer. Returns the new managed session.
func (sm *SessionManager) Spawn(cwd string) (*ManagedSession, error) {
	// Validate the working directory.
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(absPath, workspaceRoot) {
		return nil, fmt.Errorf("directory must be under %s", workspaceRoot)
	}
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("directory does not exist: %s", absPath)
	}

	// Find the claude binary.
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}

	terminalID := generateTerminalID()

	cmd := exec.Command(claudePath, "--dangerously-skip-permissions")
	cmd.Dir = absPath
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Start with a large initial size so Claude Code renders its banner
	// correctly before xterm.js connects and sends the real dimensions.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 120})
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	ms := &ManagedSession{
		TerminalID: terminalID,
		PID:        cmd.Process.Pid,
		Cmd:        cmd,
		PTY:        ptmx,
		CWD:        absPath,
		StartedAt:  time.Now(),
		Scrollback: NewRingBuffer(scrollbackCapacity),
		done:       make(chan struct{}),
	}

	sm.mu.Lock()
	sm.managed[terminalID] = ms
	sm.mu.Unlock()

	slog.Info("spawned session",
		"terminalId", terminalID,
		"pid", ms.PID,
		"cwd", absPath,
	)

	// Start the PTY output reader goroutine.
	go sm.readPTY(ms)

	// Start a goroutine that waits for the process to exit.
	go sm.waitProcess(ms)

	sm.broker.Publish()
	return ms, nil
}

// Kill terminates a managed session by terminal ID.
func (sm *SessionManager) Kill(terminalID string) error {
	sm.mu.Lock()
	ms, ok := sm.managed[terminalID]
	if !ok {
		sm.mu.Unlock()
		return fmt.Errorf("session not found: %s", terminalID)
	}
	delete(sm.managed, terminalID)
	sm.mu.Unlock()

	slog.Info("killing session", "terminalId", terminalID, "pid", ms.PID)

	// Send SIGTERM to the process.
	if ms.Cmd.Process != nil {
		_ = ms.Cmd.Process.Signal(syscall.SIGTERM)
	}

	// Close the PTY (this will also cause the read goroutine to exit).
	_ = ms.PTY.Close()

	// Close any attached WebSocket.
	ms.wsMu.Lock()
	if ms.wsConn != nil {
		_ = ms.wsConn.Close()
		ms.wsConn = nil
	}
	ms.wsMu.Unlock()

	sm.broker.Publish()
	return nil
}

// Get returns a managed session by terminal ID, or nil if not found.
func (sm *SessionManager) Get(terminalID string) *ManagedSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.managed[terminalID]
}

// Shutdown terminates all managed sessions. Called during server shutdown.
func (sm *SessionManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for id, ms := range sm.managed {
		slog.Info("shutting down session", "terminalId", id, "pid", ms.PID)
		if ms.Cmd.Process != nil {
			_ = ms.Cmd.Process.Signal(syscall.SIGTERM)
		}
		_ = ms.PTY.Close()
		ms.wsMu.Lock()
		if ms.wsConn != nil {
			_ = ms.wsConn.Close()
			ms.wsConn = nil
		}
		ms.wsMu.Unlock()
	}
	sm.managed = make(map[string]*ManagedSession)
}

// readPTY continuously reads from the PTY and writes to the scrollback buffer
// and to any attached WebSocket. This goroutine runs for the lifetime of the
// PTY (even if no WebSocket is attached).
func (sm *SessionManager) readPTY(ms *ManagedSession) {
	buf := make([]byte, 4096)
	for {
		n, err := ms.PTY.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Always write to scrollback.
			ms.Scrollback.Write(data)

			// Forward to WebSocket if attached.
			ms.wsMu.Lock()
			if ms.wsConn != nil {
				writeErr := ms.wsConn.WriteMessage(websocket.BinaryMessage, data)
				if writeErr != nil {
					slog.Debug("websocket write error, detaching",
						"terminalId", ms.TerminalID,
						"error", writeErr,
					)
					_ = ms.wsConn.Close()
					ms.wsConn = nil
				}
			}
			ms.wsMu.Unlock()
		}
		if err != nil {
			slog.Debug("pty read ended", "terminalId", ms.TerminalID, "error", err)
			return
		}
	}
}

// waitProcess waits for the process to exit, then closes the done channel
// and notifies subscribers.
func (sm *SessionManager) waitProcess(ms *ManagedSession) {
	_ = ms.Cmd.Wait()
	close(ms.done)
	slog.Info("session process exited", "terminalId", ms.TerminalID, "pid", ms.PID)

	// Close the WebSocket with a close frame if still attached.
	ms.wsMu.Lock()
	if ms.wsConn != nil {
		closeMsg := websocket.FormatCloseMessage(
			websocket.CloseNormalClosure,
			"process exited",
		)
		_ = ms.wsConn.WriteMessage(websocket.CloseMessage, closeMsg)
		_ = ms.wsConn.Close()
		ms.wsConn = nil
	}
	ms.wsMu.Unlock()

	// Remove from managed map.
	sm.mu.Lock()
	delete(sm.managed, ms.TerminalID)
	sm.mu.Unlock()

	sm.broker.Publish()
}

// discoverSessions reads all session files from ~/.claude/sessions/.
func discoverSessions() []DetectedSession {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("cannot determine home directory", "error", err)
		return nil
	}

	sessDir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		slog.Warn("failed to read sessions directory", "path", sessDir, "error", err)
		return nil
	}

	var sessions []DetectedSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			slog.Warn("failed to read session file", "name", entry.Name(), "error", err)
			continue
		}

		var sf sessionFileData
		if err := json.Unmarshal(data, &sf); err != nil {
			slog.Warn("malformed session file", "name", entry.Name(), "error", err)
			continue
		}

		sessions = append(sessions, DetectedSession{
			PID:       sf.PID,
			SessionID: sf.SessionID,
			CWD:       sf.CWD,
			StartedAt: time.UnixMilli(sf.StartedAt),
			Alive:     isProcessAlive(sf.PID),
		})
	}

	return sessions
}

// isProcessAlive checks whether a process with the given PID exists.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// generateTerminalID creates a random 16-character hex terminal identifier.
func generateTerminalID() string {
	buf := make([]byte, 8)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// humanDuration formats a duration into a human-readable string like "2h 15m"
// or "45s" for short durations.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm %ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}
