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

	api "claude-sandbox-api"

	"github.com/gorilla/websocket"
)

const (
	uploadDir = "/tmp/uploads"
	// maxUploadSize is the maximum allowed image upload size (10 MB).
	maxUploadSize = 10 << 20
)

type controlMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type Server struct {
	sm       *SessionManager
	broker   *Broker
	upgrader websocket.Upgrader
}

func NewServer(sm *SessionManager, broker *Broker, mux *http.ServeMux) *Server {
	s := &Server{
		sm:     sm,
		broker: broker,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/history", s.handleHistory)
	mux.HandleFunc("POST /api/sessions", s.handleSpawn)
	mux.HandleFunc("DELETE /api/sessions/{terminalId}", s.handleKill)
	mux.HandleFunc("DELETE /api/sessions/history/{uuid}", s.handleDeleteHistory)
	mux.HandleFunc("PUT /api/sessions/{terminalId}/name", s.handleSetSessionName)
	mux.HandleFunc("GET /api/directories", s.handleDirectories)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
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

// writeErr writes a JSON error envelope ({"error": msg}) with the given status.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleHealthz returns a simple JSON health check response.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListSessions returns all claude-prefixed sessions as JSON.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sm.ListSessions()
	writeJSON(w, http.StatusOK, sessions)
}

// handleSpawn starts a new conversation, or resumes one when "resume" (a
// conversation uuid) is present.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD    string `json:"cwd"`
		Resume string `json:"resume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var sessionName string
	var err error
	if req.Resume != "" {
		sessionName, err = s.sm.Resume(req.Resume)
	} else {
		if req.CWD == "" {
			writeErr(w, http.StatusBadRequest, "missing cwd parameter")
			return
		}
		sessionName, err = s.sm.Spawn(req.CWD)
	}
	if err != nil {
		slog.Error("failed to start session", "cwd", req.CWD, "resume", req.Resume, "error", err)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"session_name": sessionName})
}

// handleHistory returns the previous sessions recorded for a folder.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		writeErr(w, http.StatusBadRequest, "missing cwd parameter")
		return
	}
	writeJSON(w, http.StatusOK, s.sm.History(cwd))
}

// handleDeleteHistory removes a conversation from the resume history by uuid.
func (s *Server) handleDeleteHistory(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		writeErr(w, http.StatusBadRequest, "missing uuid")
		return
	}

	if err := s.sm.DeleteHistory(uuid); err != nil {
		// An unknown uuid (not in the index) maps to 404; anything else is a
		// failure killing the live session.
		if strings.HasPrefix(err.Error(), "unknown session:") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		slog.Error("failed to delete history", "uuid", uuid, "error", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.broker.Publish()
	w.WriteHeader(http.StatusNoContent)
}

// handleKill terminates a session.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeErr(w, http.StatusBadRequest, "missing session name")
		return
	}

	if err := s.sm.Kill(sessionName); err != nil {
		slog.Error("failed to kill session", "session", sessionName, "error", err)
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSetSessionName sets a custom display name for a session.
func (s *Server) handleSetSessionName(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeErr(w, http.StatusBadRequest, "missing session name")
		return
	}

	// Check session exists
	relay := s.sm.GetRelay(sessionName)
	if relay == nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	s.sm.SetSessionName(sessionName, req.Name)
	s.broker.Publish()
	w.WriteHeader(http.StatusNoContent)
}

// handleDirectories lists directories under /workspace for the directory picker.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	subpath := r.URL.Query().Get("path")

	// Resolve and validate the target path.
	target := filepath.Join(workspaceRoot, subpath)
	absTarget, err := filepath.Abs(target)
	if err != nil || !underWorkspace(absTarget) {
		writeErr(w, http.StatusBadRequest, "invalid path")
		return
	}

	info, err := os.Stat(absTarget)
	if err != nil || !info.IsDir() {
		writeErr(w, http.StatusBadRequest, "directory not found")
		return
	}

	entries, err := os.ReadDir(absTarget)
	if err != nil {
		slog.Error("failed to read directory", "path", absTarget, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to read directory")
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
	var breadcrumbs []api.Breadcrumb
	if currentRel != "" {
		parts := strings.Split(currentRel, string(filepath.Separator))
		for i, part := range parts {
			breadcrumbs = append(breadcrumbs, api.Breadcrumb{
				Name: part,
				Path: strings.Join(parts[:i+1], "/"),
			})
		}
	}

	resp := api.DirectoryData{
		Path:        currentRel,
		FullPath:    absTarget,
		Dirs:        dirs,
		Breadcrumbs: breadcrumbs,
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleUpload accepts an image file upload and saves it to a temp directory
// accessible from the session. Returns the file path as JSON.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		writeErr(w, http.StatusBadRequest, "missing session name")
		return
	}

	// Reject path traversal attempts.
	if strings.Contains(sessionName, "/") || strings.Contains(sessionName, "..") {
		writeErr(w, http.StatusBadRequest, "invalid session name")
		return
	}

	relay := s.sm.GetRelay(sessionName)
	if relay == nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}

	// 10 MB max.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeErr(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing image field")
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
		writeErr(w, http.StatusBadRequest, "unsupported image type: "+ct)
		return
	}

	// Create upload directory for this session.
	sessionDir := filepath.Join(uploadDir, sessionName)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		slog.Error("failed to create upload dir", "path", sessionDir, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to create upload directory")
		return
	}

	var randBytes [4]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate filename")
		return
	}
	filename := "clipboard-" + hex.EncodeToString(randBytes[:]) + ext
	filePath := filepath.Join(sessionDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		slog.Error("failed to create file", "path", filePath, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		slog.Error("failed to write file", "path", filePath, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to write file")
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
// buffer (replay) and the live attach PTY. Input is sent via the relay's
// attach PTY.
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
			// Resize to this viewer's dimensions if it wasn't the last to
			// resize (mimics tmux's "window-size latest" behavior).
			relay.ResizeToViewer(conn)
			// Terminal input — send to the session via relay.
			if err := relay.SendInput(data); err != nil {
				slog.Debug("relay input failed", "session", sessionName, "error", err)
				return
			}
		}
	}
}
