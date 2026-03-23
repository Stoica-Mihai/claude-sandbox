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
	"os/exec"
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
// bidirectional data between the browser terminal and a tmux attach process.
// Each WebSocket connection gets its own ephemeral `tmux attach` PTY.
// When the WebSocket disconnects, the attach process is killed but the
// tmux session continues running.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	// Verify the tmux session exists before upgrading.
	if !s.sm.sessionExists(sessionName) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "session", sessionName, "error", err)
		return
	}

	slog.Info("websocket attached", "session", sessionName)

	// Spawn tmux attach with an ephemeral PTY.
	cmd := exec.Command("tmux", "attach", "-t", sessionName)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 120})
	if err != nil {
		slog.Error("failed to start tmux attach", "session", sessionName, "error", err)
		closeMsg := websocket.FormatCloseMessage(
			websocket.CloseInternalServerErr,
			"failed to attach to session",
		)
		_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
		_ = conn.Close()
		return
	}

	// done is closed when the attach process exits (session ended or killed).
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	// PTY → WebSocket relay goroutine.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					slog.Debug("websocket write error",
						"session", sessionName,
						"error", writeErr,
					)
					return
				}
			}
			if readErr != nil {
				// PTY closed — attach process exited.
				slog.Debug("attach pty read ended", "session", sessionName)
				return
			}
		}
	}()

	// WebSocket → PTY relay goroutine.
	go func() {
		defer func() {
			// On WS disconnect, kill the attach process and close the PTY.
			// The tmux session itself continues running.
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = ptmx.Close()
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

			// Check if the attach process has exited.
			select {
			case <-done:
				return
			default:
			}

			switch msgType {
			case websocket.TextMessage:
				// JSON control message (e.g., resize).
				var msg resizeMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Debug("invalid control message", "session", sessionName, "error", err)
					continue
				}
				if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
					if err := pty.Setsize(ptmx, &pty.Winsize{
						Rows: msg.Rows,
						Cols: msg.Cols,
					}); err != nil {
						slog.Debug("pty resize failed", "session", sessionName, "error", err)
					}
				}
			case websocket.BinaryMessage:
				// Terminal input data — write directly to attach PTY stdin.
				if _, err := ptmx.Write(data); err != nil {
					slog.Debug("pty write failed", "session", sessionName, "error", err)
					return
				}
			}
		}
	}()

	// Block until the attach process exits or the HTTP context is cancelled.
	select {
	case <-done:
		// Attach process exited (tmux session ended).
		closeMsg := websocket.FormatCloseMessage(
			websocket.CloseNormalClosure,
			"session ended",
		)
		_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
		_ = ptmx.Close()
		_ = conn.Close()
		// Publish to update the session list (session may have exited).
		s.sm.invalidateCache()
		s.broker.Publish()
	case <-r.Context().Done():
		// HTTP context cancelled — cleanup handled by defer in WS→PTY goroutine.
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
