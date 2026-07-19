package main

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	api "claude-sandbox-api"
	"claude-sandbox-sessiond/protocol"
)

// ErrUnknownSession is returned when a uuid is not present in the session index.
// Handlers map it to 404 via errors.Is rather than string matching.
var ErrUnknownSession = errors.New("unknown session")

const (
	// workspaceRoot is the base directory users may spawn sessions in.
	workspaceRoot = "/workspace"
	// pollInterval is how often the poller reconciles the store against
	// sessiond's LIST (catching sessions that exit on their own).
	pollInterval = 5 * time.Second
	// sessionExitWait bounds how long DeleteHistory waits for a killed session
	// to leave sessiond's list before deleting its transcript. It must exceed
	// sessiond's SIGTERM→SIGKILL grace so the process has finished flushing.
	sessionExitWait = 3 * time.Second
	// sessionExitPoll is the poll cadence within sessionExitWait.
	sessionExitPoll = 50 * time.Millisecond
)

// SessionManager owns the backend's in-memory session store, fed by sessiond
// (spawn/kill replies plus periodic LIST reconciliation). sessiond hosts the
// processes; this side keeps the API view and the persistent index.
type SessionManager struct {
	sd       sessionHost
	store    *sessionStore
	index    *SessionIndex // persisted uuid → {cwd,created,name}
	broker   *Broker
	stopPoll chan struct{}
}

// NewSessionManager syncs the store from sessiond and starts the poller.
func NewSessionManager(broker *Broker) *SessionManager {
	sm := &SessionManager{
		sd:       &protocolHost{sockDir: sockDir},
		store:    newSessionStore(),
		index:    loadSessionIndex(),
		broker:   broker,
		stopPoll: make(chan struct{}),
	}
	sm.refreshFromList()
	sm.sweepOrphanUploads()
	go sm.pollLoop()
	return sm
}

// sweepOrphanUploads removes upload dirs left by sessions that died while the
// backend was down — dropSession only cleans up on an observed live→dead
// transition, so a crash-gap leak would otherwise persist on the shared volume.
// Runs once at startup, after the store is reconciled and before serving.
func (sm *SessionManager) sweepOrphanUploads() {
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		return // no upload dir yet
	}
	live := make(map[string]bool)
	for _, rec := range sm.store.list() {
		live[rec.Name] = true
	}
	for _, e := range entries {
		if !e.IsDir() || live[e.Name()] {
			continue
		}
		p := filepath.Join(uploadDir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			slog.Warn("failed to remove orphan upload dir", "path", p, "error", err)
			continue
		}
		slog.Info("removed orphan upload dir", "path", p)
	}
}

// HasSession reports whether a live session with this name exists.
func (sm *SessionManager) HasSession(name string) bool {
	_, ok := sm.store.get(name)
	return ok
}

// DialSession opens an attach stream to a session via sessiond.
func (sm *SessionManager) DialSession(name string) (net.Conn, error) {
	return sm.sd.DialSession(name)
}

// SetSessionName stores a custom name on the live session's conversation (keyed
// by its uuid in the persistent index), so it shows in the sidebar and resume list.
func (sm *SessionManager) SetSessionName(sessionName, displayName string) error {
	rec, ok := sm.store.get(sessionName)
	if !ok || rec.SessionID == "" {
		return nil
	}
	return sm.index.setName(rec.SessionID, displayName)
}

// History returns the previous RESUMABLE sessions for a folder (newest first).
// Sessions that were spawned but never messaged have no claude transcript and
// would exit immediately on resume, so they are filtered out.
func (sm *SessionManager) History(cwd string) []SessionHistoryEntry {
	out := []SessionHistoryEntry{}
	for _, e := range sm.index.listByCwd(cwd) {
		if hasTranscript(e.UUID) {
			out = append(out, e)
		}
	}
	return out
}

// ListSessions returns all sessions from the in-memory store, with display
// names (custom name from the index, else the directory basename).
func (sm *SessionManager) ListSessions() []api.DisplaySession {
	records := sm.store.list()
	sessions := make([]api.DisplaySession, 0, len(records))
	for _, rec := range records {
		s := rec.display()
		if customName := sm.index.name(s.SessionID); customName != "" {
			s.DisplayName = customName
		} else {
			s.DisplayName = s.DirName
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// refreshFromList reconciles the store against sessiond's live list, reporting
// whether anything changed. A LIST failure (sessiond restarting) keeps the
// last known state.
func (sm *SessionManager) refreshFromList() bool {
	infos, err := sm.sd.List()
	if err != nil {
		slog.Debug("sessiond list failed", "error", err)
		return false
	}
	alive := make(map[string]protocol.SessionInfo, len(infos))
	for _, info := range infos {
		alive[info.Name] = info
	}

	changed := false
	for _, rec := range sm.store.list() {
		if _, ok := alive[rec.Name]; !ok {
			sm.dropSession(rec.Name)
			changed = true
		}
		delete(alive, rec.Name)
	}
	for _, info := range alive {
		sm.store.add(sessionRecord{
			Name:      info.Name,
			CWD:       info.CWD,
			Created:   time.Unix(info.Created, 0),
			SessionID: info.UUID,
		})
		changed = true
	}
	return changed
}

// dropSession removes a dead session's record and uploads. Uploaded image
// paths already handed to claude die with the session, never before.
func (sm *SessionManager) dropSession(name string) {
	sm.store.remove(name)
	uploadPath := filepath.Join(uploadDir, name)
	if err := os.RemoveAll(uploadPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to clean upload dir", "path", uploadPath, "error", err)
	}
}

// Shutdown stops the polling goroutine. Sessions live in sessiond and are
// unaffected by a backend shutdown.
func (sm *SessionManager) Shutdown() {
	close(sm.stopPoll)
	slog.Info("session manager shut down (sessions live in sessiond)")
}

// pollLoop reconciles the store against sessiond on a fixed cadence, so
// sessions that exit without dashboard interaction disappear from the UI.
func (sm *SessionManager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if sm.refreshFromList() {
				sm.broker.Publish()
			}
		case <-sm.stopPoll:
			return
		}
	}
}
