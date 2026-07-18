package main

import (
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

// anyStopped reports whether any registered relay has stopped on its own.
func (r *relayRegistry) anyStopped() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, relay := range r.relays {
		if relay.IsStopped() {
			return true
		}
	}
	return false
}

// reconcile starts a relay (via start) for every name in alive that lacks one,
// and stops+drops relays whose name is absent from alive or whose relay has
// stopped. start returns nil to skip a name whose relay failed to start.
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
		if !alive[name] || relay.IsStopped() {
			relay.Stop()
			delete(r.relays, name)
		}
	}
}

// sessionCache is the TTL + change-signature cache of the discovered session list.
type sessionCache struct {
	mu   sync.RWMutex
	list []api.DisplaySession
	at   time.Time
	sig  string
}

// fresh returns a copy of the cached list when it is younger than ttl.
func (c *sessionCache) fresh(ttl time.Duration) ([]api.DisplaySession, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.at) >= ttl {
		return nil, false
	}
	out := make([]api.DisplaySession, len(c.list))
	copy(out, c.list)
	return out, true
}

// store replaces the cache with a copy of sessions and its change signature.
func (c *sessionCache) store(sessions []api.DisplaySession, sig string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = make([]api.DisplaySession, len(sessions))
	copy(c.list, sessions)
	c.at = time.Now()
	c.sig = sig
}

// invalidate forces the next fresh() to miss.
func (c *sessionCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
}

// signature returns the last stored change signature.
func (c *sessionCache) signature() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sig
}
