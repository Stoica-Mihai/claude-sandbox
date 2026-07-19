package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	api "claude-sandbox-api"
	"claude-sandbox-sessiond/protocol"

	"github.com/gorilla/websocket"
)

const (
	// maxUploadSize is the maximum allowed image upload size (10 MB).
	maxUploadSize = 10 << 20
	// maxSessionNameLen caps custom session names (bytes) — the index file
	// persists them, so unbounded names mean unbounded index growth.
	maxSessionNameLen = 120
	// maxJSONBody caps request bodies decoded as JSON — the authoritative limit,
	// shared with the frontend proxy's pre-forward bound via shared/limits.
	maxJSONBody = api.MaxJSONBody
)

// uploadDir lives on the claude-state volume shared with the sessions
// container, so claude can read pasted-image paths the backend writes. Resolved
// at startup by initPaths from protocol.StateDir (sibling of the socket dir) so
// it follows a mount move instead of being stranded off-volume. A var so tests
// can redirect it.
var uploadDir string

// newDirNameRe restricts new project folder names to a single safe path
// segment. The pattern is shared with the client-side pre-check via shared/enums.
var newDirNameRe = regexp.MustCompile(api.NewProjectNamePattern)

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

	mux.HandleFunc("GET "+api.RouteSessions, s.handleListSessions)
	mux.HandleFunc("GET "+api.RouteSessionsHistory, s.handleHistory)
	mux.HandleFunc("POST "+api.RouteSessions, s.handleSpawn)
	mux.HandleFunc("DELETE "+api.RouteSession, s.handleKill)
	mux.HandleFunc("DELETE "+api.RouteHistoryItem, s.handleDeleteHistory)
	mux.HandleFunc("PUT "+api.RouteSessionName, s.handleSetSessionName)
	mux.HandleFunc("GET "+api.RouteDirectories, s.handleDirectories)
	mux.HandleFunc("POST "+api.RouteDirectories, s.handleCreateDirectory)
	mux.HandleFunc("GET "+api.RouteSettings, s.handleGetSettings)
	mux.HandleFunc("PUT "+api.RouteSettings, s.handlePutSettings)
	mux.HandleFunc("GET "+api.RouteUIPrefs, s.handleGetUIPrefs)
	mux.HandleFunc("PUT "+api.RouteUIPrefs, s.handlePutUIPrefs)
	mux.HandleFunc("GET "+api.RouteEvents, s.handleSSE)
	mux.HandleFunc("GET "+api.RouteWSTerminal, s.handleWebSocket)
	mux.HandleFunc("POST "+api.RouteSessionUpload, s.handleUpload)
	mux.HandleFunc("GET "+api.RouteHealthz, s.handleHealthz)

	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode failed", "error", err)
	}
}

// writeErr writes a JSON error envelope ({"error": msg}) with the given status.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeWorkspaceDirErr maps a resolveWorkspaceDir sentinel to the 400 the
// directory-picker handlers return.
func writeWorkspaceDirErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotDir) {
		writeErr(w, http.StatusBadRequest, "directory not found")
		return
	}
	writeErr(w, http.StatusBadRequest, "invalid path")
}

// decodeJSON decodes the request body into dst, writing a 400 with errMsg and
// returning false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, errMsg string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, errMsg)
		return false
	}
	return true
}

// requireEnum reports whether val is in allowed; when not, it writes a 400
// "invalid <field>" and returns false so the caller returns early. Shared by
// the settings and ui-prefs validators.
func requireEnum(w http.ResponseWriter, val string, allowed []string, field string) bool {
	if slices.Contains(allowed, val) {
		return true
	}
	writeErr(w, http.StatusBadRequest, "invalid "+field)
	return false
}

// requirePathValue returns a required path parameter, writing a 400 with
// missingMsg and returning ok=false when it is empty.
func requirePathValue(w http.ResponseWriter, r *http.Request, key, missingMsg string) (string, bool) {
	v := r.PathValue(key)
	if v == "" {
		writeErr(w, http.StatusBadRequest, missingMsg)
		return "", false
	}
	return v, true
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
	var req api.SpawnRequest
	if !decodeJSON(w, r, &req, "invalid JSON") {
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
		switch {
		case errors.Is(err, ErrUnknownSession):
			writeErr(w, http.StatusNotFound, err.Error())
		case errors.Is(err, errSessiondUnreachable):
			writeErr(w, http.StatusBadGateway, err.Error())
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, api.SpawnResponse{SessionName: sessionName})
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
	uuid, ok := requirePathValue(w, r, "uuid", "missing uuid")
	if !ok {
		return
	}

	if err := s.sm.DeleteHistory(uuid); err != nil {
		// An unknown uuid (not in the index) maps to 404; anything else is a
		// failure killing the live session.
		if errors.Is(err, ErrUnknownSession) {
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
	sessionName, ok := requirePathValue(w, r, "terminalId", "missing session name")
	if !ok {
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
	sessionName, ok := requirePathValue(w, r, "terminalId", "missing session name")
	if !ok {
		return
	}

	// Check session exists
	if !s.sm.HasSession(sessionName) {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req, "invalid JSON") {
		return
	}
	if len(req.Name) > maxSessionNameLen {
		writeErr(w, http.StatusBadRequest, "name too long")
		return
	}

	if err := s.sm.SetSessionName(sessionName, req.Name); err != nil {
		slog.Error("failed to persist session name", "session", sessionName, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to save name")
		return
	}
	s.broker.Publish()
	w.WriteHeader(http.StatusNoContent)
}

// handleDirectories lists directories under /workspace for the directory picker.
func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	subpath := r.URL.Query().Get("path")

	// Resolve and validate the target path.
	absTarget, err := resolveWorkspaceDir(filepath.Join(workspaceRoot, subpath))
	if err != nil {
		writeWorkspaceDirErr(w, err)
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

// handleCreateDirectory creates a new project folder under /workspace and
// optionally runs `git init` in it. The parent is resolved with the same
// join/prefix/stat logic as handleDirectories so both agree on error messages.
func (s *Server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	var req api.CreateDirectoryRequest
	if !decodeJSON(w, r, &req, "invalid request body") {
		return
	}

	// Validate the name before any filesystem call.
	if !newDirNameRe.MatchString(req.Name) {
		writeErr(w, http.StatusBadRequest, "Invalid name")
		return
	}

	// Resolve and validate the parent path.
	absParent, err := resolveWorkspaceDir(filepath.Join(workspaceRoot, req.Path))
	if err != nil {
		writeWorkspaceDirErr(w, err)
		return
	}

	newDir := filepath.Join(absParent, req.Name)
	if err := os.Mkdir(newDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			writeErr(w, http.StatusConflict, "Folder already exists")
			return
		}
		slog.Error("failed to create directory", "path", newDir, "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to create directory")
		return
	}

	rel, _ := filepath.Rel(workspaceRoot, newDir)

	// git init failures keep the folder — the directory itself succeeded.
	var warning string
	if req.GitInit {
		out, gitErr := exec.Command("git", "-C", newDir, "init").CombinedOutput()
		if gitErr != nil {
			slog.Error("git init failed", "path", newDir, "error", gitErr, "output", string(out))
			warning = "git init failed"
		}
	}

	writeJSON(w, http.StatusCreated, api.CreateDirectoryResponse{Path: rel, Warning: warning})
}

// handleUpload accepts an image file upload and saves it to a temp directory
// accessible from the session. Returns the file path as JSON.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	sessionName, ok := requirePathValue(w, r, "terminalId", "missing session name")
	if !ok {
		return
	}

	// Reject path traversal attempts.
	if strings.Contains(sessionName, "/") || strings.Contains(sessionName, "..") {
		writeErr(w, http.StatusBadRequest, "invalid session name")
		return
	}

	if !s.sm.HasSession(sessionName) {
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

	// Periodic comment keepalive: updates only publish on real change now, so
	// idle streams need heartbeats to keep proxies/browsers from timing out.
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ch:
			fmt.Fprint(w, "event: update\ndata: \n\n")
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// preAttachInputMax bounds input buffered between WS open and the first
// resize (which triggers the protocol ATTACH); the client resizes on open, so
// this holds at most a few early keystrokes.
const preAttachInputMax = 64 << 10

// handleWebSocket upgrades the HTTP connection and bridges it to the
// session's sessiond socket: WS binary ↔ DATA frames, WS text ↔ CONTROL
// frames. The bridge holds no session state; sessiond owns the PTY, the
// emulator, and the viewer fan-out.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("terminalId")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	if !s.sm.HasSession(sessionName) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "session", sessionName, "error", err)
		return
	}
	defer conn.Close()

	sess, err := s.sm.DialSession(sessionName)
	if err != nil {
		// Already upgraded: signal failure with an abnormal close so the
		// client retries with backoff (sessiond may be mid-restart).
		slog.Warn("session dial failed", "session", sessionName, "error", err)
		msg := websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "session connection failed")
		_ = conn.WriteMessage(websocket.CloseMessage, msg)
		return
	}
	defer sess.Close()

	slog.Info("websocket attached", "session", sessionName)
	defer slog.Info("websocket detached", "session", sessionName)

	done := make(chan struct{})
	go func() {
		defer close(done)
		bridgeFramesToWS(sess, conn)
	}()
	bridgeWSToFrames(conn, sess, sessionName)
	_ = sess.Close() // unblocks the frame reader
	<-done
}

// bridgeWSToFrames pumps WS messages into protocol frames. The first resize
// becomes the ATTACH handshake (carrying the viewer's dimensions, so the
// snapshot renders at them); binary input arriving before it is buffered.
func bridgeWSToFrames(conn *websocket.Conn, sess net.Conn, sessionName string) {
	attached := false
	var pending []byte

	writeFrame := func(typ byte, payload []byte) bool {
		_ = sess.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := protocol.WriteFrame(sess, typ, payload); err != nil {
			slog.Debug("session write failed", "session", sessionName, "error", err)
			return false
		}
		return true
	}

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				slog.Debug("websocket read error", "session", sessionName, "error", err)
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			var msg protocol.Control
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Debug("invalid control message", "session", sessionName, "error", err)
				continue
			}
			switch msg.Type {
			case protocol.ControlResize:
				if msg.Cols == 0 || msg.Rows == 0 {
					continue
				}
				// The first valid resize becomes the ATTACH handshake (it carries
				// the viewer's dimensions); later ones forward as CONTROL.
				if !attached {
					att, _ := json.Marshal(protocol.Attach{Cols: msg.Cols, Rows: msg.Rows})
					if !writeFrame(protocol.FrameAttach, att) {
						return
					}
					attached = true
					if len(pending) > 0 {
						if !writeFrame(protocol.FrameData, pending) {
							return
						}
						pending = nil
					}
					continue
				}
				if !writeFrame(protocol.FrameControl, data) {
					return
				}
			case protocol.ControlReactivate:
				// Passive live-view request — meaningless before attach.
				if attached && !writeFrame(protocol.FrameControl, data) {
					return
				}
			}
		case websocket.BinaryMessage:
			if !attached {
				if len(pending)+len(data) <= preAttachInputMax {
					pending = append(pending, data...)
				}
				continue
			}
			if !writeFrame(protocol.FrameData, data) {
				return
			}
		}
	}
}

// bridgeFramesToWS pumps protocol frames into WS messages. A CLOSE frame maps
// to a normal WS closure (the session ended); any other stream end leaves the
// socket to close abnormally, so the client's reconnect logic engages.
func bridgeFramesToWS(sess net.Conn, conn *websocket.Conn) {
	writeWS := func(msgType int, data []byte) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(msgType, data) == nil
	}
	// Buffered reads + a reused scratch buffer: this loop carries every PTY
	// output chunk, and gorilla copies the payload into its own write buffer.
	br := bufio.NewReader(sess)
	scratch := make([]byte, 16<<10)
	for {
		typ, payload, err := protocol.ReadFrameInto(br, scratch)
		if err != nil {
			return
		}
		switch typ {
		case protocol.FrameSnapshot, protocol.FrameData:
			if !writeWS(websocket.BinaryMessage, payload) {
				return
			}
		case protocol.FrameControl:
			if !writeWS(websocket.TextMessage, payload) {
				return
			}
		case protocol.FrameClose:
			writeWS(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session ended"))
			return
		}
	}
}
