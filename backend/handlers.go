package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

type controlMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type Server struct {
	sm       *SessionManager
	broker   *Broker
	mux      *http.ServeMux
	upgrader websocket.Upgrader
}

func NewServer(sm *SessionManager, broker *Broker, mux *http.ServeMux) *Server {
	s := &Server{
		sm:     sm,
		broker: broker,
		mux:    mux,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleSpawn)
	mux.HandleFunc("DELETE /api/sessions/{terminalId}", s.handleKill)
	mux.HandleFunc("PUT /api/sessions/{terminalId}/name", s.handleSetSessionName)
	mux.HandleFunc("GET /api/directories", s.handleDirectories)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocket)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleHealthz returns a simple JSON health check response.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// handleListSessions returns all claude-prefixed tmux sessions as JSON.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sm.ListSessions()
	writeJSON(w, 200, sessions)
}

// handleSpawn creates a new Claude Code session inside a tmux session.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.CWD == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing cwd parameter"})
		return
	}

	sessionName, err := s.sm.Spawn(req.CWD)
	if err != nil {
		slog.Error("failed to spawn session", "cwd", req.CWD, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 201, map[string]string{"session_name": sessionName})
}

// handleKill terminates a tmux session.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session name"})
		return
	}

	if err := s.sm.Kill(sessionName); err != nil {
		slog.Error("failed to kill session", "session", sessionName, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSetSessionName sets a custom display name for a session.
func (s *Server) handleSetSessionName(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session name"})
		return
	}

	// Check session exists
	relay := s.sm.GetRelay(sessionName)
	if relay == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	s.sm.SetSessionName(sessionName, req.Name)
	s.broker.Publish()
	w.WriteHeader(http.StatusNoContent)
}

type breadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type directoryResponse struct {
	Path        string       `json:"path"`
	FullPath    string       `json:"full_path"`
	Dirs        []string     `json:"dirs"`
	Breadcrumbs []breadcrumb `json:"breadcrumbs"`
}

// handleDirectories lists directories under /workspace for the directory picker.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	subpath := r.URL.Query().Get("path")

	// Resolve and validate the target path.
	target := filepath.Join(workspaceRoot, subpath)
	absTarget, err := filepath.Abs(target)
	if err != nil || !strings.HasPrefix(absTarget, workspaceRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	info, err := os.Stat(absTarget)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "directory not found"})
		return
	}

	entries, err := os.ReadDir(absTarget)
	if err != nil {
		slog.Error("failed to read directory", "path", absTarget, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read directory"})
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
	var breadcrumbs []breadcrumb
	if currentRel != "" {
		parts := strings.Split(currentRel, string(filepath.Separator))
		for i, part := range parts {
			breadcrumbs = append(breadcrumbs, breadcrumb{
				Name: part,
				Path: strings.Join(parts[:i+1], "/"),
			})
		}
	}

	resp := directoryResponse{
		Path:        currentRel,
		FullPath:    absTarget,
		Dirs:        dirs,
		Breadcrumbs: breadcrumbs,
	}

	writeJSON(w, 200, resp)
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
