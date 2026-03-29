package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

const uploadDir = "/tmp/uploads"

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
	mux.HandleFunc("POST /api/sessions/{terminalId}/upload", s.handleUpload)
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

// handleUpload accepts an image file upload and saves it to a temp directory
// accessible from the tmux session. Returns the file path as JSON.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session name"})
		return
	}

	// Reject path traversal attempts.
	if strings.Contains(sessionName, "/") || strings.Contains(sessionName, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session name"})
		return
	}

	relay := s.sm.GetRelay(sessionName)
	if relay == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	// 10 MB max.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid form"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing image field"})
		return
	}
	defer file.Close()

	// Validate content type.
	ct := header.Header.Get("Content-Type")
	var ext string
	switch {
	case strings.HasPrefix(ct, "image/png"):
		ext = ".png"
	case strings.HasPrefix(ct, "image/jpeg"):
		ext = ".jpg"
	case strings.HasPrefix(ct, "image/gif"):
		ext = ".gif"
	case strings.HasPrefix(ct, "image/webp"):
		ext = ".webp"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported image type: " + ct})
		return
	}

	// Create upload directory for this session.
	sessionDir := filepath.Join(uploadDir, sessionName)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		slog.Error("failed to create upload dir", "path", sessionDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create upload directory"})
		return
	}

	var randBytes [4]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate filename"})
		return
	}
	filename := "clipboard-" + hex.EncodeToString(randBytes[:]) + ext
	filePath := filepath.Join(sessionDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		slog.Error("failed to create file", "path", filePath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		slog.Error("failed to write file", "path", filePath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write file"})
		return
	}

	slog.Info("image uploaded", "session", sessionName, "path", filePath, "size", header.Size)
	writeJSON(w, http.StatusOK, map[string]string{"path": filePath})
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
