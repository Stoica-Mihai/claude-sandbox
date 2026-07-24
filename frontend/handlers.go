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

// maxProxyJSONBody caps a JSON body the frontend buffers before forwarding it
// to the backend (which enforces the same, authoritative limit). Shared via
// shared/limits so the proxy bound and the backend cap can't drift.
const maxProxyJSONBody = api.MaxJSONBody

// Server is the HTTP server serving the dashboard frontend.
// It renders HTML templates with data fetched from the backend API,
// and proxies WebSocket, SSE, and healthz requests to the backend.
type Server struct {
	templates     *template.Template
	backendURL    string
	client        *http.Client
	backendProxy  http.Handler // verbatim reverse proxy for passthrough /api routes + SSE
	holesailProxy http.Handler // verbatim reverse proxy for /api/share/* (guarded)
	logdProxy     http.Handler // verbatim reverse proxy for /api/logs* (guarded, SSE)
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
func NewServer(backendURL, holesailURL, logdURL string, mux *http.ServeMux) (*Server, error) {
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
	logdProxy, err := newReverseProxy(logdURL, logdDownBody())
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
		logdProxy:     logdProxy,
	}

	// Transform routes: translate the request or render HTML — not verbatim
	// proxies. Registered with specific patterns, so ServeMux precedence gives
	// them priority over the /api/ catch-all below.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /logs", s.handleLogs)
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

	// Log + status routes proxied to the logd sidecar (guarded). Both the bare
	// path and the prefix are registered for each: the prefix pattern does not
	// match the bare path, so without the exact route "/api/logs" (or
	// "/api/status") would fall through to the /api/ backend catch-all and
	// bypass the tunnel guard.
	mux.HandleFunc(api.RouteLogs, s.handleGuardedLogd)
	mux.HandleFunc(api.RouteLogs+"/", s.handleGuardedLogd)
	mux.HandleFunc(api.RouteStatus, s.handleGuardedLogd)
	mux.HandleFunc(api.RouteStatus+"/", s.handleGuardedLogd)

	// Streaming proxy routes. SSE goes through the reverse proxy, which
	// auto-flushes text/event-stream responses.
	mux.Handle("GET "+api.RouteEvents, s.backendProxy)
	mux.HandleFunc("GET "+api.RouteWSTerminal, s.handleWebSocketProxy)

	// Catch-all: every other /api/ path (settings, ui-prefs, session history,
	// upload, create-directory POST) is forwarded verbatim to the backend. The
	// specific routes above win by precedence, so only genuine passthrough
	// requests reach the proxy.
	mux.Handle("/api/", s.backendProxy)
	// Frontend's own shallow liveness — served locally, NOT proxied to backend,
	// so it reports the frontend's health (not backend's) and can't cascade.
	mux.HandleFunc("GET "+api.RouteHealthz, s.handleHealthz)

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
		// sessionKindsJSON injects the spawn/resume kind allowlist
		// (window.SESSION_KINDS) so the NEW SESSION modal's mode choice and the
		// resume list render from the shared Go contract, not re-typed literals.
		"sessionKindsJSON": func() (template.JS, error) {
			b, err := json.Marshal(api.SessionKindValues())
			return template.JS(b), err
		},
		"sseEvent": func() string { return api.SSEEventUpdate },
		// Route helpers so template hx-*/sse-connect attributes render from
		// shared/routes.go instead of re-typed literals.
		"routeEvents":      func() string { return api.RouteEvents },
		"routeSessions":    func() string { return api.RouteSessions },
		"routeDirectories": func() string { return api.RouteDirectories },
		"sessionPath":      api.SessionPath,
		// routesJSON injects the route patterns the browser JS builds URLs from
		// (window.ROUTES), so a route rename in shared/routes.go flows to the JS.
		"routesJSON": func() (template.JS, error) {
			b, err := json.Marshal(map[string]string{
				"sessions":          api.RouteSessions,
				"session":           api.RouteSession,
				"settings":          api.RouteSettings,
				"uiPrefs":           api.RouteUIPrefs,
				"directories":       api.RouteDirectories,
				"sessionsHistory":   api.RouteSessionsHistory,
				"sessionName":       api.RouteSessionName,
				"sessionUpload":     api.RouteSessionUpload,
				"historyItem":       api.RouteHistoryItem,
				"wsTerminal":        api.RouteWSTerminal,
				"sessionTranscript": api.RouteSessionTranscript,
				"sessionMode":       api.RouteSessionMode,
				"logs":              api.RouteLogs,
				"logsStream":        api.RouteLogsStream,
				"status":            api.RouteStatus,
				"statusStream":      api.RouteStatusStream,
			})
			return template.JS(b), err
		},
		// shareStateJSON injects the share-tunnel state vocabulary
		// (window.SHARE_STATE) so share.js branches on the shared Go contract
		// instead of re-typed literals.
		"shareStateJSON": func() (template.JS, error) {
			b, err := json.Marshal(map[string]string{
				"private":    string(api.SharePrivate),
				"publishing": string(api.SharePublishing),
				"public":     string(api.SharePublic),
				"error":      string(api.ShareError),
			})
			return template.JS(b), err
		},
		// wsControlJSON injects the WS control vocabulary (window.WS_CONTROL) so
		// the browser JS speaks the same control protocol as sessiond/protocol
		// without re-typing the literals.
		"wsControlJSON": func() (template.JS, error) {
			b, err := json.Marshal(map[string]string{
				"resize":      api.WSControlResize,
				"deactivated": api.WSControlDeactivated,
				"reactivate":  api.WSControlReactivate,
				"error":       api.WSControlError,
			})
			return template.JS(b), err
		},
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

// requirePathValue returns a required path parameter, writing a 400 (plain
// text — these transform routes are not JSON) and returning ok=false when it is
// empty. Mirrors the backend helper of the same name.
func requirePathValue(w http.ResponseWriter, r *http.Request, key, missingMsg string) (string, bool) {
	v := r.PathValue(key)
	if v == "" {
		http.Error(w, missingMsg, http.StatusBadRequest)
		return "", false
	}
	return v, true
}

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

// logdDownBody is the 502 JSON envelope for an unreachable logd sidecar.
func logdDownBody() []byte {
	b, _ := json.Marshal(map[string]string{"error": "log service unreachable"})
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

// handleLogs renders the dashboard shell with the logs surface active. The
// session list is fetched best-effort only to keep the header session count
// accurate — the logs view itself depends on logd, not the backend, so a
// backend failure degrades to a zero count rather than a 500.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.fetchSessions(r)
	if err != nil {
		slog.Debug("logs page: session count unavailable", "error", err)
		sessions = nil
	}
	s.renderTemplate(w, "layout.html", DashboardData{Sessions: sessions, Logs: true})
}

// handleSessionsFragment renders the sessions list HTML fragment for HTMX.
func (s *Server) handleSessionsFragment(w http.ResponseWriter, r *http.Request) {
	s.renderSessions(w, r, "sessions")
}

// handleSpawn forwards the spawn request to the backend (as JSON), then
// fetches the updated session list and renders the sessions fragment.
// The form sends cwd as a form field; we convert it to JSON for the backend.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	// The HTMX form sends application/x-www-form-urlencoded with "cwd", an
	// optional "resume" (conversation uuid), and a "kind" (terminal|chat;
	// empty defaults to terminal on the backend) field.
	cwd := r.FormValue("cwd")
	resume := r.FormValue("resume")
	kind := r.FormValue("kind")
	if cwd == "" && resume == "" {
		http.Error(w, "missing cwd parameter", http.StatusBadRequest)
		return
	}

	// Forward as JSON to backend.
	payload, _ := json.Marshal(api.SpawnRequest{CWD: cwd, Resume: resume, Kind: api.SessionKind(kind)})
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
	terminalId, ok := requirePathValue(w, r, "terminalId", "missing session name")
	if !ok {
		return
	}

	s.proxyThenRenderSessions(w, r, "DELETE", api.SessionPath(terminalId), nil, "failed to kill session via backend")
}

// handleDeleteHistoryProxy forwards a history-delete to the backend and passes
// its status through (the dashboard JS re-renders the resume list on 204; the
// backend's SSE refreshes the sidebar). Keyed by conversation uuid, distinct
// from handleKill's terminalId route.
func (s *Server) handleDeleteHistoryProxy(w http.ResponseWriter, r *http.Request) {
	uuid, ok := requirePathValue(w, r, "uuid", "missing uuid")
	if !ok {
		return
	}

	resp, err := s.backendRequest(r.Context(), "DELETE", api.HistoryItemPath(uuid), nil)
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
	terminalId, ok := requirePathValue(w, r, "terminalId", "missing session name")
	if !ok {
		return
	}

	// Read the JSON body and forward it to the backend. Cap it so the proxy
	// never buffers an unbounded body before the backend's own limit applies.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxProxyJSONBody))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	s.proxyThenRenderSessions(w, r, "PUT", api.SessionNamePath(terminalId), bytes.NewReader(body), "failed to rename session via backend")
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

// handleGuardedLogd proxies /api/logs* and /api/status* to the logd sidecar.
// Both may expose secrets or fleet topology, so — stricter than the share
// guard, which allows GET — every method including GET is rejected for a
// tunnel-originated request. logd is otherwise unauthenticated; the boundary is
// this guard plus the external auth proxy.
func (s *Server) handleGuardedLogd(w http.ResponseWriter, r *http.Request) {
	if isTunnelRequest(r) {
		http.Error(w, "not available over the tunnel", http.StatusForbidden)
		return
	}
	s.logdProxy.ServeHTTP(w, r)
}

// handleHealthz is the frontend's own shallow liveness endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
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
