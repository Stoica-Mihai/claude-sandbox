package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	api "claude-sandbox-api"
)

// writeSessionMeta writes the per-session metadata sidecar.
func writeSessionMeta(name, cwd, sessionID string) error {
	m := sessionMeta{CWD: cwd, Created: time.Now().Unix(), SessionID: sessionID}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(name), data, 0o600)
}

// readSessionMeta reads the per-session metadata sidecar (zero value if absent).
func readSessionMeta(name string) sessionMeta {
	var m sessionMeta
	data, err := os.ReadFile(metaPath(name))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

// discoverSessions scans the PID sidecars for live claude-* sessions, unlinking
// the sidecars of any whose process is gone. Discovery keys off the PID sidecar
// (which the backend owns) rather than the socket: dtach removes its own socket
// when the inner process exits, so a socket scan would miss dead sessions and
// leak their metadata sidecars. (Keying off the PID sidecar also avoids a
// premature-cleanup race: the meta is written before the socket exists, so a
// meta scan could delete a session still mid-spawn. spawnDtach waits for the PID
// sidecar before publishing, so a new session is discoverable as soon as it's up.)
func discoverSessions() []api.DisplaySession {
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return nil
	}

	var sessions []api.DisplaySession

	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, sessionPrefix) || !strings.HasSuffix(n, ".pid") {
			continue
		}
		name := strings.TrimSuffix(n, ".pid")
		if !sessionAlive(name) {
			removeSessionFiles(name)
			continue
		}

		meta := readSessionMeta(name)
		createdAt := meta.createdTime(name)

		sessions = append(sessions, api.DisplaySession{
			Name:      name,
			CWD:       meta.CWD,
			DirName:   filepath.Base(meta.CWD),
			CreatedAt: createdAt,
			Alive:     true,
			SessionID: meta.SessionID,
		})
	}

	// Oldest first → newest last, matching the tab bar (new tabs append on the
	// right), so a session sits in the same relative spot in sidebar and tabs.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})

	return sessions
}

// sessionsSignature builds a deterministic change-detection signature.
func sessionsSignature(sessions []api.DisplaySession) string {
	var b strings.Builder
	for _, s := range sessions {
		b.WriteString(s.Name)
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(s.CreatedAt.Unix(), 10))
		b.WriteByte('\n')
	}
	return b.String()
}

// generateSessionName creates a session name like "claude-a1b2c3d4".
func generateSessionName() string {
	return sessionPrefix + hex.EncodeToString(randBytes(4))
}
