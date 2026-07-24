package main

import (
	"strings"
	"testing"

	api "claude-sandbox-api"
)

// TestHealthTargetsUseComposeServiceNames guards the probe hostnames against the
// sessiond-vs-sessions trap (Docker DNS resolves the compose *service* name).
func TestHealthTargetsUseComposeServiceNames(t *testing.T) {
	wantHost := map[string]string{
		"backend": "backend", "frontend": "frontend",
		"sessiond": "sessions", "holesail": "holesail",
		// logd is intentionally absent — it does not probe itself.
	}
	for _, tg := range healthTargets() {
		h, ok := wantHost[tg.service]
		if !ok {
			t.Errorf("unexpected target service %q", tg.service)
			continue
		}
		if !strings.Contains(tg.url, "//"+h+":") {
			t.Errorf("%s url = %q, want host %q", tg.service, tg.url, h)
		}
		delete(wantHost, tg.service)
	}
	if len(wantHost) != 0 {
		t.Errorf("missing targets: %v", wantHost)
	}
}

// stateFor finds a service's status in a snapshot.
func stateFor(snap []api.ServiceStatus, svc string) api.ServiceStatus {
	for _, s := range snap {
		if s.Service == svc {
			return s
		}
	}
	return api.ServiceStatus{}
}

func newTestMonitor(healthy *bool) *healthMonitor {
	targets := []target{{"backend", "http://backend/healthz"}}
	probe := func(string) bool { return *healthy }
	return newHealthMonitor(targets, probe, nil)
}

func TestHealthStartsDownThenUpOnFirstSuccess(t *testing.T) {
	up := true
	m := newTestMonitor(&up)
	if got := stateFor(m.status(), "backend").State; got != api.ServiceDown {
		t.Fatalf("initial state = %q, want down (unconfirmed)", got)
	}
	m.pollOnce()
	if got := stateFor(m.status(), "backend").State; got != api.ServiceUp {
		t.Fatalf("after healthy poll = %q, want up", got)
	}
}

func TestHealthSingleBlipDoesNotFlip(t *testing.T) {
	up := true
	m := newTestMonitor(&up)
	m.pollOnce() // up
	sinceUp := stateFor(m.status(), "backend").Since

	up = false
	m.pollOnce() // 1 fail — below failsToDown (2)
	s := stateFor(m.status(), "backend")
	if s.State != api.ServiceUp {
		t.Fatalf("after 1 fail = %q, want still up (debounce)", s.State)
	}
	if !s.Since.Equal(sinceUp) {
		t.Errorf("Since changed on a non-transition")
	}

	up = true
	m.pollOnce() // recovers before the 2nd fail
	if got := stateFor(m.status(), "backend").State; got != api.ServiceUp {
		t.Fatalf("after recovery = %q, want up", got)
	}
}

func TestHealthSustainedFailureMarksDownThenRecovers(t *testing.T) {
	up := true
	m := newTestMonitor(&up)
	m.pollOnce() // up

	up = false
	m.pollOnce() // fail 1
	m.pollOnce() // fail 2 → down
	down := stateFor(m.status(), "backend")
	if down.State != api.ServiceDown {
		t.Fatalf("after 2 fails = %q, want down", down.State)
	}

	up = true
	m.pollOnce() // 1 success → up
	recovered := stateFor(m.status(), "backend")
	if recovered.State != api.ServiceUp {
		t.Fatalf("after recovery = %q, want up", recovered.State)
	}
	if !recovered.Since.After(down.Since) {
		t.Errorf("Since should advance on the up transition")
	}
}

func TestHealthStatusIncludesLastLogSeen(t *testing.T) {
	up := true
	st := newStore(t.TempDir(), 100)
	st.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "hi"})
	m := newHealthMonitor([]target{{"backend", "http://backend/healthz"}}, func(string) bool { return up }, st)
	if ls := stateFor(m.status(), "backend").LastLogSeen; ls == nil {
		t.Errorf("LastLogSeen should be set after a log was ingested")
	}
}

func TestHealthSubscribeReceivesOnTransition(t *testing.T) {
	up := false
	m := newTestMonitor(&up)
	id, ch := m.subscribe()
	defer m.unsubscribe(id)

	up = true
	m.pollOnce() // down→up transition → broadcast
	select {
	case snap := <-ch:
		if stateFor(snap, "backend").State != api.ServiceUp {
			t.Errorf("pushed snapshot not up")
		}
	default:
		t.Fatal("no snapshot pushed on transition")
	}
}
