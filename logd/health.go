package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	api "claude-sandbox-api"
)

const (
	defaultHealthInterval = 2 * time.Second
	defaultProbeTimeout   = 1 * time.Second
	defaultFailsToDown    = 2
	statusSubQueue        = 16
)

// target is a service and the /healthz URL to probe it at.
type target struct {
	service string
	url     string
}

// probeFunc reports whether the target at url is healthy (HTTP 200).
type probeFunc func(url string) bool

type svcState struct {
	state       string // api.ServiceUp / api.ServiceDown
	since       time.Time
	consecFails int
}

// healthMonitor polls each target's /healthz on an interval, tracks edge-
// debounced up/down state, and fans status snapshots out to SSE subscribers.
// Services start `down` (unconfirmed) and flip `up` on the first healthy poll.
type healthMonitor struct {
	targets     []target
	probe       probeFunc
	interval    time.Duration
	failsToDown int
	store       *store // for last-log-seen

	mu     sync.Mutex
	states map[string]*svcState
	subs   map[int]chan []api.ServiceStatus
	nextID int
}

func newHealthMonitor(targets []target, probe probeFunc, store *store) *healthMonitor {
	if probe == nil {
		probe = httpProbe
	}
	m := &healthMonitor{
		targets:     targets,
		probe:       probe,
		interval:    defaultHealthInterval,
		failsToDown: defaultFailsToDown,
		store:       store,
		states:      map[string]*svcState{},
		subs:        map[int]chan []api.ServiceStatus{},
	}
	now := time.Now()
	for _, t := range targets {
		m.states[t.service] = &svcState{state: api.ServiceDown, since: now}
	}
	return m
}

// httpProbe is the default probe: GET url, healthy iff it answers 200.
func httpProbe(url string) bool {
	client := &http.Client{Timeout: defaultProbeTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Run polls until ctx is cancelled.
func (m *healthMonitor) Run(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	m.pollOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.pollOnce()
		}
	}
}

// pollOnce probes every target and broadcasts if any state changed.
func (m *healthMonitor) pollOnce() {
	changed := false
	for _, tg := range m.targets {
		if m.apply(tg.service, m.probe(tg.url)) {
			changed = true
		}
	}
	if changed {
		m.broadcast()
	}
}

// apply folds one probe result into a service's debounced state, reporting
// whether the state transitioned. Caller holds no lock.
func (m *healthMonitor) apply(service string, healthy bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[service]
	if s == nil {
		s = &svcState{state: api.ServiceDown, since: time.Now()}
		m.states[service] = s
	}
	if healthy {
		s.consecFails = 0
		if s.state != api.ServiceUp {
			s.state = api.ServiceUp
			s.since = time.Now()
			return true
		}
		return false
	}
	s.consecFails++
	if s.state != api.ServiceDown && s.consecFails >= m.failsToDown {
		s.state = api.ServiceDown
		s.since = time.Now()
		return true
	}
	return false
}

// status returns the current per-service status, merged with last-log-seen,
// in target order.
func (m *healthMonitor) status() []api.ServiceStatus {
	var seen map[string]time.Time
	if m.store != nil {
		seen = m.store.lastSeenSnapshot()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]api.ServiceStatus, 0, len(m.targets))
	for _, tg := range m.targets {
		s := m.states[tg.service]
		st := api.ServiceStatus{Service: tg.service, State: s.state, Since: s.since}
		if ts, ok := seen[tg.service]; ok {
			st.LastLogSeen = &ts
		}
		out = append(out, st)
	}
	return out
}

func (m *healthMonitor) subscribe() (int, <-chan []api.ServiceStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	m.nextID++
	ch := make(chan []api.ServiceStatus, statusSubQueue)
	m.subs[id] = ch
	return id, ch
}

func (m *healthMonitor) unsubscribe(id int) {
	m.mu.Lock()
	delete(m.subs, id)
	m.mu.Unlock()
}

// broadcast pushes the current snapshot to every subscriber (evict-on-full).
func (m *healthMonitor) broadcast() {
	snap := m.status()
	m.mu.Lock()
	subs := make([]chan []api.ServiceStatus, 0, len(m.subs))
	for _, ch := range m.subs {
		subs = append(subs, ch)
	}
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- snap:
		default:
		}
	}
}
