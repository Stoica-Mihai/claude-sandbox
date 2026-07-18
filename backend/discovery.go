package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"time"
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

// adoptSessions runs ONCE at boot: it scans the PID sidecars for claude-*
// sessions that survived a backend restart (dtach masters are init children),
// unlinks the files of dead ones, and returns records for the live ones. After
// this the in-memory store is authoritative and updated by events; the scan is
// never repeated. Adoption keys off the PID sidecar (which the backend owns)
// rather than the socket: dtach removes its own socket when the inner process
// exits, so a socket scan would miss dead sessions and leak their metadata
// sidecars.
func adoptSessions() []sessionRecord {
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return nil
	}

	var records []sessionRecord

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
		records = append(records, sessionRecord{
			Name:      name,
			CWD:       meta.CWD,
			Created:   meta.createdTime(name),
			SessionID: meta.SessionID,
			PID:       sessionPID(name),
		})
	}

	return records
}

// generateSessionName creates a session name like "claude-a1b2c3d4".
func generateSessionName() string {
	return sessionPrefix + hex.EncodeToString(randBytes(4))
}
