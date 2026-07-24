package main

// Characterization tests that lock the frontend's observable proxy/transform
// contract through the real mux (NewServer), so they survive a change of proxy
// implementation. They are the safety net for replacing the hand-rolled
// per-route proxy with a catch-all: passthrough routes must still forward
// verbatim, and the transform routes (spawn/directories/kill/rename) must NOT
// be swallowed by any /api/ catch-all.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "claude-sandbox-api"
)

const noHolesail = "http://holesail.invalid:9000"
const noLogd = "http://logd.invalid:8082"

// newMuxServer stands up the full frontend mux pointed at the given upstreams
// (logd defaulted to an unreachable stub for tests that don't exercise it).
func newMuxServer(t *testing.T, backendURL, holesailURL string) *http.ServeMux {
	return newMuxServerFull(t, backendURL, holesailURL, noLogd)
}

func newMuxServerFull(t *testing.T, backendURL, holesailURL, logdURL string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if _, err := NewServer(backendURL, holesailURL, logdURL, mux); err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return mux
}

// TestProxyPassthroughForwardsVerbatim locks the verbatim-forward contract for
// every passthrough route: method, path, query, body, and Content-Type reach
// the backend; hop-by-hop headers do not; the backend's status, body, and
// response headers come back unchanged.
func TestProxyPassthroughForwardsVerbatim(t *testing.T) {
	cases := []struct {
		name, method, path string
		hasBody            bool
	}{
		{"history", "GET", "/api/sessions/history", false},
		{"settings-get", "GET", "/api/settings", false},
		{"settings-put", "PUT", "/api/settings", true},
		{"uiprefs-get", "GET", "/api/ui-prefs", false},
		{"uiprefs-put", "PUT", "/api/ui-prefs", true},
		{"create-dir", "POST", "/api/directories", true},
		{"upload", "POST", "/api/sessions/claude-x/upload", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery, gotBody, gotCT, gotConn string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
				gotCT = r.Header.Get("Content-Type")
				gotConn = r.Header.Get("Connection")
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.Header().Set("X-Backend-Marker", "yes")
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, "BACKEND-BODY")
			}))
			defer backend.Close()

			mux := newMuxServer(t, backend.URL, noHolesail)

			body := ""
			if c.hasBody {
				body = `{"k":"v"}`
			}
			req := httptest.NewRequest(c.method, c.path+"?a=1&b=2", strings.NewReader(body))
			if c.hasBody {
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("Connection", "keep-alive")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			if rb, _ := io.ReadAll(res.Body); string(rb) != "BACKEND-BODY" {
				t.Errorf("response body = %q, want BACKEND-BODY", rb)
			}
			if res.Header.Get("X-Backend-Marker") != "yes" {
				t.Errorf("backend response header not copied through")
			}
			if gotMethod != c.method {
				t.Errorf("backend method = %q, want %q", gotMethod, c.method)
			}
			if gotPath != c.path {
				t.Errorf("backend path = %q, want %q", gotPath, c.path)
			}
			if gotQuery != "a=1&b=2" {
				t.Errorf("backend query = %q, want a=1&b=2", gotQuery)
			}
			if gotConn != "" {
				t.Errorf("hop-by-hop Connection leaked to backend: %q", gotConn)
			}
			if c.hasBody {
				if gotBody != body {
					t.Errorf("backend body = %q, want %q", gotBody, body)
				}
				if gotCT != "application/json" {
					t.Errorf("backend Content-Type = %q, want application/json", gotCT)
				}
			}
		})
	}
}

// TestProxyPassthroughBackendErrorVerbatim locks that a backend error status and
// body pass through a passthrough route unchanged.
func TestProxyPassthroughBackendErrorVerbatim(t *testing.T) {
	const errBody = `{"error":"nope"}`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, errBody)
	}))
	defer backend.Close()

	mux := newMuxServer(t, backend.URL, noHolesail)
	req := httptest.NewRequest("GET", "/api/settings", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", res.StatusCode)
	}
	if rb, _ := io.ReadAll(res.Body); string(rb) != errBody {
		t.Errorf("body = %q, want %q", rb, errBody)
	}
}

// TestProxyPassthroughBackendUnreachable locks that a dead backend yields 502
// through a passthrough route.
func TestProxyPassthroughBackendUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	mux := newMuxServer(t, "http://"+addr, noHolesail)
	req := httptest.NewRequest("GET", "/api/settings", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Result().StatusCode)
	}
}

// TestSpawnTransformsFormToJSON is the key precedence guard: POST /api/sessions
// is a TRANSFORM route (form-urlencoded -> JSON), not a passthrough. A catch-all
// must not swallow it. The backend must see JSON (not the raw form), and the
// frontend must set X-Terminal-Id from the backend's session_name and return the
// rendered HTML fragment, not the backend JSON.
func TestSpawnTransformsFormToJSON(t *testing.T) {
	var spawnBody, spawnCT string
	backend := http.NewServeMux()
	backend.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		spawnBody, spawnCT = string(b), r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"session_name":"claude-abc"}`)
	})
	backend.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	})
	bsrv := httptest.NewServer(backend)
	defer bsrv.Close()

	mux := newMuxServer(t, bsrv.URL, noHolesail)
	req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader("cwd=/workspace/x&resume="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if !strings.Contains(spawnBody, `"cwd":"/workspace/x"`) {
		t.Errorf("backend spawn body = %q; want JSON with cwd (form was not transformed)", spawnBody)
	}
	if spawnCT != "application/json" {
		t.Errorf("backend spawn Content-Type = %q, want application/json", spawnCT)
	}
	if got := res.Header.Get("X-Terminal-Id"); got != "claude-abc" {
		t.Errorf("X-Terminal-Id = %q, want claude-abc", got)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html (rendered fragment)", ct)
	}
}

// TestSpawnForwardsBackendError locks that a backend spawn error is forwarded
// with its status and body (not rendered as a fragment).
func TestSpawnForwardsBackendError(t *testing.T) {
	backend := http.NewServeMux()
	backend.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"bad cwd"}`)
	})
	bsrv := httptest.NewServer(backend)
	defer bsrv.Close()

	mux := newMuxServer(t, bsrv.URL, noHolesail)
	req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader("cwd=/nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
	if rb, _ := io.ReadAll(res.Body); !strings.Contains(string(rb), "bad cwd") {
		t.Errorf("body = %q, want backend error forwarded", rb)
	}
}

// TestDirectoriesGETRendersHTML locks that GET /api/directories is a TRANSFORM
// (backend JSON -> rendered HTML picker), not a passthrough.
func TestDirectoriesGETRendersHTML(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.DirectoryData{FullPath: "/workspace", Dirs: []string{"a", "b"}})
	}))
	defer backend.Close()

	mux := newMuxServer(t, backend.URL, noHolesail)
	req := httptest.NewRequest("GET", "/api/directories", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html (rendered picker, not JSON passthrough)", ct)
	}
}

// TestKillProxiesThenRendersFragment locks kill: DELETE proxied to the backend,
// then the sessions fragment rendered (HTML 200) on success.
func TestKillProxiesThenRendersFragment(t *testing.T) {
	var delMethod, delPath string
	backend := http.NewServeMux()
	backend.HandleFunc("DELETE /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		delMethod, delPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	backend.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	})
	bsrv := httptest.NewServer(backend)
	defer bsrv.Close()

	mux := newMuxServer(t, bsrv.URL, noHolesail)
	req := httptest.NewRequest("DELETE", "/api/sessions/claude-x", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if delMethod != "DELETE" || delPath != "/api/sessions/claude-x" {
		t.Errorf("backend saw %s %s, want DELETE /api/sessions/claude-x", delMethod, delPath)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (fragment)", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html fragment", ct)
	}
}

// TestRenameProxiesBodyThenFragment locks rename: PUT with the JSON body proxied
// to the backend, then the sessions fragment rendered on success.
func TestRenameProxiesBodyThenFragment(t *testing.T) {
	var gotMethod, gotBody string
	backend := http.NewServeMux()
	backend.HandleFunc("PUT /api/sessions/{id}/name", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	backend.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	})
	bsrv := httptest.NewServer(backend)
	defer bsrv.Close()

	mux := newMuxServer(t, bsrv.URL, noHolesail)
	req := httptest.NewRequest("PUT", "/api/sessions/claude-x/name", strings.NewReader(`{"name":"foo"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if gotMethod != "PUT" || gotBody != `{"name":"foo"}` {
		t.Errorf("backend saw %s body=%q, want PUT {\"name\":\"foo\"}", gotMethod, gotBody)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (fragment)", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html fragment", ct)
	}
}

// TestDeleteHistoryPassesStatusNoFragment locks that delete-history proxies and
// passes the backend status straight through (no fragment render).
func TestDeleteHistoryPassesStatusNoFragment(t *testing.T) {
	backend := http.NewServeMux()
	backend.HandleFunc("DELETE /api/sessions/history/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	bsrv := httptest.NewServer(backend)
	defer bsrv.Close()

	mux := newMuxServer(t, bsrv.URL, noHolesail)
	req := httptest.NewRequest("DELETE", "/api/sessions/history/uuid-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204 (status passthrough)", res.StatusCode)
	}
	if rb, _ := io.ReadAll(res.Body); len(rb) != 0 {
		t.Errorf("body = %q, want empty (no fragment)", rb)
	}
}

// TestShareStatusRoutesToHolesail locks that /api/share/* proxies to the holesail
// sidecar, never the backend.
func TestShareStatusRoutesToHolesail(t *testing.T) {
	backendHit := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
	}))
	defer backend.Close()
	holesail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, stubStatusBody)
	}))
	defer holesail.Close()

	mux := newMuxServer(t, backend.URL, holesail.URL)
	req := httptest.NewRequest("GET", "/api/share/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if backendHit {
		t.Errorf("share route hit the backend; must go to holesail")
	}
	if rb, _ := io.ReadAll(res.Body); string(rb) != stubStatusBody {
		t.Errorf("body = %q, want holesail status %q", rb, stubStatusBody)
	}
}

// TestShareUnknownMethodStaysOnHolesail locks the method-blind share prefix:
// a method the share API doesn't define (PUT) must still route to the sidecar
// (which 404s), never fall through to the backend catch-all.
func TestShareUnknownMethodStaysOnHolesail(t *testing.T) {
	backendHit := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
	}))
	defer backend.Close()
	holesailStatus := 0
	holesail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holesailStatus = http.StatusNotFound
		w.WriteHeader(http.StatusNotFound)
	}))
	defer holesail.Close()

	mux := newMuxServer(t, backend.URL, holesail.URL)
	req := httptest.NewRequest("PUT", "/api/share/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if backendHit {
		t.Error("PUT /api/share/status reached the backend; must stay on the share proxy")
	}
	if holesailStatus != http.StatusNotFound || rec.Code != http.StatusNotFound {
		t.Errorf("expected sidecar 404 passthrough, got %d (sidecar saw %d)", rec.Code, holesailStatus)
	}
}

// TestLogsRoutesToLogdNotBackend locks that both the bare /api/logs query path
// and the /api/logs/stream path route to the logd sidecar, never falling
// through to the /api/ backend catch-all (which would bypass the guard).
func TestLogsRoutesToLogdNotBackend(t *testing.T) {
	for _, path := range []string{"/api/logs?service=backend", "/api/logs/stream"} {
		t.Run(path, func(t *testing.T) {
			backendHit := false
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				backendHit = true
			}))
			defer backend.Close()
			var logdPath string
			logd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				logdPath = r.URL.Path
				io.WriteString(w, "LOGD-BODY")
			}))
			defer logd.Close()

			mux := newMuxServerFull(t, backend.URL, noHolesail, logd.URL)
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if backendHit {
				t.Errorf("%s reached the backend; must go to logd", path)
			}
			if logdPath == "" {
				t.Errorf("%s did not reach logd", path)
			}
			if rb, _ := io.ReadAll(rec.Result().Body); string(rb) != "LOGD-BODY" {
				t.Errorf("body = %q, want LOGD-BODY", rb)
			}
		})
	}
}

// TestHealthzServedLocally locks that GET /healthz is served by the frontend
// itself (200), NOT proxied to backend — so it reports frontend liveness and
// can't cascade when backend is down.
func TestHealthzServedLocally(t *testing.T) {
	backendHit := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
	}))
	defer backend.Close()

	mux := newMuxServer(t, backend.URL, noHolesail)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if backendHit {
		t.Error("/healthz reached the backend; must be served locally by the frontend")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rb, _ := io.ReadAll(rec.Result().Body); !strings.Contains(string(rb), `"status":"ok"`) {
		t.Errorf("body = %q, want frontend's own ok", rb)
	}
}

// TestStatusRoutesToLogdGuarded locks that /api/status (bare and slashed) is
// proxied to logd, not the backend catch-all, and 403'd for tunnel visitors.
func TestStatusRoutesToLogdGuarded(t *testing.T) {
	for _, path := range []string{"/api/status", "/api/status/stream"} {
		t.Run("lan "+path, func(t *testing.T) {
			backendHit := false
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { backendHit = true }))
			defer backend.Close()
			logdHit := false
			logd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { logdHit = true }))
			defer logd.Close()

			mux := newMuxServerFull(t, backend.URL, noHolesail, logd.URL)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
			if backendHit || !logdHit {
				t.Errorf("%s: backendHit=%v logdHit=%v, want logd only", path, backendHit, logdHit)
			}
		})
	}
	t.Run("tunnel 403", func(t *testing.T) {
		logdHit := false
		logd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { logdHit = true }))
		defer logd.Close()
		mux := newMuxServerFull(t, "http://backend.invalid:8081", noHolesail, logd.URL)
		rec := httptest.NewRecorder()
		markTunnel(mux).ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
		if rec.Code != http.StatusForbidden || logdHit {
			t.Errorf("tunnel GET /api/status: code=%d logdHit=%v, want 403 and no logd", rec.Code, logdHit)
		}
	})
}

// TestLogsTunnelForbiddenAllMethods locks that a tunnel-originated request to a
// logs route is 403'd for every method — including GET, stricter than share —
// and never reaches logd.
func TestLogsTunnelForbiddenAllMethods(t *testing.T) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			logdHit := false
			logd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				logdHit = true
			}))
			defer logd.Close()

			mux := newMuxServerFull(t, "http://backend.invalid:8081", noHolesail, logd.URL)
			// markTunnel stamps every request as tunnel-originated, mirroring the
			// dedicated tunnel listener in main.go.
			handler := markTunnel(mux)
			req := httptest.NewRequest(method, "/api/logs", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("%s over tunnel: status = %d, want 403", method, rec.Code)
			}
			if logdHit {
				t.Errorf("%s over tunnel reached logd; must be blocked", method)
			}
		})
	}
}

// TestLogsLANRequestProxied locks that a non-tunnel GET reaches logd verbatim.
func TestLogsLANRequestProxied(t *testing.T) {
	var gotQuery string
	logd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		io.WriteString(w, "OK")
	}))
	defer logd.Close()

	mux := newMuxServerFull(t, "http://backend.invalid:8081", noHolesail, logd.URL)
	req := httptest.NewRequest("GET", "/api/logs?service=backend&q=timeout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotQuery != "service=backend&q=timeout" {
		t.Errorf("logd query = %q, want service=backend&q=timeout", gotQuery)
	}
}

// TestSSEProxyForwardsEventStream locks that /events streams from the backend
// with the event-stream content type.
func TestSSEProxyForwardsEventStream(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "event: update\ndata: \n\n")
	}))
	defer backend.Close()

	mux := newMuxServer(t, backend.URL, noHolesail)
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if rb, _ := io.ReadAll(res.Body); !strings.Contains(string(rb), "event: update") {
		t.Errorf("body = %q, want the forwarded SSE event", rb)
	}
}
