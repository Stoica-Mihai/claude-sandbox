package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer builds a Server pointed at backendURL, wired for handler tests.
// The client has a short timeout so the unreachable-backend case fails fast.
// The guard's resolver never succeeds, so share routes stay permissive here;
// guard behavior is covered in shareguard_test.go.
func newTestServer(backendURL string) *Server {
	return &Server{
		backendURL: backendURL,
		guard:      newFailingGuard(),
		client:     &http.Client{Timeout: 2 * time.Second},
	}
}

// TestHandleCreateDirectoryProxyForwardsRequest verifies the create-directory
// proxy forwards method, path, query, headers, and body to the backend, and
// returns the backend's status and body verbatim (the route consumed as JSON by
// views.js, never re-decoded here).
func TestHandleCreateDirectoryProxyForwardsRequest(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotContentType, gotBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"created":"/workspace/newproj"}`)
	}))
	defer backend.Close()

	s := newTestServer(backend.URL)

	body := `{"parent":"/workspace","name":"newproj"}`
	req := httptest.NewRequest("POST", "/api/directories?dry=1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleCreateDirectoryProxy(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	respBody, _ := io.ReadAll(res.Body)
	if got, want := string(respBody), `{"created":"/workspace/newproj"}`; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("response Content-Type = %q, want application/json", ct)
	}

	if gotMethod != "POST" {
		t.Errorf("backend method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/directories" {
		t.Errorf("backend path = %q, want /api/directories", gotPath)
	}
	if gotQuery != "dry=1" {
		t.Errorf("backend query = %q, want dry=1", gotQuery)
	}
	if gotContentType != "application/json" {
		t.Errorf("backend Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody != body {
		t.Errorf("backend body = %q, want %q", gotBody, body)
	}
}

// TestHandleCreateDirectoryProxyPassesBackendError verifies a backend error
// status and body pass through unchanged, rather than being swallowed or
// re-decoded (the distinction from the GET template-render directories route).
func TestHandleCreateDirectoryProxyPassesBackendError(t *testing.T) {
	const errBody = `{"error":"directory exists"}`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, errBody)
	}))
	defer backend.Close()

	s := newTestServer(backend.URL)

	req := httptest.NewRequest("POST", "/api/directories", strings.NewReader(`{"name":"dup"}`))
	rec := httptest.NewRecorder()

	s.handleCreateDirectoryProxy(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusConflict)
	}
	respBody, _ := io.ReadAll(res.Body)
	if got := string(respBody); got != errBody {
		t.Errorf("response body = %q, want %q (backend error must pass through verbatim)", got, errBody)
	}
}

// TestHandleCreateDirectoryProxyBackendUnreachable verifies a failed backend
// connection yields 502 Bad Gateway.
func TestHandleCreateDirectoryProxyBackendUnreachable(t *testing.T) {
	// Reserve a port with a listener, then close it so nothing answers there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	s := newTestServer("http://" + addr)

	req := httptest.NewRequest("POST", "/api/directories", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()

	s.handleCreateDirectoryProxy(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusBadGateway)
	}
}
