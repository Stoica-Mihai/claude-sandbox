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
	"net/http"
	"strings"
	"time"

	"claude-frontend/web"
	api "claude-sandbox-api"
)

// Server is the HTTP server serving the dashboard frontend.
// It renders HTML templates with data fetched from the backend API,
// and proxies WebSocket, SSE, and healthz requests to the backend.
type Server struct {
	templates   *template.Template
	backendURL  string
	holesailURL string
	guard       *tunnelGuard
	client      *http.Client
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

	s := &Server{
		templates:   tmpl,
		backendURL:  backendURL,
		holesailURL: holesailURL,
		guard:       newTunnelGuard(holesailURL),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Template-rendering routes (fetch JSON from backend, render HTML).
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /fragments/sessions", s.handleSessionsFragment)
	mux.HandleFunc("POST /api/sessions", s.handleSpawn)
	mux.HandleFunc("DELETE /api/sessions/{terminalId}", s.handleKill)
	mux.HandleFunc("DELETE /api/sessions/history/{uuid}", s.handleDeleteHistoryProxy)
	mux.HandleFunc("PUT /api/sessions/{terminalId}/name", s.handleSetSessionName)
	mux.HandleFunc("POST /api/sessions/{terminalId}/upload", s.handleUploadProxy)
	mux.HandleFunc("GET /api/directories", s.handleDirectories)
	mux.HandleFunc("POST /api/directories", s.handleCreateDirectoryProxy)
	mux.HandleFunc("GET /api/sessions/history", s.handleHistoryProxy)
	mux.HandleFunc("GET /api/settings", s.handleSettingsProxy)
	mux.HandleFunc("PUT /api/settings", s.handleSettingsProxy)
	mux.HandleFunc("GET /api/ui-prefs", s.handleUIPrefsProxy)
	mux.HandleFunc("PUT /api/ui-prefs", s.handleUIPrefsProxy)
	mux.HandleFunc("GET /api/share/status", s.handleShareProxy)
	mux.HandleFunc("POST /api/share/start", s.handleShareProxy)
	mux.HandleFunc("POST /api/share/stop", s.handleShareProxy)
	mux.HandleFunc("POST /api/share/regenerate", s.handleShareProxy)

	// Pure proxy routes.
	mux.HandleFunc("GET /events", s.handleSSEProxy)
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocketProxy)
	mux.HandleFunc("GET /healthz", s.handleHealthzProxy)

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

// forwardError writes the backend's error status and body to the client.
func forwardError(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
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
	payload, _ := json.Marshal(map[string]string{"cwd": cwd, "resume": resume})
	resp, err := s.backendRequest(r.Context(), "POST", "/api/sessions", bytes.NewReader(payload))
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
	var spawnResp struct {
		SessionName string `json:"session_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spawnResp); err != nil {
		slog.Error("failed to decode spawn response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set the X-Terminal-Id header for the client JS to pick up.
	w.Header().Set("X-Terminal-Id", spawnResp.SessionName)

	// Fetch updated sessions and render the fragment.
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

	resp, err := s.backendRequest(r.Context(), "DELETE", "/api/sessions/"+terminalId, nil)
	if err != nil {
		badGateway(w, "failed to kill session via backend", err)
		return
	}
	defer resp.Body.Close()

	if forwardIfError(w, resp) {
		return
	}

	// Render updated sessions fragment.
	s.handleSessionsFragment(w, r)
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

	resp, err := s.backendRequest(r.Context(), "DELETE", "/api/sessions/history/"+uuid, nil)
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

	resp, err := s.backendRequest(r.Context(), "PUT", "/api/sessions/"+terminalId+"/name", bytes.NewReader(body))
	if err != nil {
		badGateway(w, "failed to rename session via backend", err)
		return
	}
	defer resp.Body.Close()

	if forwardIfError(w, resp) {
		return
	}

	// Render updated sessions fragment.
	s.handleSessionsFragment(w, r)
}

// handleDirectories fetches directory data from the backend and renders the
// directory-picker template.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	path := "/api/directories"
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

// handleHistoryProxy proxies GET /api/sessions/history (JSON) to the backend.
// Pure passthrough, so it uses the generic httpProxy like settings/upload/healthz.
func (s *Server) handleHistoryProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// handleCreateDirectoryProxy proxies POST /api/directories (JSON) to the
// backend. The response is consumed as JSON by views.js, never rendered, so it
// passes the backend's status and body through verbatim via the generic
// httpProxy rather than re-decoding like the GET template-render route.
func (s *Server) handleCreateDirectoryProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// --- Pure proxy routes ---

// handleSSEProxy proxies the SSE event stream from the backend.
func (s *Server) handleSSEProxy(w http.ResponseWriter, r *http.Request) {
	sseProxy(w, r, s.backendURL)
}

// handleWebSocketProxy proxies a WebSocket connection to the backend.
func (s *Server) handleWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	wsProxy(w, r, s.backendURL)
}

// handleUploadProxy proxies image uploads to the backend.
func (s *Server) handleUploadProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// handleHealthzProxy proxies the health check to the backend.
func (s *Server) handleHealthzProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// handleSettingsProxy proxies GET/PUT /api/settings (JSON) to the backend.
func (s *Server) handleSettingsProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// handleUIPrefsProxy proxies GET/PUT /api/ui-prefs (JSON) to the backend.
func (s *Server) handleUIPrefsProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy(w, r, s.backendURL)
}

// handleShareProxy proxies /api/share/* (JSON) to the holesail sidecar, which
// serves the same paths. Mutating actions (start/stop/regenerate) arriving
// through the tunnel itself are rejected so tunnel visitors cannot operate the
// controls; the read-only status GET is always allowed, so a client browsing
// over the tunnel still sees the (necessarily public) sharing state.
func (s *Server) handleShareProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && s.guard.isTunnelRequest(r) {
		// Same {state,url,error} shape the sidecar returns, so share.js's
		// renderShare surfaces the message instead of dropping it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"state":"error","url":null,"error":"Share controls are not available over the tunnel."}`)
		return
	}
	httpProxy(w, r, s.holesailURL)
}

// --- Helpers ---

// fetchSessions calls the backend's GET /api/sessions and returns the parsed list.
func (s *Server) fetchSessions(r *http.Request) ([]api.DisplaySession, error) {
	resp, err := s.backendRequest(r.Context(), "GET", "/api/sessions", nil)
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
