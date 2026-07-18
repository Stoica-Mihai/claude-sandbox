package main

import (
	"errors"
	"log/slog"
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

// SessionManager coordinates session discovery, the relay registry, the
// session-list cache, and the persistent index; it provides spawn/kill/resume.
type SessionManager struct {
	relays   *relayRegistry
	cache    *sessionCache
	index    *SessionIndex // persisted uuid → {cwd,created,name}
	broker   *Broker
	stopPoll chan struct{}
}

// NewSessionManager creates a SessionManager wired to the given SSE broker and
// starts the background polling goroutine.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		relays:   newRelayRegistry(),
		cache:    &sessionCache{},
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
	return sm.relays.get(sessionName)
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
	alive := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		alive[s.Name] = true
	}
	// Relays for gone sessions are stopped and dropped; custom names live in the
	// persistent index (keyed by conversation uuid) and intentionally survive a
	// session ending — they drive the resume list.
	sm.relays.reconcile(alive, func(name string) *Relay {
		relay := NewRelay(name)
		if err := relay.Start(); err != nil {
			slog.Warn("failed to start relay", "session", name, "error", err)
			return nil
		}
		return relay
	})
}

// ListSessions returns all sessions, using cache if fresh. Display-name
// enrichment is applied on every call.
func (sm *SessionManager) ListSessions() []api.DisplaySession {
	if cached, ok := sm.cache.fresh(cacheTTL); ok {
		return sm.enrichSessions(cached)
	}
	return sm.enrichSessions(sm.refreshSessions())
}

// enrichSessions sets each session's display name (custom name from the index,
// else the directory basename). The index is internally synchronized.
func (sm *SessionManager) enrichSessions(sessions []api.DisplaySession) []api.DisplaySession {
	for i := range sessions {
		if customName := sm.index.name(sessions[i].SessionID); customName != "" {
			sessions[i].DisplayName = customName
		} else {
			sessions[i].DisplayName = sessions[i].DirName
		}
	}
	return sessions
}

// refreshSessions queries discovery, updates the cache, and returns the fresh
// list (the cache holds its own copy, so the caller may enrich this one).
func (sm *SessionManager) refreshSessions() []api.DisplaySession {
	sessions := discoverSessions()
	sm.cache.store(sessions, sessionsSignature(sessions))
	return sessions
}

// invalidateCache forces the next ListSessions call to re-discover.
func (sm *SessionManager) invalidateCache() {
	sm.cache.invalidate()
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
			changed := sig != sm.cache.signature()
			deadRelay := sm.relays.anyStopped()

			if changed {
				sm.cache.store(sessions, sig)
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
