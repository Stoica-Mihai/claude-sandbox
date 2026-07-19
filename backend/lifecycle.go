package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// Spawn creates a new Claude Code conversation in cwd. It generates a uuid and
// passes it to sessiond so the dashboard owns the conversation id.
func (sm *SessionManager) Spawn(cwd string) (string, error) {
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	uuid := newUUID()
	name, err := sm.spawn(absPath, uuid, false)
	if err != nil {
		return "", err
	}
	if err := sm.index.add(uuid, absPath, time.Now().Unix()); err != nil {
		// The session is already live; a failed index write only costs its
		// resume-history entry, so log rather than orphan a working session.
		slog.Warn("failed to persist session index entry", "uuid", uuid, "error", err)
	}
	return name, nil
}

// Resume reopens a previously recorded conversation by uuid. If the
// conversation is already running, its live session is returned instead of
// spawning a second `claude --resume` onto the same transcript.
func (sm *SessionManager) Resume(uuid string) (string, error) {
	if rec, ok := sm.store.byUUID(uuid); ok {
		return rec.Name, nil
	}
	// Index membership gates the uuid; the format check keeps malformed values
	// out of the claude command line outright (defense in depth).
	if !uuidRe.MatchString(uuid) {
		return "", fmt.Errorf("%w: %s", ErrUnknownSession, uuid)
	}
	cwd, ok := sm.index.cwd(uuid)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownSession, uuid)
	}
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	return sm.spawn(absPath, uuid, true)
}

// spawn delegates to sessiond and records the new session in the store.
func (sm *SessionManager) spawn(absPath, uuid string, resume bool) (string, error) {
	name, err := sm.sd.Spawn(absPath, uuid, resume)
	if err != nil {
		return "", err
	}
	sm.store.add(sessionRecord{
		Name:      name,
		CWD:       absPath,
		Created:   time.Now(),
		SessionID: uuid,
	})
	sm.broker.Publish()
	slog.Info("spawned session", "session", name, "cwd", absPath, "uuid", uuid, "resume", resume)
	return name, nil
}

// DeleteHistory removes a conversation from history: it verifies the uuid is in
// the index, kills any live session running that conversation, then drops the
// index entry and its transcript. An unknown uuid is an error before any kill
// or delete, so callers can map it to a 404.
func (sm *SessionManager) DeleteHistory(uuid string) error {
	if _, ok := sm.index.cwd(uuid); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSession, uuid)
	}

	// Kill the live session running this conversation, if any, and wait for it
	// to actually exit before deleting the transcript. sessiond ACKs a kill at
	// SIGTERM, but claude can still flush its .jsonl during the grace period, so
	// deleting earlier would let the dying process re-create the transcript we
	// removed — an orphan the index no longer references.
	if rec, ok := sm.store.byUUID(uuid); ok {
		if err := sm.Kill(rec.Name); err != nil {
			return err
		}
		sm.waitForSessionGone(rec.Name)
	}

	if err := sm.index.remove(uuid); err != nil {
		return err
	}
	deleteTranscript(uuid)
	return nil
}

// waitForSessionGone blocks until sessiond's list no longer contains name (the
// process has exited and been reaped) or sessionExitWait elapses. A list error
// or the timeout returns best-effort, so a wedged sessiond can't hang a delete.
func (sm *SessionManager) waitForSessionGone(name string) {
	deadline := time.Now().Add(sessionExitWait)
	for {
		infos, err := sm.sd.List()
		if err != nil {
			return
		}
		if !containsSession(infos, name) {
			return
		}
		if !time.Now().Before(deadline) {
			slog.Warn("session still listed after kill; deleting transcript anyway", "session", name)
			return
		}
		time.Sleep(sessionExitPoll)
	}
}

// containsSession reports whether infos holds a session with the given name.
func containsSession(infos []protocol.SessionInfo, name string) bool {
	for _, info := range infos {
		if info.Name == name {
			return true
		}
	}
	return false
}

// Kill terminates a session via sessiond and drops it from the store. A
// session sessiond no longer knows is treated as already dead.
func (sm *SessionManager) Kill(sessionName string) error {
	if _, ok := sm.store.get(sessionName); !ok {
		return fmt.Errorf("session not found: %s", sessionName)
	}

	if err := sm.sd.Kill(sessionName); err != nil && !errors.Is(err, errHostSession) {
		return err
	}

	slog.Info("killed session", "session", sessionName)
	sm.dropSession(sessionName)
	sm.broker.Publish()
	return nil
}
