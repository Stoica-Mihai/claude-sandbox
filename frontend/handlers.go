package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"claude-frontend/web"
)

// Server is the HTTP server serving the dashboard frontend.
// It renders HTML templates with data fetched from the backend API,
// and proxies WebSocket, SSE, and healthz requests to the backend.
type Server struct {
	templates  *template.Template
	backendURL string
	client     *http.Client
	mux        *http.ServeMux
}

// NewServer creates a Server by parsing embedded templates and registering
// all routes on the provided mux.
func NewServer(backendURL string, mux *http.ServeMux) (*Server, error) {
	tmpl, err := template.ParseFS(web.Templates, "templates/*.html", "templates/fragments/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, fmt.Errorf("creating static sub-FS: %w", err)
	}

	s := &Server{
		templates:  tmpl,
		backendURL: backendURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		mux: mux,
	}

	// Template-rendering routes (fetch JSON from backend, render HTML).
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /fragments/sessions", s.handleSessionsFragment)
	mux.HandleFunc("POST /api/sessions", s.handleSpawn)
	mux.HandleFunc("DELETE /api/sessions/{terminalId}", s.handleKill)
	mux.HandleFunc("PUT /api/sessions/{terminalId}/name", s.handleSetSessionName)
	mux.HandleFunc("POST /api/sessions/{terminalId}/upload", s.handleUploadProxy)
	mux.HandleFunc("GET /api/directories", s.handleDirectories)

	// Pure proxy routes.
	mux.HandleFunc("GET /events", s.handleSSEProxy)
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocketProxy)
	mux.HandleFunc("GET /healthz", s.handleHealthzProxy)

	// Static files.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	return s, nil
}

// --- Template-rendering routes ---

// handleIndex renders the full dashboard page with initial session data.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.fetchSessions(r)
	if err != nil {
		slog.Error("failed to fetch sessions from backend", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := DashboardData{Sessions: sessions}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		slog.Error("failed to render dashboard", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// handleSessionsFragment renders the sessions list HTML fragment for HTMX.
func (s *Server) handleSessionsFragment(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.fetchSessions(r)
	if err != nil {
		slog.Error("failed to fetch sessions from backend", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := DashboardData{Sessions: sessions}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "sessions", data); err != nil {
		slog.Error("failed to render sessions fragment", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// handleSpawn forwards the spawn request to the backend (as JSON), then
// fetches the updated session list and renders the sessions fragment.
// The form sends cwd as a form field; we convert it to JSON for the backend.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	// The HTMX form sends application/x-www-form-urlencoded with "cwd" field.
	cwd := r.FormValue("cwd")
	if cwd == "" {
		http.Error(w, "missing cwd parameter", http.StatusBadRequest)
		return
	}

	// Forward as JSON to backend.
	payload, _ := json.Marshal(map[string]string{"cwd": cwd})
	backendReq, err := http.NewRequestWithContext(r.Context(), "POST",
		s.backendURL+"/api/sessions", bytes.NewReader(payload))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	backendReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(backendReq)
	if err != nil {
		slog.Error("failed to spawn session via backend", "error", err)
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("backend spawn failed", "status", resp.StatusCode, "body", string(body))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
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

	backendReq, err := http.NewRequestWithContext(r.Context(), "DELETE",
		s.backendURL+"/api/sessions/"+terminalId, nil)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp, err := s.client.Do(backendReq)
	if err != nil {
		slog.Error("failed to kill session via backend", "error", err)
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Render updated sessions fragment.
	s.handleSessionsFragment(w, r)
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

	backendReq, err := http.NewRequestWithContext(r.Context(), "PUT",
		s.backendURL+"/api/sessions/"+terminalId+"/name", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	backendReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(backendReq)
	if err != nil {
		slog.Error("failed to rename session via backend", "error", err)
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Render updated sessions fragment.
	s.handleSessionsFragment(w, r)
}

// handleDirectories fetches directory data from the backend and renders the
// directory-picker template.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	targetURL := s.backendURL + "/api/directories"
	if q := r.URL.RawQuery; q != "" {
		targetURL += "?" + q
	}

	backendReq, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp, err := s.client.Do(backendReq)
	if err != nil {
		slog.Error("failed to fetch directories from backend", "error", err)
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, string(body), resp.StatusCode)
		return
	}

	var dirData DirectoryData
	if err := json.NewDecoder(resp.Body).Decode(&dirData); err != nil {
		slog.Error("failed to decode directory response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "directory-picker", dirData); err != nil {
		slog.Error("failed to render directory picker", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
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

// --- Helpers ---

// fetchSessions calls the backend's GET /api/sessions and returns the parsed list.
func (s *Server) fetchSessions(r *http.Request) ([]DisplaySession, error) {
	backendReq, err := http.NewRequestWithContext(r.Context(), "GET",
		s.backendURL+"/api/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(backendReq)
	if err != nil {
		return nil, fmt.Errorf("backend request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("backend returned %d: %s", resp.StatusCode, string(body))
	}

	var sessions []DisplaySession
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}

	return sessions, nil
}
