package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"claude-dashboard/web"

	"github.com/gorilla/websocket"
)

// DashboardData holds the data passed to the full layout template.
type DashboardData struct {
	Sessions []DisplaySession
}

// Breadcrumb represents a path segment in the directory picker breadcrumb.
type Breadcrumb struct {
	Name string
	Path string
}

// DirectoryData holds the data passed to the directory picker fragment.
type DirectoryData struct {
	Path        string       // relative path from /workspace (e.g., "subdir/nested")
	FullPath    string       // absolute path (e.g., "/workspace/subdir/nested")
	Dirs        []string     // directory names at current level
	Breadcrumbs []Breadcrumb // path segments for breadcrumb navigation
}

// controlMessage is the JSON structure for WebSocket control messages (resize, refresh).
type controlMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// Server is the HTTP server serving the dashboard, API, SSE, and WebSocket.
type Server struct {
	templates *template.Template
	sm        *SessionManager
	broker    *Broker
	mux       *http.ServeMux
	upgrader  websocket.Upgrader
}

// NewServer creates a Server by parsing embedded templates and registering
// all routes on the provided mux.
func NewServer(sm *SessionManager, broker *Broker, mux *http.ServeMux) (*Server, error) {
	tmpl, err := template.ParseFS(web.Templates, "templates/*.html", "templates/fragments/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, fmt.Errorf("creating static sub-FS: %w", err)
	}

	s := &Server{
		templates: tmpl,
		sm:        sm,
		broker:    broker,
		mux:       mux,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	// Register routes.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /fragments/sessions", s.handleSessionsFragment)
	mux.HandleFunc("POST /api/sessions", s.handleSpawn)
	mux.HandleFunc("DELETE /api/sessions/{terminalId}", s.handleKill)
	mux.HandleFunc("GET /api/directories", s.handleDirectories)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("GET /api/sessions/{terminalId}/scrollback", s.handleScrollback)
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocket)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	return s, nil
}

// handleIndex renders the full dashboard page with initial session data.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sessions := s.sm.ListSessions()
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
	sessions := s.sm.ListSessions()
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

// handleSpawn creates a new Claude Code session inside a tmux session.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	cwd := r.FormValue("cwd")
	if cwd == "" {
		http.Error(w, "missing cwd parameter", http.StatusBadRequest)
		return
	}

	sessionName, err := s.sm.Spawn(cwd)
	if err != nil {
		slog.Error("failed to spawn session", "cwd", cwd, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("X-Terminal-Id", sessionName)
	s.handleSessionsFragment(w, r)
}

// handleKill terminates a tmux session.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	if err := s.sm.Kill(sessionName); err != nil {
		slog.Error("failed to kill session", "session", sessionName, "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	s.handleSessionsFragment(w, r)
}

// handleDirectories lists directories under /workspace for the directory picker.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	subpath := r.URL.Query().Get("path")

	// Resolve and validate the target path.
	target := filepath.Join(workspaceRoot, subpath)
	absTarget, err := filepath.Abs(target)
	if err != nil || !strings.HasPrefix(absTarget, workspaceRoot) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absTarget)
	if err != nil || !info.IsDir() {
		http.Error(w, "directory not found", http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(absTarget)
	if err != nil {
		slog.Error("failed to read directory", "path", absTarget, "error", err)
		http.Error(w, "failed to read directory", http.StatusInternalServerError)
		return
	}

	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, entry.Name())
	}

	// Compute relative current path for display.
	currentRel, _ := filepath.Rel(workspaceRoot, absTarget)
	if currentRel == "." {
		currentRel = ""
	}

	// Build breadcrumbs from path segments.
	var breadcrumbs []Breadcrumb
	if currentRel != "" {
		parts := strings.Split(currentRel, string(filepath.Separator))
		for i, part := range parts {
			breadcrumbs = append(breadcrumbs, Breadcrumb{
				Name: part,
				Path: strings.Join(parts[:i+1], "/"),
			})
		}
	}

	data := DirectoryData{
		Path:        currentRel,
		FullPath:    absTarget,
		Dirs:        dirs,
		Breadcrumbs: breadcrumbs,
	}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "directory-picker", data); err != nil {
		slog.Error("failed to render directory picker", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// handleScrollback returns the ring buffer contents for a session as plain text.
func (s *Server) handleScrollback(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	relay := s.sm.GetRelay(sessionName)
	if relay == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	data := relay.ringBuf.Bytes()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

// handleSSE streams Server-Sent Events to the client. It subscribes to the
// broker and forwards update notifications as SSE events until the client
// disconnects.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	id, ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(id)

	for {
		select {
		case <-ch:
			fmt.Fprint(w, "event: update\ndata: \n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleWebSocket upgrades the HTTP connection to a WebSocket and registers
// the viewer with the session's relay. Output comes from the relay's ring
// buffer (replay) and live pipe-pane stream. Input is sent via the relay's
// unix socket. No tmux attach process is spawned.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	// Get the relay for this session.
	relay := s.sm.GetRelay(sessionName)
	if relay == nil || relay.IsStopped() {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "session", sessionName, "error", err)
		return
	}

	slog.Info("websocket attached", "session", sessionName)

	// Register viewer with the relay (sends reset + scrollback replay).
	relay.AddViewer(conn)

	// Read loop: WebSocket → relay (input/resize).
	// This goroutine runs until the WebSocket disconnects or the relay stops.
	defer func() {
		relay.RemoveViewer(conn)
		_ = conn.Close()
		slog.Info("websocket detached", "session", sessionName)
	}()

	for {
		msgType, data, readErr := conn.ReadMessage()
		if readErr != nil {
			if websocket.IsUnexpectedCloseError(readErr,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				slog.Debug("websocket read error", "session", sessionName, "error", readErr)
			}
			return
		}

		// Check if relay has been stopped (session exited).
		if relay.IsStopped() {
			return
		}

		switch msgType {
		case websocket.TextMessage:
			// JSON control message (resize, refresh).
			var msg controlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Debug("invalid control message", "session", sessionName, "error", err)
				continue
			}
			if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				relay.Resize(conn, msg.Cols, msg.Rows)
			}
		case websocket.BinaryMessage:
			// Resume broadcast delivery if this viewer was suspended.
			relay.UnsuspendViewer(conn)
			// Resize tmux to this viewer's dimensions if it wasn't the last
			// to resize (mimics tmux's "window-size latest" behavior).
			relay.ResizeToViewer(conn)
			// Terminal input — send to tmux pane via relay.
			if err := relay.SendInput(data); err != nil {
				slog.Debug("relay input failed", "session", sessionName, "error", err)
				return
			}
		}
	}
}

// ListDirectoriesFlat returns the full absolute path for a workspace-relative
// directory selection. Used by handleSpawn to resolve the cwd.
func ListDirectoriesFlat(subpath string) (string, error) {
	target := filepath.Join(workspaceRoot, subpath)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(absTarget, workspaceRoot) {
		return "", fmt.Errorf("path must be under %s", workspaceRoot)
	}
	return absTarget, nil
}
