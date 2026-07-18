package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newFailingGuard returns a guard whose resolver always errors — permissive
// (fail-open) because the hostname never resolves.
func newFailingGuard() *tunnelGuard {
	return &tunnelGuard{
		host:    "holesail",
		resolve: func(string) ([]net.IP, error) { return nil, errors.New("no such host") },
		ttl:     10 * time.Second,
	}
}

// newFixedGuard returns a guard resolving to the given IPs, counting calls.
func newFixedGuard(calls *int, ips ...string) *tunnelGuard {
	return &tunnelGuard{
		host: "holesail",
		resolve: func(string) ([]net.IP, error) {
			*calls++
			out := make([]net.IP, len(ips))
			for i, s := range ips {
				out[i] = net.ParseIP(s)
			}
			return out, nil
		},
		ttl: 10 * time.Second,
	}
}

func reqFrom(remoteAddr string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/share/status", nil)
	r.RemoteAddr = remoteAddr
	return r
}

func TestGuardBlocksTunnelIP(t *testing.T) {
	calls := 0
	g := newFixedGuard(&calls, "172.18.0.5")
	if !g.isTunnelRequest(reqFrom("172.18.0.5:53210")) {
		t.Fatal("request from the sidecar IP should be identified as tunneled")
	}
}

func TestGuardAllowsOtherIPs(t *testing.T) {
	calls := 0
	g := newFixedGuard(&calls, "172.18.0.5")
	if g.isTunnelRequest(reqFrom("172.18.0.1:40000")) {
		t.Fatal("request from a non-sidecar IP should be allowed")
	}
}

func TestGuardCachesResolutions(t *testing.T) {
	calls := 0
	g := newFixedGuard(&calls, "172.18.0.5")
	g.isTunnelRequest(reqFrom("172.18.0.5:1"))
	g.isTunnelRequest(reqFrom("172.18.0.5:2"))
	g.isTunnelRequest(reqFrom("172.18.0.5:3"))
	if calls != 1 {
		t.Fatalf("expected 1 resolver call within TTL, got %d", calls)
	}
}

// A cache miss against a cache older than the double-check threshold forces a
// re-resolve, so a sidecar restarted with a new IP is still blocked mid-TTL.
func TestGuardReResolvesOnMissAfterRestart(t *testing.T) {
	calls := 0
	current := "172.18.0.5"
	g := &tunnelGuard{
		host: "holesail",
		resolve: func(string) ([]net.IP, error) {
			calls++
			return []net.IP{net.ParseIP(current)}, nil
		},
		ttl: 10 * time.Second,
	}
	g.isTunnelRequest(reqFrom("172.18.0.5:1")) // populate cache with .5
	current = "172.18.0.9"                     // sidecar restarted with a new IP
	g.refreshed = time.Now().Add(-2 * time.Second)
	if !g.isTunnelRequest(reqFrom("172.18.0.9:1")) {
		t.Fatal("request from the restarted sidecar's new IP should be blocked")
	}
	if calls != 2 {
		t.Fatalf("expected a forced re-resolve, got %d resolver calls", calls)
	}
}

func TestGuardFailsOpenWhenNeverResolved(t *testing.T) {
	g := newFailingGuard()
	if g.isTunnelRequest(reqFrom("172.18.0.5:1")) {
		t.Fatal("guard must fail open when the hostname has never resolved")
	}
}

func TestGuardKeepsStaleCacheOnResolveFailure(t *testing.T) {
	fail := false
	g := &tunnelGuard{
		host: "holesail",
		resolve: func(string) ([]net.IP, error) {
			if fail {
				return nil, errors.New("dns down")
			}
			return []net.IP{net.ParseIP("172.18.0.5")}, nil
		},
		ttl: 10 * time.Second,
	}
	g.isTunnelRequest(reqFrom("172.18.0.5:1")) // successful resolution
	fail = true
	g.expires = time.Now().Add(-time.Second) // expire the cache
	if !g.isTunnelRequest(reqFrom("172.18.0.5:2")) {
		t.Fatal("stale cache should still block the sidecar IP when resolution fails")
	}
}

// TestHandleShareProxyBlocksTunnelMutation verifies a mutating action from the
// tunnel is 403'd before the sidecar is contacted.
func TestHandleShareProxyBlocksTunnelMutation(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
	}))
	defer upstream.Close()

	calls := 0
	s := newTestServer(upstream.URL)
	s.holesailURL = upstream.URL
	s.guard = newFixedGuard(&calls, "172.18.0.5")

	req := httptest.NewRequest(http.MethodPost, "/api/share/start", nil)
	req.RemoteAddr = "172.18.0.5:53210"
	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if upstreamCalled {
		t.Fatal("sidecar must not be contacted for a tunneled mutation")
	}
}

// TestHandleShareProxyAllowsTunnelStatus verifies the read-only status GET is
// proxied even from the tunnel, so a client browsing over the tunnel can still
// see the sharing state (and its ambient glow).
func TestHandleShareProxyAllowsTunnelStatus(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"state":"public","url":"hs://s000ab","error":null}`)
	}))
	defer upstream.Close()

	calls := 0
	s := newTestServer(upstream.URL)
	s.holesailURL = upstream.URL
	s.guard = newFixedGuard(&calls, "172.18.0.5")

	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, reqFrom("172.18.0.5:53210")) // GET /api/share/status

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a tunnel status read, got %d", rec.Code)
	}
	if !upstreamCalled {
		t.Fatal("status GET should be proxied to the sidecar even over the tunnel")
	}
}
