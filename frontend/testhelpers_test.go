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
// fails fast; the guard's resolver never succeeds, so share routes stay
// permissive (guard behavior is covered in shareguard_test.go).
func newTestServer(backendURL string) *Server {
	return &Server{
		backendURL: backendURL,
		guard:      newFailingGuard(),
		client:     &http.Client{Timeout: 2 * time.Second},
	}
}
