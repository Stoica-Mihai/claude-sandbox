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
	"syscall"
	"time"
)

const (
	// sessionPrefix identifies dashboard-relevant sessions.
	sessionPrefix = "claude-"
	// workspaceRoot is the base directory users may spawn sessions in.
	workspaceRoot = "/workspace"
	// cacheTTL is how long discovery results are cached.
	cacheTTL = 2 * time.Second
	// pollInterval is how often the background poller checks for session changes.
	pollInterval = 5 * time.Second
	// maxSpawnRetries is the number of retries on session-name collision.
	maxSpawnRetries = 3
	// recentActivityThreshold is how recently a session must have had output to be "active".
	recentActivityThreshold = 5 * time.Second
	// termType is the TERM environment variable set for new sessions.
	termType = "xterm-256color"
	// killGracePeriod is how long Kill waits after SIGTERM before SIGKILL.
	killGracePeriod = 2 * time.Second
)

// sessionNamesPath is the persisted custom-display-names file (0600, not in /tmp).
func sessionNamesPath() string { return filepath.Join(metaDir, "session-names.json") }

// DisplaySession is the view used by API responses.
type DisplaySession struct {
	Name           string    `json:"name"`
	CWD            string    `json:"cwd"`
	DirName        string    `json:"dir_name"`
	CreatedAt      time.Time `json:"created_at"`
	Duration       string    `json:"duration"`
	Alive          bool      `json:"alive"`
	LastActivity   time.Time `json:"last_activity"`
	LastActiveStr  string    `json:"last_active_str,omitempty"`
	RecentActivity bool      `json:"recent_activity"`
	DisplayName    string    `json:"display_name"`
	Hue            int       `json:"hue"`
}

// sessionHues is a hand-picked palette of 12 maximally distinct hues.
var sessionHues = []int{0, 30, 60, 120, 170, 210, 260, 300, 330, 45, 150, 240}

// computeHue returns a deterministic hue from a fixed palette, derived from the
// session name's hex suffix.
func computeHue(name string) int {
	suffix := strings.TrimPrefix(name, sessionPrefix)
	var idx uint64
	if v, err := strconv.ParseUint(suffix, 16, 64); err == nil {
		idx = v
	} else {
		for _, b := range []byte(name) {
			idx += uint64(b)
		}
	}
	return sessionHues[int(idx%uint64(len(sessionHues)))]
}

// SessionManager discovers dtach sessions, manages relays, and provides
// spawn/kill operations.
type SessionManager struct {
	mu           sync.RWMutex
	cached       []DisplaySession
	cachedAt     time.Time
	cachedSig    string // discovery signature for change detection
	relays       map[string]*Relay
	sessionNames map[string]string // custom display names keyed by session name
	broker       *Broker
	stopPoll     chan struct{}
}

// NewSessionManager creates a SessionManager wired to the given SSE broker and
// starts the background polling goroutine.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		relays:       make(map[string]*Relay),
		sessionNames: make(map[string]string),
		broker:       broker,
		stopPoll:     make(chan struct{}),
	}
	sm.loadSessionNames()
	sm.syncRelays(discoverSessions())
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
	data, err := os.ReadFile(sessionNamesPath())
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

// saveSessionNames writes current display names to disk (0600).
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
	if err := os.WriteFile(sessionNamesPath(), data, 0o600); err != nil {
		slog.Warn("failed to save session names", "error", err)
	}
}

// syncRelays ensures every live session has a running relay and relays for gone
// sessions are stopped. The caller supplies the discovered session list to avoid
// a redundant directory scan.
func (sm *SessionManager) syncRelays(sessions []DisplaySession) {
	currentNames := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		currentNames[s.Name] = true
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

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

// ListSessions returns all sessions, using cache if fresh. Enrichment (activity
// timestamps, display names) is applied on every call.
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

// enrichSessions adds live activity data and display names to a session list
// copy. Hue is set at discovery time (pure function of the immutable name).
func (sm *SessionManager) enrichSessions(sessions []DisplaySession) []DisplaySession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for i := range sessions {
		if relay := sm.relays[sessions[i].Name]; relay != nil {
			lastActivity := relay.GetLastActivity()
			sessions[i].LastActivity = lastActivity
			sessions[i].LastActiveStr = humanRelativeTime(lastActivity)
			sessions[i].RecentActivity = !lastActivity.IsZero() && time.Since(lastActivity) < recentActivityThreshold
		}
		if customName := sm.sessionNames[sessions[i].Name]; customName != "" {
			sessions[i].DisplayName = customName
		} else {
			sessions[i].DisplayName = sessions[i].DirName
		}
	}
	return sessions
}

// refreshSessions queries discovery and updates the cache. Returns a copy of the
// new list.
func (sm *SessionManager) refreshSessions() []DisplaySession {
	sessions := discoverSessions()
	sig := sessionsSignature(sessions)

	sm.mu.Lock()
	sm.cached = sessions
	sm.cachedAt = time.Now()
	sm.cachedSig = sig
	sm.mu.Unlock()

	result := make([]DisplaySession, len(sessions))
	copy(result, sessions)
	return result
}

// invalidateCache forces the next ListSessions call to re-discover.
func (sm *SessionManager) invalidateCache() {
	sm.mu.Lock()
	sm.cachedAt = time.Time{}
	sm.mu.Unlock()
}

// Spawn creates a new Claude Code session as a detached dtach master.
func (sm *SessionManager) Spawn(cwd string) (string, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if absPath != workspaceRoot && !strings.HasPrefix(absPath, workspaceRoot+"/") {
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

	for i := 0; i < maxSpawnRetries; i++ {
		sessionName := generateSessionName()
		if _, statErr := os.Stat(sockPath(sessionName)); statErr == nil {
			continue // name collision, retry
		}

		if err := writeSessionMeta(sessionName, absPath); err != nil {
			slog.Warn("failed to write session metadata", "session", sessionName, "error", err)
		}
		acceptFolderTrust(absPath)

		innerScript := fmt.Sprintf("echo $$ > %q; exec %q --dangerously-skip-permissions",
			pidPath(sessionName), claudePath)
		cmd := exec.Command("dtach", "-n", sockPath(sessionName), "-E", "-z",
			"bash", "-c", innerScript)
		cmd.Dir = absPath
		cmd.Env = append(os.Environ(), "TERM="+termType)

		if err := cmd.Run(); err != nil {
			slog.Warn("dtach spawn failed, retrying",
				"session", sessionName, "attempt", i+1, "error", err)
			removeSessionFiles(sessionName)
			continue
		}

		slog.Info("spawned session", "session", sessionName, "cwd", absPath)

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

	return "", fmt.Errorf("failed to create session after %d attempts", maxSpawnRetries)
}

// Kill terminates a session by signalling its process group via the PID sidecar.
func (sm *SessionManager) Kill(sessionName string) error {
	if !sessionAlive(sessionName) {
		return fmt.Errorf("session not found: %s", sessionName)
	}

	if pid := sessionPID(sessionName); pid > 0 {
		// Signal the process group (negative pid); the inner bash is the session
		// leader, so this reaps claude and any children it spawned.
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		deadline := time.Now().Add(killGracePeriod)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}

	slog.Info("killed session", "session", sessionName)

	sm.mu.Lock()
	if relay, ok := sm.relays[sessionName]; ok {
		relay.Stop()
		delete(sm.relays, sessionName)
	}
	delete(sm.sessionNames, sessionName)
	sm.mu.Unlock()

	removeSessionFiles(sessionName)
	sm.invalidateCache()
	sm.broker.Publish()
	return nil
}

// Shutdown stops the polling goroutine. Sessions are NOT killed — they persist
// for reconnection after dashboard restart.
func (sm *SessionManager) Shutdown() {
	close(sm.stopPoll)
	slog.Info("session manager shut down (sessions preserved)")
}

// pollLoop periodically re-discovers sessions and publishes SSE events on change.
func (sm *SessionManager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sessions := discoverSessions()
			sig := sessionsSignature(sessions)

			sm.mu.RLock()
			changed := sig != sm.cachedSig
			sm.mu.RUnlock()

			if changed {
				sm.mu.Lock()
				sm.cached = sessions
				sm.cachedAt = time.Now()
				sm.cachedSig = sig
				sm.mu.Unlock()

				sm.syncRelays(sessions)
			}

			sm.broker.Publish()
		case <-sm.stopPoll:
			return
		}
	}
}

var trustMu sync.Mutex

// acceptFolderTrust pre-accepts Claude Code's folder-trust dialog for cwd by
// setting projects[cwd].hasTrustDialogAccepted in the scoped .claude.json.
// Dashboard-spawned sessions are always the user's own /workspace directories,
// so the interactive trust prompt is redundant. No-op if already trusted.
func acceptFolderTrust(cwd string) {
	trustMu.Lock()
	defer trustMu.Unlock()

	path := filepath.Join(claudeConfigDir(), ".claude.json")
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	projects, ok := cfg["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
		cfg["projects"] = projects
	}
	entry, ok := projects[cwd].(map[string]any)
	if !ok {
		entry = map[string]any{}
		projects[cwd] = entry
	}
	if t, _ := entry["hasTrustDialogAccepted"].(bool); t {
		return // already trusted — avoid rewriting (and clobbering concurrent writes)
	}
	entry["hasTrustDialogAccepted"] = true

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal .claude.json for trust", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("failed to write trust temp file", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("failed to commit folder trust", "error", err)
	}
}

// writeSessionMeta writes the per-session metadata sidecar.
func writeSessionMeta(name, cwd string) error {
	m := sessionMeta{CWD: cwd, Created: time.Now().Unix()}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(name), data, 0o600)
}

// readSessionMeta reads the per-session metadata sidecar (zero value if absent).
func readSessionMeta(name string) sessionMeta {
	var m sessionMeta
	data, err := os.ReadFile(metaPath(name))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

// discoverSessions scans the PID sidecars for live claude-* sessions, unlinking
// the sidecars of any whose process is gone. Discovery keys off the PID sidecar
// (which the backend owns) rather than the socket: dtach removes its own socket
// when the inner process exits, so a socket scan would miss dead sessions and
// leak their metadata sidecars.
func discoverSessions() []DisplaySession {
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return nil
	}

	now := time.Now()
	var sessions []DisplaySession

	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, sessionPrefix) || !strings.HasSuffix(n, ".pid") {
			continue
		}
		name := strings.TrimSuffix(n, ".pid")
		if !sessionAlive(name) {
			removeSessionFiles(name)
			continue
		}

		meta := readSessionMeta(name)
		createdAt := meta.createdTime(name)

		sessions = append(sessions, DisplaySession{
			Name:      name,
			CWD:       meta.CWD,
			DirName:   filepath.Base(meta.CWD),
			CreatedAt: createdAt,
			Duration:  humanDuration(now.Sub(createdAt)),
			Alive:     true,
			Hue:       computeHue(name),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions
}

// sessionsSignature builds a deterministic change-detection signature.
func sessionsSignature(sessions []DisplaySession) string {
	var b strings.Builder
	for _, s := range sessions {
		b.WriteString(s.Name)
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(s.CreatedAt.Unix(), 10))
		b.WriteByte('\n')
	}
	return b.String()
}

// generateSessionName creates a session name like "claude-a1b2c3d4".
func generateSessionName() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return sessionPrefix + hex.EncodeToString(buf)
}

// humanRelativeTime formats a time as a relative string like "3s ago".
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

// humanDuration formats a duration like "2h 15m" or "45s".
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
