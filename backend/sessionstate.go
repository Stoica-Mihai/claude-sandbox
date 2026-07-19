package main

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	api "claude-sandbox-api"
)

// sessionRecord is the backend's view of one live session, fed by sessiond
// spawn replies and LIST reconciliation.
type sessionRecord struct {
	Name      string
	CWD       string
	Created   time.Time
	SessionID string // claude conversation uuid
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

// sessionStore is the in-memory session registry, mutated by spawn/kill events
// and the sessiond LIST reconciliation.
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
