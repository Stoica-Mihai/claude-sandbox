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
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionNamesFile = "/tmp/claude-session-names.json"

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
	Name           string // tmux session name (e.g. "claude-a1b2c3d4")
	CWD            string
	DirName        string
	CreatedAt      time.Time
	Duration       string
	Alive          bool
	LastActivity   time.Time
	LastActiveStr  string
	RecentActivity bool
	DisplayName    string
}

// sessionHues is a hand-picked palette of 12 maximally distinct hues.
var sessionHues = []int{0, 30, 60, 120, 170, 210, 260, 300, 330, 45, 150, 240}

// Hue returns a deterministic hue from a fixed palette of visually distinct
// colors, derived from the session name's hex suffix.
func (s DisplaySession) Hue() int {
	// Parse the hex suffix after "claude-" as an integer for a clean index.
	suffix := strings.TrimPrefix(s.Name, sessionPrefix)
	var idx uint64
	if v, err := strconv.ParseUint(suffix, 16, 64); err == nil {
		idx = v
	} else {
		// Fallback: sum bytes.
		for _, b := range []byte(s.Name) {
			idx += uint64(b)
		}
	}
	return sessionHues[int(idx%uint64(len(sessionHues)))]
}

// SessionManager discovers tmux sessions, manages relays, and provides
// spawn/kill operations.
type SessionManager struct {
	mu           sync.RWMutex
	cached       []DisplaySession
	cachedAt     time.Time
	cachedRaw    string // raw tmux output for change detection
	relays       map[string]*Relay
	sessionNames map[string]string // custom display names keyed by session name
	broker       *Broker
	stopPoll     chan struct{}
}

// NewSessionManager creates a SessionManager wired to the given SSE broker
// and starts the background polling goroutine.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		relays:       make(map[string]*Relay),
		sessionNames: make(map[string]string),
		broker:       broker,
		stopPoll:     make(chan struct{}),
	}
	sm.loadSessionNames()
	// Start relays for any existing sessions.
	sm.syncRelays()
	go sm.pollLoop()
	return sm
}

// GetRelay returns the relay for a session, or nil if not found.
func (sm *SessionManager) GetRelay(sessionName string) *Relay {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.relays[sessionName]
}

// SetSessionName sets or clears a custom display name for a session.
func (sm *SessionManager) SetSessionName(sessionName, displayName string) {
	sm.mu.Lock()
	if displayName == "" {
		delete(sm.sessionNames, sessionName)
	} else {
		sm.sessionNames[sessionName] = displayName
	}
	sm.mu.Unlock()
	sm.saveSessionNames()
}

// GetSessionName returns the custom display name for a session, or "" if unset.
func (sm *SessionManager) GetSessionName(sessionName string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionNames[sessionName]
}

// loadSessionNames reads persisted display names from disk.
func (sm *SessionManager) loadSessionNames() {
	data, err := os.ReadFile(sessionNamesFile)
	if err != nil {
		return // file doesn't exist yet — normal on first run
	}
	var names map[string]string
	if err := json.Unmarshal(data, &names); err != nil {
		slog.Warn("failed to parse session names file", "error", err)
		return
	}
	sm.mu.Lock()
	for k, v := range names {
		sm.sessionNames[k] = v
	}
	sm.mu.Unlock()
	slog.Info("loaded session names", "count", len(names))
}

// saveSessionNames writes current display names to disk.
func (sm *SessionManager) saveSessionNames() {
	sm.mu.RLock()
	names := make(map[string]string, len(sm.sessionNames))
	for k, v := range sm.sessionNames {
		names[k] = v
	}
	sm.mu.RUnlock()

	data, err := json.Marshal(names)
	if err != nil {
		slog.Warn("failed to marshal session names", "error", err)
		return
	}
	if err := os.WriteFile(sessionNamesFile, data, 0644); err != nil {
		slog.Warn("failed to save session names", "error", err)
	}
}

// syncRelays ensures every discovered tmux session has a running relay,
// and relays for gone sessions are stopped.
func (sm *SessionManager) syncRelays() {
	sessions := discoverTmuxSessions()
	currentNames := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		currentNames[s.Name] = true
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Start relays for new sessions.
	for name := range currentNames {
		if _, exists := sm.relays[name]; !exists {
			relay := NewRelay(name)
			if err := relay.Start(); err != nil {
				slog.Warn("failed to start relay", "session", name, "error", err)
				continue
			}
			sm.relays[name] = relay
		}
	}

	// Stop relays for gone sessions.
	var namesChanged bool
	for name, relay := range sm.relays {
		if !currentNames[name] || relay.IsStopped() {
			relay.Stop()
			delete(sm.relays, name)
			if _, had := sm.sessionNames[name]; had {
				delete(sm.sessionNames, name)
				namesChanged = true
			}
		}
	}
	if namesChanged {
		go sm.saveSessionNames()
	}
}

// ListSessions returns all claude-prefixed tmux sessions, using cache if fresh.
// Enrichment (activity timestamps, display names) is applied on every call.
func (sm *SessionManager) ListSessions() []DisplaySession {
	sm.mu.RLock()
	if time.Since(sm.cachedAt) < cacheTTL {
		result := make([]DisplaySession, len(sm.cached))
		copy(result, sm.cached)
		sm.mu.RUnlock()
		return sm.enrichSessions(result)
	}
	sm.mu.RUnlock()

	return sm.enrichSessions(sm.refreshSessions())
}

// enrichSessions adds live activity data and display names to a session list copy.
func (sm *SessionManager) enrichSessions(sessions []DisplaySession) []DisplaySession {
	for i := range sessions {
		if relay := sm.GetRelay(sessions[i].Name); relay != nil {
			lastActivity := relay.GetLastActivity()
			sessions[i].LastActivity = lastActivity
			sessions[i].LastActiveStr = humanRelativeTime(lastActivity)
			sessions[i].RecentActivity = !lastActivity.IsZero() && time.Since(lastActivity) < 5*time.Second
		}
		if customName := sm.GetSessionName(sessions[i].Name); customName != "" {
			sessions[i].DisplayName = customName
		} else {
			sessions[i].DisplayName = sessions[i].DirName
		}
	}
	return sessions
}

// refreshSessions queries tmux and updates the cache. Returns a copy of the
// new list (safe to mutate without affecting the cache).
func (sm *SessionManager) refreshSessions() []DisplaySession {
	sessions := discoverTmuxSessions()

	sm.mu.Lock()
	sm.cached = sessions
	sm.cachedAt = time.Now()
	sm.mu.Unlock()

	result := make([]DisplaySession, len(sessions))
	copy(result, sessions)
	return result
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

		// Start relay for the new session.
		relay := NewRelay(sessionName)
		if err := relay.Start(); err != nil {
			slog.Warn("failed to start relay for new session", "session", sessionName, "error", err)
		} else {
			sm.mu.Lock()
			sm.relays[sessionName] = relay
			sm.mu.Unlock()
		}

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

	// Stop relay for the killed session.
	sm.mu.Lock()
	if relay, ok := sm.relays[sessionName]; ok {
		relay.Stop()
		delete(sm.relays, sessionName)
	}
	sm.mu.Unlock()

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

				// Sync relays for new/gone sessions.
				sm.syncRelays()
			}

			// Always publish so activity timestamps and pulse states refresh.
			sm.broker.Publish()
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

// humanRelativeTime formats a time as a relative string like "3s ago" or "5m ago".
func humanRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
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
