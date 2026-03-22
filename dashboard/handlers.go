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

	"github.com/creack/pty"
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

// resizeMessage is the JSON structure sent by the client to resize the PTY.
type resizeMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
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

// handleSpawn creates a new Claude Code session from the POSTed form data.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	cwd := r.FormValue("cwd")
	if cwd == "" {
		http.Error(w, "missing cwd parameter", http.StatusBadRequest)
		return
	}

	ms, err := s.sm.Spawn(cwd)
	if err != nil {
		slog.Error("failed to spawn session", "cwd", cwd, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Return the updated sessions fragment. The HTMX client will swap it in.
	// Also set a custom header so the client JS knows the new terminal ID.
	w.Header().Set("X-Terminal-Id", ms.TerminalID)
	s.handleSessionsFragment(w, r)
}

// handleKill terminates a managed session.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	terminalID := r.PathValue("terminalId")
	if terminalID == "" {
		http.Error(w, "missing terminalId", http.StatusBadRequest)
		return
	}

	if err := s.sm.Kill(terminalID); err != nil {
		slog.Error("failed to kill session", "terminalId", terminalID, "error", err)
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

// handleWebSocket upgrades the HTTP connection to a WebSocket and relays
// bidirectional data between the browser terminal and the PTY. When the
// WebSocket disconnects, the PTY continues running (detached mode). When a
// new WebSocket connects, scrollback is replayed.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	terminalID := r.PathValue("terminalId")
	if terminalID == "" {
		http.Error(w, "missing terminalId", http.StatusBadRequest)
		return
	}

	ms := s.sm.Get(terminalID)
	if ms == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "terminalId", terminalID, "error", err)
		return
	}

	slog.Info("websocket attached", "terminalId", terminalID)

	// Detach any previously attached WebSocket.
	ms.wsMu.Lock()
	if ms.wsConn != nil {
		_ = ms.wsConn.Close()
	}
	ms.wsConn = conn
	ms.wsMu.Unlock()

	// Replay scrollback to the new connection.
	scrollback := ms.Scrollback.Bytes()
	if len(scrollback) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, scrollback); err != nil {
			slog.Error("scrollback replay failed", "terminalId", terminalID, "error", err)
			ms.wsMu.Lock()
			if ms.wsConn == conn {
				ms.wsConn = nil
			}
			ms.wsMu.Unlock()
			_ = conn.Close()
			return
		}
	}

	// Read from WebSocket → write to PTY.
	// This goroutine runs until the WebSocket disconnects or the process exits.
	go func() {
		defer func() {
			// On WS disconnect, detach but keep PTY alive.
			ms.wsMu.Lock()
			if ms.wsConn == conn {
				ms.wsConn = nil
			}
			ms.wsMu.Unlock()
			_ = conn.Close()
			slog.Info("websocket detached", "terminalId", terminalID)
		}()

		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err,
					websocket.CloseGoingAway,
					websocket.CloseNormalClosure,
				) {
					slog.Debug("websocket read error", "terminalId", terminalID, "error", err)
				}
				return
			}

			// Check if the process has exited.
			select {
			case <-ms.done:
				return
			default:
			}

			switch msgType {
			case websocket.TextMessage:
				// JSON control message (e.g., resize).
				var msg resizeMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Debug("invalid control message", "terminalId", terminalID, "error", err)
					continue
				}
				if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
					if err := pty.Setsize(ms.PTY, &pty.Winsize{
						Rows: msg.Rows,
						Cols: msg.Cols,
					}); err != nil {
						slog.Debug("pty resize failed", "terminalId", terminalID, "error", err)
					}
				}
			case websocket.BinaryMessage:
				// Terminal input data — write directly to PTY stdin.
				if _, err := ms.PTY.Write(data); err != nil {
					slog.Debug("pty write failed", "terminalId", terminalID, "error", err)
					return
				}
			}
		}
	}()

	// Block until the process exits or the connection is replaced.
	// The PTY→WS relay is handled by readPTY in session.go.
	select {
	case <-ms.done:
		// Process exited — close this WebSocket if still attached.
		ms.wsMu.Lock()
		if ms.wsConn == conn {
			closeMsg := websocket.FormatCloseMessage(
				websocket.CloseNormalClosure,
				"process exited",
			)
			_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
			ms.wsConn = nil
		}
		ms.wsMu.Unlock()
		_ = conn.Close()
	case <-r.Context().Done():
		// HTTP context cancelled.
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
