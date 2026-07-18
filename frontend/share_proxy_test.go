package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleShareProxyForwardsRequest verifies the share proxy forwards
// method, path, and body to the sidecar verbatim, and returns the sidecar's
// status, headers, and body unchanged.
func TestHandleShareProxyForwardsRequest(t *testing.T) {
	var gotMethod, gotPath string
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, stubStatusBody)
	})

	s := newTestServer("http://unused-backend")
	s.holesailURL = upstream.URL

	req := httptest.NewRequest(http.MethodPost, "/api/share/start", nil)
	req.RemoteAddr = "192.168.1.10:40000"
	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, req)

	if gotMethod != http.MethodPost || gotPath != "/api/share/start" {
		t.Fatalf("sidecar saw %s %s, want POST /api/share/start", gotMethod, gotPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"state":"public"`) {
		t.Fatalf("body not passed through verbatim: %s", rec.Body.String())
	}
}

// TestHandleShareProxyPassesUpstreamError verifies the wrapper's error shape
// (502 + JSON status body) reaches the browser unchanged.
func TestHandleShareProxyPassesUpstreamError(t *testing.T) {
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"state":"error","url":null,"error":"tunnel did not become ready in time"}`)
	})

	s := newTestServer("http://unused-backend")
	s.holesailURL = upstream.URL

	req := httptest.NewRequest(http.MethodPost, "/api/share/start", nil)
	req.RemoteAddr = "192.168.1.10:40000"
	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 passthrough, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"state":"error"`) {
		t.Fatalf("error body not passed through: %s", rec.Body.String())
	}
}

// TestHandleShareProxyUpstreamUnreachable verifies an unreachable sidecar
// yields a 502 rather than a hang or panic.
func TestHandleShareProxyUpstreamUnreachable(t *testing.T) {
	s := newTestServer("http://unused-backend")
	s.holesailURL = "http://127.0.0.1:1" // nothing listens here

	rec := httptest.NewRecorder()
	s.handleShareProxy(rec, httptest.NewRequest(http.MethodGet, "/api/share/status", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for unreachable sidecar, got %d", rec.Code)
	}
}
