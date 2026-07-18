package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const stubStatusBody = `{"state":"public","url":"hs://s000ab","error":null}`

// newUpstream starts a test server running handler and closes it at test end.
func newUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}
