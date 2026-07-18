package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// tunnelRequest builds a request stamped as tunnel-originated, the way the
// tunnel listener's markTunnel middleware does.
func tunnelRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	var stamped *http.Request
	markTunnel(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		stamped = req
	})).ServeHTTP(httptest.NewRecorder(), r)
	return stamped
}

func TestMarkTunnelStampsRequests(t *testing.T) {
	if !isTunnelRequest(tunnelRequest(http.MethodGet, "/")) {
		t.Fatal("a request through markTunnel must be identified as tunneled")
	}
	if isTunnelRequest(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("a request on the main listener must not be identified as tunneled")
	}
}

// TestHandleShareProxyBlocksTunnelMutation verifies a mutating action from the
// tunnel is 403'd before the sidecar is contacted.
func TestHandleShareProxyBlocksTunnelMutation(t *testing.T) {
	upstreamCalled := false
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
	})

	s := newTestServer(upstream.URL)
	s.holesailURL = upstream.URL

	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, tunnelRequest(http.MethodPost, "/api/share/start"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if upstreamCalled {
		t.Fatal("sidecar must not be contacted for a tunneled mutation")
	}
}

// TestHandleShareProxyAllowsDirectMutation verifies a mutation on the main
// listener is proxied to the sidecar.
func TestHandleShareProxyAllowsDirectMutation(t *testing.T) {
	upstreamCalled := false
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, stubStatusBody)
	})

	s := newTestServer(upstream.URL)
	s.holesailURL = upstream.URL

	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, httptest.NewRequest(http.MethodPost, "/api/share/start", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !upstreamCalled {
		t.Fatal("a direct mutation must reach the sidecar")
	}
}

// TestHandleShareProxyAllowsTunnelStatus verifies the read-only status GET is
// proxied even from the tunnel, so a client browsing over the tunnel can still
// see the sharing state (and its ambient glow).
func TestHandleShareProxyAllowsTunnelStatus(t *testing.T) {
	upstreamCalled := false
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, stubStatusBody)
	})

	s := newTestServer(upstream.URL)
	s.holesailURL = upstream.URL

	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, tunnelRequest(http.MethodGet, "/api/share/status"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a tunnel status read, got %d", rec.Code)
	}
	if !upstreamCalled {
		t.Fatal("status GET should be proxied to the sidecar even over the tunnel")
	}
}