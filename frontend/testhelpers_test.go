package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const stubStatusBody = `{"state":"public","url":"hs://s000ab","error":null}`

// newUpstream starts a test server running handler and closes it at test end.
func newUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// newTestServer builds a bare Server pointed at backendURL for handler-level
// tests (share routes). The client has a short timeout so the unreachable case
// fails fast. Requests built with httptest are not tunnel-stamped, so share
// routes stay permissive (guard behavior is covered in shareguard_test.go).
func newTestServer(backendURL string) *Server {
	return &Server{
		backendURL: backendURL,
		client:     &http.Client{Timeout: 2 * time.Second},
	}
}

// setHolesail points a test Server's share proxy at the given sidecar URL.
func setHolesail(t *testing.T, s *Server, url string) {
	t.Helper()
	hp, err := newReverseProxy(url, shareDownBody())
	if err != nil {
		t.Fatalf("building holesail proxy: %v", err)
	}
	s.holesailProxy = hp
}
