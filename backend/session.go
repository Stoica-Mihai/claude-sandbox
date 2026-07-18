package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	api "claude-sandbox-api"
)

// ErrUnknownSession is returned when a uuid is not present in the session index.
// Handlers map it to 404 via errors.Is rather than string matching.
var ErrUnknownSession = errors.New("unknown session")

const (
	// sessionPrefix identifies dashboard-relevant sessions.
	sessionPrefix = "claude-"
	// workspaceRoot is the base directory users may spawn sessions in.
	workspaceRoot = "/workspace"
	// pollInterval is how often the fallback poller verifies store liveness.
	pollInterval = 5 * time.Second
	// maxSpawnRetries is the number of retries on session-name collision.
	maxSpawnRetries = 3
	// termType is the TERM environment variable set for new sessions.
	termType = "xterm-256color"
	// killGracePeriod is how long Kill waits after SIGTERM before SIGKILL.
	killGracePeriod = 2 * time.Second
	// pidWaitTimeout bounds how long Spawn waits for the inner bash to write
	// the PID sidecar, so the new record carries a real PID.
	pidWaitTimeout = 1 * time.Second
)

// SessionManager owns the authoritative in-memory session store and the relay
// registry; it provides spawn/kill/resume. State changes are event-driven
// (spawn, kill, relay exit); the sidecar files are read once at boot.
type SessionManager struct {
	relays   *relayRegistry
	store    *sessionStore
	index    *SessionIndex // persisted uuid → {cwd,created,name}
	broker   *Broker
	stopPoll chan struct{}
}

// NewSessionManager adopts sessions that survived a backend restart, wires
// their relays, and starts the fallback liveness poller.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		relays:   newRelayRegistry(),
		store:    newSessionStore(),
		index:    loadSessionIndex(),
		broker:   broker,
		stopPoll: make(chan struct{}),
	}
	for _, rec := range adoptSessions() {
		sm.store.add(rec)
	}
	sm.syncRelays()
	go sm.pollLoop()
	return sm
}

// GetRelay returns the relay for a session, or nil if not found.
func (sm *SessionManager) GetRelay(sessionName string) *Relay {
	return sm.relays.get(sessionName)
}

// SetSessionName stores a custom name on the live session's conversation (keyed
// by its uuid in the persistent index), so it shows in the sidebar and resume list.
func (sm *SessionManager) SetSessionName(sessionName, displayName string) {
	rec, ok := sm.store.get(sessionName)
	if !ok || rec.SessionID == "" {
		return
	}
	sm.index.setName(rec.SessionID, displayName)
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

// ListSessions returns all sessions from the in-memory store, with display
// names (custom name from the index, else the directory basename).
func (sm *SessionManager) ListSessions() []api.DisplaySession {
	records := sm.store.list()
	sessions := make([]api.DisplaySession, 0, len(records))
	for _, rec := range records {
		s := rec.display()
		if customName := sm.index.name(s.SessionID); customName != "" {
			s.DisplayName = customName
		} else {
			s.DisplayName = s.DirName
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// syncRelays ensures every session in the store has a running relay and relays
// for gone sessions are stopped.
func (sm *SessionManager) syncRelays() {
	records := sm.store.list()
	alive := make(map[string]bool, len(records))
	for _, rec := range records {
		alive[rec.Name] = true
	}
	// Relays for gone sessions are stopped and dropped; custom names live in the
	// persistent index (keyed by conversation uuid) and intentionally survive a
	// session ending — they drive the resume list.
	sm.relays.reconcile(alive, sm.newRelay)
}

// newRelay creates and starts a relay wired to notify the manager when it
// exits on its own. Returns nil when the attach fails.
func (sm *SessionManager) newRelay(name string) *Relay {
	relay := NewRelay(name)
	relay.onExit = func() { sm.relayExited(name, relay) }
	if err := relay.Start(); err != nil {
		slog.Warn("failed to start relay", "session", name, "error", err)
		return nil
	}
	return relay
}

// relayExited handles a relay that stopped on its own (attach reconnect
// exhausted or session gone). If the session still lives, a fresh relay is
// attached; otherwise the session is dropped and clients notified.
func (sm *SessionManager) relayExited(name string, relay *Relay) {
	if !sm.relays.dropIf(name, relay) {
		return // manager already removed/replaced it (e.g. Kill)
	}
	if sessionAlive(name) {
		sm.relays.ensure(name, sm.newRelay)
		return
	}
	sm.dropSession(name)
	sm.broker.Publish()
}

// dropSession removes a dead session's record, sidecar files, and uploads.
// Uploads are cleaned here (session death), NOT on relay stop — a relay can
// stop and be replaced while the session lives, and uploaded image paths
// already handed to claude must survive that.
func (sm *SessionManager) dropSession(name string) {
	sm.store.remove(name)
	removeSessionFiles(name)
	uploadPath := filepath.Join(uploadDir, name)
	if err := os.RemoveAll(uploadPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to clean upload dir", "path", uploadPath, "error", err)
	}
}

// Shutdown stops the polling goroutine. Sessions are NOT killed — they persist
// for reconnection after dashboard restart.
func (sm *SessionManager) Shutdown() {
	close(sm.stopPoll)
	slog.Info("session manager shut down (sessions preserved)")
}

// pollLoop is the safety net behind the event-driven paths: it verifies the
// liveness of every stored session (catching anything the relay-exit path
// missed) and restarts relays that failed to attach.
func (sm *SessionManager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			changed := false
			for _, rec := range sm.store.list() {
				if !rec.alive() {
					sm.relays.remove(rec.Name)
					sm.dropSession(rec.Name)
					changed = true
				}
			}
			sm.syncRelays()
			if changed {
				sm.broker.Publish()
			}
		case <-sm.stopPoll:
			return
		}
	}
}
