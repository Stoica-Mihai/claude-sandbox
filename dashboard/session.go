package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// sessionPrefix identifies dashboard-relevant tmux sessions.
	sessionPrefix = "claude-"
	// workspaceRoot is the base directory users may spawn sessions in.
	workspaceRoot = "/workspace"
	// cacheTTL is how long tmux list-sessions results are cached.
	cacheTTL = 2 * time.Second
	// pollInterval is how often the background poller checks for session changes.
	pollInterval = 5 * time.Second
	// maxSpawnRetries is the number of retries on tmux session name collision.
	maxSpawnRetries = 3
)

// DisplaySession is the view used by templates. All sessions are tmux sessions.
type DisplaySession struct {
	Name      string // tmux session name (e.g. "claude-a1b2c3d4")
	CWD       string
	DirName   string
	CreatedAt time.Time
	Duration  string
	Alive     bool
}

// SessionManager discovers tmux sessions and provides spawn/kill operations.
type SessionManager struct {
	mu        sync.RWMutex
	cached    []DisplaySession
	cachedAt  time.Time
	cachedRaw string // raw tmux output for change detection
	broker    *Broker
	stopPoll  chan struct{}
}

// NewSessionManager creates a SessionManager wired to the given SSE broker
// and starts the background polling goroutine.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		broker:   broker,
		stopPoll: make(chan struct{}),
	}
	go sm.pollLoop()
	return sm
}

// ListSessions returns all claude-prefixed tmux sessions, using cache if fresh.
func (sm *SessionManager) ListSessions() []DisplaySession {
	sm.mu.RLock()
	if time.Since(sm.cachedAt) < cacheTTL {
		result := make([]DisplaySession, len(sm.cached))
		copy(result, sm.cached)
		sm.mu.RUnlock()
		return result
	}
	sm.mu.RUnlock()

	return sm.refreshSessions()
}

// refreshSessions queries tmux and updates the cache. Returns the new list.
func (sm *SessionManager) refreshSessions() []DisplaySession {
	sessions := discoverTmuxSessions()

	sm.mu.Lock()
	sm.cached = sessions
	sm.cachedAt = time.Now()
	sm.mu.Unlock()

	return sessions
}

// invalidateCache forces the next ListSessions call to query tmux.
func (sm *SessionManager) invalidateCache() {
	sm.mu.Lock()
	sm.cachedAt = time.Time{}
	sm.mu.Unlock()
}

// Spawn creates a new Claude Code session inside a tmux session.
func (sm *SessionManager) Spawn(cwd string) (string, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(absPath, workspaceRoot) {
		return "", fmt.Errorf("directory must be under %s", workspaceRoot)
	}
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("directory does not exist: %s", absPath)
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}

	var sessionName string
	for i := 0; i < maxSpawnRetries; i++ {
		sessionName = generateSessionName()
		cmd := exec.Command("tmux", "new-session",
			"-d",
			"-s", sessionName,
			"-c", absPath,
			"--", claudePath, "--dangerously-skip-permissions",
		)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")

		if err := cmd.Run(); err != nil {
			slog.Warn("tmux new-session failed, retrying",
				"session", sessionName,
				"attempt", i+1,
				"error", err,
			)
			continue
		}

		slog.Info("spawned tmux session",
			"session", sessionName,
			"cwd", absPath,
		)
		sm.invalidateCache()
		sm.broker.Publish()
		return sessionName, nil
	}

	return "", fmt.Errorf("failed to create tmux session after %d attempts", maxSpawnRetries)
}

// Kill terminates a tmux session by name.
func (sm *SessionManager) Kill(sessionName string) error {
	if !sm.sessionExists(sessionName) {
		return fmt.Errorf("session not found: %s", sessionName)
	}

	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to kill session %s: %w", sessionName, err)
	}

	slog.Info("killed tmux session", "session", sessionName)
	sm.invalidateCache()
	sm.broker.Publish()
	return nil
}

// sessionExists checks if a tmux session with the given name exists.
func (sm *SessionManager) sessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// Shutdown stops the polling goroutine. tmux sessions are NOT killed —
// they persist for reconnection after dashboard restart.
func (sm *SessionManager) Shutdown() {
	close(sm.stopPoll)
	slog.Info("session manager shut down (tmux sessions preserved)")
}

// pollLoop periodically checks tmux for session changes and publishes SSE
// events when the session list changes.
func (sm *SessionManager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			raw := rawTmuxOutput()

			sm.mu.RLock()
			changed := raw != sm.cachedRaw
			sm.mu.RUnlock()

			if changed {
				sessions := parseTmuxOutput(raw)

				sm.mu.Lock()
				sm.cached = sessions
				sm.cachedAt = time.Now()
				sm.cachedRaw = raw
				sm.mu.Unlock()

				sm.broker.Publish()
			}
		case <-sm.stopPoll:
			return
		}
	}
}

// discoverTmuxSessions queries tmux and returns all claude-prefixed sessions.
func discoverTmuxSessions() []DisplaySession {
	raw := rawTmuxOutput()
	return parseTmuxOutput(raw)
}

// rawTmuxOutput runs tmux list-sessions and returns the raw stdout string.
func rawTmuxOutput() string {
	cmd := exec.Command("tmux", "list-sessions",
		"-F", "#{session_name}|#{session_created}|#{pane_current_path}",
	)
	out, err := cmd.Output()
	if err != nil {
		// tmux not running or no sessions — not an error.
		return ""
	}
	return string(out)
}

// parseTmuxOutput parses raw tmux list-sessions output into DisplaySessions.
func parseTmuxOutput(raw string) []DisplaySession {
	if raw == "" {
		return nil
	}

	now := time.Now()
	var sessions []DisplaySession

	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}

		name := parts[0]
		if !strings.HasPrefix(name, sessionPrefix) {
			continue
		}

		created, _ := strconv.ParseInt(parts[1], 10, 64)
		cwd := parts[2]
		createdAt := time.Unix(created, 0)

		sessions = append(sessions, DisplaySession{
			Name:      name,
			CWD:       cwd,
			DirName:   filepath.Base(cwd),
			CreatedAt: createdAt,
			Duration:  humanDuration(now.Sub(createdAt)),
			Alive:     true,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions
}

// generateSessionName creates a tmux session name like "claude-a1b2c3d4".
func generateSessionName() string {
	buf := make([]byte, 4)
	rand.Read(buf)
	return sessionPrefix + hex.EncodeToString(buf)
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
