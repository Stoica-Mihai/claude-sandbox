package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"claude-frontend/web"
	api "claude-sandbox-api"
)

// Server is the HTTP server serving the dashboard frontend.
// It renders HTML templates with data fetched from the backend API,
// and proxies WebSocket, SSE, and healthz requests to the backend.
type Server struct {
	templates     *template.Template
	backendURL    string
	client        *http.Client
	backendProxy  http.Handler // verbatim reverse proxy for passthrough /api routes + SSE
	holesailProxy http.Handler // verbatim reverse proxy for /api/share/* (guarded)
}

// newReverseProxy builds a reverse proxy that forwards verbatim to the target,
// with bounded dial + response-header timeouts so a hung upstream can't wedge a
// request goroutine. SSE responses (text/event-stream) flush immediately —
// httputil.ReverseProxy detects the content type itself. An unreachable
// upstream answers 502 with errBody (JSON), not the default empty body, so
// clients that parse error envelopes get a real message.
func newReverseProxy(targetURL string, errBody []byte) (http.Handler, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parsing proxy target URL: %w", err)
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("proxy upstream error", "target", targetURL, "path", r.URL.Path, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(errBody)
	}
	return rp, nil
}

// NewServer creates a Server by parsing embedded templates and registering
// all routes on the provided mux.
func NewServer(backendURL, holesailURL string, mux *http.ServeMux) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, fmt.Errorf("creating static sub-FS: %w", err)
	}

	backendProxy, err := newReverseProxy(backendURL, backendDownBody())
	if err != nil {
		return nil, err
	}
	holesailProxy, err := newReverseProxy(holesailURL, shareDownBody())
	if err != nil {
		return nil, err
	}

	s := &Server{
		templates:  tmpl,
		backendURL: backendURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		backendProxy:  backendProxy,
		holesailProxy: holesailProxy,
	}

	// Transform routes: translate the request or render HTML — not verbatim
	// proxies. Registered with specific patterns, so ServeMux precedence gives
	// them priority over the /api/ catch-all below.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /fragments/sessions", s.handleSessionsFragment)
	mux.HandleFunc("POST "+api.RouteSessions, s.handleSpawn)
	mux.HandleFunc("DELETE "+api.RouteSession, s.handleKill)
	mux.HandleFunc("DELETE "+api.RouteHistoryItem, s.handleDeleteHistoryProxy)
	mux.HandleFunc("PUT "+api.RouteSessionName, s.handleSetSessionName)
	mux.HandleFunc("GET "+api.RouteDirectories, s.handleDirectories)

	// Share-tunnel routes proxied to the holesail sidecar (guarded). Registered
	// method-blind on the whole prefix so no method/path variant can fall
	// through to the /api/ backend catch-all; the sidecar 404s unknown routes.
	mux.HandleFunc("/api/share/", s.handleShareProxy)

	// Streaming proxy routes. SSE goes through the reverse proxy, which
	// auto-flushes text/event-stream responses.
	mux.Handle("GET "+api.RouteEvents, s.backendProxy)
	mux.HandleFunc("GET "+api.RouteWSTerminal, s.handleWebSocketProxy)

	// Catch-all: every other /api/ path (settings, ui-prefs, session history,
	// upload, create-directory POST) and healthz is forwarded verbatim to the
	// backend. The specific routes above win by precedence, so only genuine
	// passthrough requests reach the proxy.
	mux.Handle("/api/", s.backendProxy)
	mux.Handle("GET /healthz", s.backendProxy)

	// Static files. embed.FS files carry no modtime, so http.FileServer alone
	// sends no ETag/Last-Modified and browsers re-download every asset on every
	// load (~500KB of xterm+htmx+CSS). Hash each file once at startup and serve
	// conditional 304s; vendor libs change only on image rebuild, so they also
	// get a day of freshness.
	etags, err := computeETags(staticFS)
	if err != nil {
		return nil, fmt.Errorf("hashing static files: %w", err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	mux.Handle("GET /static/", cacheMiddleware(etags, fileServer))

	return s, nil
}

// templateFuncs exposes the shared dashboard enums to templates so the settings
// modal and accent picker render from the same source the backend validates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"models":   func() []api.Option { return api.Models },
		"efforts":  func() []api.Option { return api.EffortLevels },
		"advisors": func() []api.Option { return api.AdvisorModels },
		"accentsJSON": func() (template.JS, error) {
			b, err := json.Marshal(api.Accents)
			return template.JS(b), err
		},
		// sessionsJSON embeds the session list as the fragment's data payload
		// (the client store's source). json.Marshal escapes <,>,& so the output
		// is safe inside the <script type="application/json"> block.
		"sessionsJSON": func(sessions []api.DisplaySession) (template.JS, error) {
			if sessions == nil {
				return template.JS("[]"), nil
			}
			b, err := json.Marshal(sessions)
			return template.JS(b), err
		},
		"newProjectPattern": func() string { return api.NewProjectNamePattern },
	}
}

// parseTemplates parses the embedded templates with the shared FuncMap. Shared
// by NewServer and the template tests so both parse identically.
func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseFS(web.Templates, "templates/*.html", "templates/fragments/*.html")
}

// computeETags hashes every file in the static FS into a path → ETag map.
func computeETags(fsys fs.FS) (map[string]string, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		etags[path] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})
	return etags, err
}

// cacheMiddleware adds ETag/Cache-Control to static responses and answers
// If-None-Match with 304, so unchanged assets cost a header round-trip
// instead of a full transfer.
func cacheMiddleware(etags map[string]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/static/")
		etag, ok := etags[path]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Shared helpers ---

// renderTemplate executes a named template into a buffer and writes the result.
func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("template render failed", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// backendRequest constructs and executes an HTTP request to the backend API.
func (s *Server) backendRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.backendURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

// backendDownBody is the 502 JSON envelope for an unreachable backend.
func backendDownBody() []byte {
	b, _ := json.Marshal(map[string]string{"error": "backend connection failed"})
	return b
}

// shareDownBody is the 502 ShareStatus envelope for an unreachable sidecar,
// so share.js's renderShare surfaces the message instead of a parse failure.
func shareDownBody() []byte {
	msg := "Share sidecar unreachable."
	b, _ := json.Marshal(api.ShareStatus{State: api.ShareError, Error: &msg})
	return b
}

// forwardError writes the backend's error status, Content-Type, and body.
func forwardError(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// badGateway logs a failed backend call and replies 502.
func badGateway(w http.ResponseWriter, logMsg string, err error) {
	slog.Error(logMsg, "error", err)
	http.Error(w, "backend connection failed", http.StatusBadGateway)
}

// forwardIfError forwards a backend error response (status >= 400) to the
// client and reports whether it did, so callers can return early.
func forwardIfError(w http.ResponseWriter, resp *http.Response) bool {
	if resp.StatusCode >= 400 {
		forwardError(w, resp)
		return true
	}
	return false
}

// renderSessions fetches the session list and renders it into the named
// template, replying 500 on a backend failure.
func (s *Server) renderSessions(w http.ResponseWriter, r *http.Request, name string) {
	sessions, err := s.fetchSessions(r)
	if err != nil {
		slog.Error("failed to fetch sessions from backend", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	s.renderTemplate(w, name, DashboardData{Sessions: sessions})
}

// --- Template-rendering routes ---

// handleIndex renders the full dashboard page with initial session data.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderSessions(w, r, "layout.html")
}

// handleSessionsFragment renders the sessions list HTML fragment for HTMX.
func (s *Server) handleSessionsFragment(w http.ResponseWriter, r *http.Request) {
	s.renderSessions(w, r, "sessions")
}

// handleSpawn forwards the spawn request to the backend (as JSON), then
// fetches the updated session list and renders the sessions fragment.
// The form sends cwd as a form field; we convert it to JSON for the backend.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	// The HTMX form sends application/x-www-form-urlencoded with "cwd" and an
	// optional "resume" (conversation uuid) field.
	cwd := r.FormValue("cwd")
	resume := r.FormValue("resume")
	if cwd == "" && resume == "" {
		http.Error(w, "missing cwd parameter", http.StatusBadRequest)
		return
	}

	// Forward as JSON to backend.
	payload, _ := json.Marshal(api.SpawnRequest{CWD: cwd, Resume: resume})
	resp, err := s.backendRequest(r.Context(), "POST", api.RouteSessions, bytes.NewReader(payload))
	if err != nil {
		badGateway(w, "failed to spawn session via backend", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Error("backend spawn failed", "status", resp.StatusCode)
		forwardError(w, resp)
		return
	}

	// Parse the backend response to get the session name.
	var spawnResp api.SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&spawnResp); err != nil {
		slog.Error("failed to decode spawn response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set the terminal-id header for the client JS to pick up.
	w.Header().Set(api.HeaderTerminalID, spawnResp.SessionName)

	// Fetch updated sessions and render the fragment.
	s.handleSessionsFragment(w, r)
}

// proxyThenRenderSessions forwards a mutating request to the backend and, on
// success, renders the updated sessions fragment. Shared by kill and rename,
// which differ only in method, path, body, and failure log message.
func (s *Server) proxyThenRenderSessions(w http.ResponseWriter, r *http.Request, method, path string, body io.Reader, failMsg string) {
	resp, err := s.backendRequest(r.Context(), method, path, body)
	if err != nil {
		badGateway(w, failMsg, err)
		return
	}
	defer resp.Body.Close()

	if forwardIfError(w, resp) {
		return
	}

	s.handleSessionsFragment(w, r)
}

// handleKill forwards the kill request to the backend, then renders the
// updated sessions fragment.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	terminalId := r.PathValue("terminalId")
	if terminalId == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	s.proxyThenRenderSessions(w, r, "DELETE", api.RouteSessions+"/"+terminalId, nil, "failed to kill session via backend")
}

// handleDeleteHistoryProxy forwards a history-delete to the backend and passes
// its status through (the dashboard JS re-renders the resume list on 204; the
// backend's SSE refreshes the sidebar). Keyed by conversation uuid, distinct
// from handleKill's terminalId route.
func (s *Server) handleDeleteHistoryProxy(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		http.Error(w, "missing uuid", http.StatusBadRequest)
		return
	}

	resp, err := s.backendRequest(r.Context(), "DELETE", api.RouteSessionsHistory+"/"+uuid, nil)
	if err != nil {
		badGateway(w, "failed to delete history via backend", err)
		return
	}
	defer resp.Body.Close()

	if forwardIfError(w, resp) {
		return
	}

	w.WriteHeader(resp.StatusCode)
}

// handleSetSessionName forwards the rename request to the backend, then
// renders the updated sessions fragment.
func (s *Server) handleSetSessionName(w http.ResponseWriter, r *http.Request) {
	terminalId := r.PathValue("terminalId")
	if terminalId == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	// Read the JSON body and forward it to the backend.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	s.proxyThenRenderSessions(w, r, "PUT", api.RouteSessions+"/"+terminalId+"/name", bytes.NewReader(body), "failed to rename session via backend")
}

// handleDirectories fetches directory data from the backend and renders the
// directory-picker template.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	path := api.RouteDirectories
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}

	resp, err := s.backendRequest(r.Context(), "GET", path, nil)
	if err != nil {
		badGateway(w, "failed to fetch directories from backend", err)
		return
	}
	defer resp.Body.Close()

	if forwardIfError(w, resp) {
		return
	}

	var dirData api.DirectoryData
	if err := json.NewDecoder(resp.Body).Decode(&dirData); err != nil {
		slog.Error("failed to decode directory response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "directory-picker", dirData)
}

// --- Streaming and share proxy routes ---

// handleWebSocketProxy proxies a WebSocket connection to the backend.
func (s *Server) handleWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	wsProxy(w, r, s.backendURL)
}

// handleShareProxy proxies /api/share/* (JSON) to the holesail sidecar, which
// serves the same paths. Mutating actions (start/stop/regenerate) arriving
// through the tunnel itself are rejected so tunnel visitors cannot operate the
// controls; the read-only status GET is always allowed, so a client browsing
// over the tunnel still sees the (necessarily public) sharing state.
func (s *Server) handleShareProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && isTunnelRequest(r) {
		// Same ShareStatus shape the sidecar returns, so share.js's
		// renderShare surfaces the message instead of dropping it.
		msg := "Share controls are not available over the tunnel."
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(api.ShareStatus{State: api.ShareError, Error: &msg})
		return
	}
	s.holesailProxy.ServeHTTP(w, r)
}

// --- Helpers ---

// fetchSessions calls the backend's GET /api/sessions and returns the parsed list.
func (s *Server) fetchSessions(r *http.Request) ([]api.DisplaySession, error) {
	resp, err := s.backendRequest(r.Context(), "GET", api.RouteSessions, nil)
	if err != nil {
		return nil, fmt.Errorf("backend request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("backend returned %d: %s", resp.StatusCode, string(body))
	}

	var sessions []api.DisplaySession
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}

	return sessions, nil
}
