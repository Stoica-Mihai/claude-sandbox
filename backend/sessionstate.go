package main

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	api "claude-sandbox-api"
)

// relayRegistry owns the live session-name → relay map and its concurrency,
// separate from the session-list cache so the two no longer share one lock.
type relayRegistry struct {
	mu     sync.RWMutex
	relays map[string]*Relay
}

func newRelayRegistry() *relayRegistry {
	return &relayRegistry{relays: make(map[string]*Relay)}
}

// get returns the relay for a session, or nil if absent.
func (r *relayRegistry) get(name string) *Relay {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.relays[name]
}

// set registers (or replaces) a relay.
func (r *relayRegistry) set(name string, relay *Relay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relays[name] = relay
}

// remove stops and drops a relay if present.
func (r *relayRegistry) remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if relay, ok := r.relays[name]; ok {
		relay.Stop()
		delete(r.relays, name)
	}
}

// dropIf removes the entry for name only when it still maps to relay,
// reporting whether it did. It does not stop the relay (used for relays that
// already stopped on their own).
func (r *relayRegistry) dropIf(name string, relay *Relay) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.relays[name] != relay {
		return false
	}
	delete(r.relays, name)
	return true
}

// reconcile starts a relay (via start) for every name in alive that lacks one,
// and stops+drops relays whose name is absent from alive. start returns nil to
// skip a name whose relay failed to start.
func (r *relayRegistry) reconcile(alive map[string]bool, start func(name string) *Relay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range alive {
		if _, ok := r.relays[name]; !ok {
			if relay := start(name); relay != nil {
				r.relays[name] = relay
			}
		}
	}
	for name, relay := range r.relays {
		if !alive[name] {
			relay.Stop()
			delete(r.relays, name)
		}
	}
}

// sessionRecord is the authoritative in-memory state of one live session.
// Sidecar files exist only to rebuild these records at boot (adoptSessions).
type sessionRecord struct {
	Name      string
	CWD       string
	Created   time.Time
	SessionID string // claude conversation uuid
	PID       int    // inner bash PID; 0 if the sidecar was never written
}

// display converts a record to its API view.
func (rec sessionRecord) display() api.DisplaySession {
	return api.DisplaySession{
		Name:      rec.Name,
		CWD:       rec.CWD,
		DirName:   filepath.Base(rec.CWD),
		CreatedAt: rec.Created,
		Alive:     true,
		SessionID: rec.SessionID,
	}
}

// alive probes the session's inner process, preferring the recorded PID and
// falling back to the sidecar/socket check when it was never captured.
func (rec sessionRecord) alive() bool {
	if rec.PID > 0 {
		return processAlive(rec.PID)
	}
	return sessionAlive(rec.Name)
}

// sessionStore is the in-memory session registry, mutated by spawn/kill/exit
// events and seeded once at boot.
type sessionStore struct {
	mu sync.RWMutex
	m  map[string]sessionRecord
}

func newSessionStore() *sessionStore {
	return &sessionStore{m: make(map[string]sessionRecord)}
}

// add registers (or replaces) a session record.
func (s *sessionStore) add(rec sessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[rec.Name] = rec
}

// remove drops a session record.
func (s *sessionStore) remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
}

// get returns the record for a session name.
func (s *sessionStore) get(name string) (sessionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.m[name]
	return rec, ok
}

// byUUID returns the record whose conversation uuid matches.
func (s *sessionStore) byUUID(uuid string) (sessionRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rec := range s.m {
		if rec.SessionID == uuid {
			return rec, true
		}
	}
	return sessionRecord{}, false
}

// list returns all records, oldest first — matching the tab bar (new tabs
// append on the right), so a session sits in the same relative spot in
// sidebar and tabs.
func (s *sessionStore) list() []sessionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]sessionRecord, 0, len(s.m))
	for _, rec := range s.m {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
