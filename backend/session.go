package main

import (
	"errors"
	"log/slog"
	"sync"
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
	// cacheTTL is how long discovery results are cached.
	cacheTTL = 2 * time.Second
	// pollInterval is how often the background poller checks for session changes.
	pollInterval = 5 * time.Second
	// maxSpawnRetries is the number of retries on session-name collision.
	maxSpawnRetries = 3
	// termType is the TERM environment variable set for new sessions.
	termType = "xterm-256color"
	// killGracePeriod is how long Kill waits after SIGTERM before SIGKILL.
	killGracePeriod = 2 * time.Second
	// pidWaitTimeout bounds how long Spawn waits for the inner bash to write the
	// PID sidecar before publishing, so discovery (keyed off the PID sidecar)
	// sees the new session immediately instead of after a later poll.
	pidWaitTimeout = 1 * time.Second
)

// SessionManager discovers dtach sessions, manages relays, and provides
// spawn/kill operations.
type SessionManager struct {
	mu        sync.RWMutex
	cached    []api.DisplaySession
	cachedAt  time.Time
	cachedSig string // discovery signature for change detection
	relays    map[string]*Relay
	index     *SessionIndex // persisted uuid → {cwd,created,name}
	broker    *Broker
	stopPoll  chan struct{}
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
func (sm *SessionManager) syncRelays(sessions []api.DisplaySession) {
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

// ListSessions returns all sessions, using cache if fresh. Display-name
// enrichment is applied on every call.
func (sm *SessionManager) ListSessions() []api.DisplaySession {
	sm.mu.RLock()
	if time.Since(sm.cachedAt) < cacheTTL {
		result := make([]api.DisplaySession, len(sm.cached))
		copy(result, sm.cached)
		sm.mu.RUnlock()
		return sm.enrichSessions(result)
	}
	sm.mu.RUnlock()

	return sm.enrichSessions(sm.refreshSessions())
}

// enrichSessions sets each session's display name (custom name from the index,
// else the directory basename).
func (sm *SessionManager) enrichSessions(sessions []api.DisplaySession) []api.DisplaySession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for i := range sessions {
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
func (sm *SessionManager) refreshSessions() []api.DisplaySession {
	sessions := discoverSessions()
	sig := sessionsSignature(sessions)

	sm.mu.Lock()
	sm.cached = sessions
	sm.cachedAt = time.Now()
	sm.cachedSig = sig
	sm.mu.Unlock()

	result := make([]api.DisplaySession, len(sessions))
	copy(result, sessions)
	return result
}

// invalidateCache forces the next ListSessions call to re-discover.
func (sm *SessionManager) invalidateCache() {
	sm.mu.Lock()
	sm.cachedAt = time.Time{}
	sm.mu.Unlock()
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

			// A relay can die on its own (attach reconnect exhausted) while its
			// session lives — the signature won't change, so check for stopped
			// relays explicitly or the session stays unreachable until the
			// session set happens to change.
			sm.mu.RLock()
			changed := sig != sm.cachedSig
			deadRelay := false
			for _, relay := range sm.relays {
				if relay.IsStopped() {
					deadRelay = true
					break
				}
			}
			sm.mu.RUnlock()

			if changed {
				sm.mu.Lock()
				sm.cached = sessions
				sm.cachedAt = time.Now()
				sm.cachedSig = sig
				sm.mu.Unlock()
			}
			if changed || deadRelay {
				sm.syncRelays(sessions)
			}

			// Publish only on change — clients refetch the sessions fragment on
			// every event, and durations tick client-side, so an unconditional
			// 5s publish was N-clients × render for nothing. SSE keepalive is
			// handled by the SSE handler itself.
			if changed {
				sm.broker.Publish()
			}
		case <-sm.stopPoll:
			return
		}
	}
}
