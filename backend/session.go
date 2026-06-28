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
	SessionID      string    `json:"-"` // claude conversation uuid (for name lookup)
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
	index        *SessionIndex // persisted uuid → {cwd,created,name}
	broker       *Broker
	stopPoll     chan struct{}
}

// NewSessionManager creates a SessionManager wired to the given SSE broker and
// starts the background polling goroutine.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		relays:   make(map[string]*Relay),
		index:    loadSessionIndex(),
		broker:   broker,
		stopPoll: make(chan struct{}),
	}
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

// SetSessionName stores a custom name on the live session's conversation (keyed
// by its uuid in the persistent index), so it shows in the sidebar and resume list.
func (sm *SessionManager) SetSessionName(sessionName, displayName string) {
	uuid := readSessionMeta(sessionName).SessionID
	if uuid == "" {
		return
	}
	sm.index.setName(uuid, displayName)
}

// GetSessionName returns the custom name for a live session via its conversation uuid.
func (sm *SessionManager) GetSessionName(sessionName string) string {
	return sm.index.name(readSessionMeta(sessionName).SessionID)
}

// History returns the previous RESUMABLE sessions for a folder (newest first).
// Sessions that were spawned but never messaged have no claude transcript and
// would exit immediately on resume, so they are filtered out.
func (sm *SessionManager) History(cwd string) []SessionHistoryEntry {
	out := []SessionHistoryEntry{}
	for _, e := range sm.index.listByCwd(cwd) {
		if hasTranscript(e.UUID) {
			out = append(out, e)
		}
	}
	return out
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

	for name, relay := range sm.relays {
		if !currentNames[name] || relay.IsStopped() {
			relay.Stop()
			delete(sm.relays, name)
			// Custom names live in the persistent index (keyed by conversation
			// uuid) and intentionally survive a session ending — they drive the
			// resume list.
		}
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
		if customName := sm.index.name(sessions[i].SessionID); customName != "" {
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

// Spawn creates a new Claude Code conversation in cwd. It generates a uuid and
// passes it to claude via --session-id so the dashboard owns the conversation id.
func (sm *SessionManager) Spawn(cwd string) (string, error) {
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	uuid := newUUID()
	name, err := sm.spawnDtach(absPath, uuid, "--session-id "+uuid)
	if err != nil {
		return "", err
	}
	sm.index.add(uuid, absPath, time.Now().Unix())
	return name, nil
}

// Resume reopens a previously recorded conversation by uuid (only uuids in the
// index can be resumed, which gates the shell-interpolated value).
func (sm *SessionManager) Resume(uuid string) (string, error) {
	cwd, ok := sm.index.cwd(uuid)
	if !ok {
		return "", fmt.Errorf("unknown session: %s", uuid)
	}
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	return sm.spawnDtach(absPath, uuid, "--resume "+uuid)
}

// validWorkspaceDir resolves cwd and ensures it is an existing directory under /workspace.
func validWorkspaceDir(cwd string) (string, error) {
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
	return absPath, nil
}

// spawnDtach launches a dtach session running claude with the given flag
// (`--session-id <uuid>` or `--resume <uuid>`), records the uuid in the meta
// sidecar, starts the relay, and returns the dtach session name.
func (sm *SessionManager) spawnDtach(absPath, uuid, claudeFlag string) (string, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}

	for i := 0; i < maxSpawnRetries; i++ {
		sessionName := generateSessionName()
		if _, statErr := os.Stat(sockPath(sessionName)); statErr == nil {
			continue // name collision, retry
		}

		if err := writeSessionMeta(sessionName, absPath, uuid); err != nil {
			slog.Warn("failed to write session metadata", "session", sessionName, "error", err)
		}

		innerScript := fmt.Sprintf("echo $$ > %q; exec %q %s --dangerously-skip-permissions",
			pidPath(sessionName), claudePath, claudeFlag)
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

		slog.Info("spawned session", "session", sessionName, "cwd", absPath, "uuid", uuid)

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

// writeSessionMeta writes the per-session metadata sidecar.
func writeSessionMeta(name, cwd, sessionID string) error {
	m := sessionMeta{CWD: cwd, Created: time.Now().Unix(), SessionID: sessionID}
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
			SessionID: meta.SessionID,
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
