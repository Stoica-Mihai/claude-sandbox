package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Spawn creates a new Claude Code conversation in cwd. It generates a uuid and
// passes it to claude via --session-id so the dashboard owns the conversation id.
func (sm *SessionManager) Spawn(cwd string) (string, error) {
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	uuid := newUUID()
	name, err := sm.spawnDtach(absPath, uuid, "--session-id "+uuid)
	if err != nil {
		return "", err
	}
	sm.index.add(uuid, absPath, time.Now().Unix())
	return name, nil
}

// Resume reopens a previously recorded conversation by uuid (only uuids in the
// index can be resumed, which gates the shell-interpolated value).
func (sm *SessionManager) Resume(uuid string) (string, error) {
	cwd, ok := sm.index.cwd(uuid)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownSession, uuid)
	}
	absPath, err := validWorkspaceDir(cwd)
	if err != nil {
		return "", err
	}
	return sm.spawnDtach(absPath, uuid, "--resume "+uuid)
}

// DeleteHistory removes a conversation from history: it verifies the uuid is in
// the index, kills any live dtach session running that conversation, then drops
// the index entry and its transcript. An unknown uuid is an error before any
// kill or delete, so callers can map it to a 404.
func (sm *SessionManager) DeleteHistory(uuid string) error {
	if _, ok := sm.index.cwd(uuid); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSession, uuid)
	}

	// Resolve the live session via discoverSessions (not the cached, heavier
	// ListSessions) and kill it by dtach name before dropping its history.
	for _, s := range discoverSessions() {
		if s.SessionID == uuid {
			if err := sm.Kill(s.Name); err != nil {
				return err
			}
			break
		}
	}

	sm.index.remove(uuid)
	deleteTranscript(uuid)
	return nil
}

// spawnDtach launches a dtach session running claude with the given flag
// (`--session-id <uuid>` or `--resume <uuid>`), records the uuid in the meta
// sidecar, starts the relay, and returns the dtach session name.
func (sm *SessionManager) spawnDtach(absPath, uuid, claudeFlag string) (string, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		claudePath = "claude"
	}

	for i := 0; i < maxSpawnRetries; i++ {
		sessionName := generateSessionName()
		if _, statErr := os.Stat(sockPath(sessionName)); statErr == nil {
			continue // name collision, retry
		}

		if err := writeSessionMeta(sessionName, absPath, uuid); err != nil {
			slog.Warn("failed to write session metadata", "session", sessionName, "error", err)
		}

		innerScript := fmt.Sprintf("echo $$ > %q; exec %q %s --dangerously-skip-permissions",
			pidPath(sessionName), claudePath, claudeFlag)
		cmd := exec.Command("dtach", "-n", sockPath(sessionName), "-E", "-z",
			"bash", "-c", innerScript)
		cmd.Dir = absPath
		cmd.Env = append(os.Environ(), "TERM="+termType)

		if err := cmd.Run(); err != nil {
			slog.Warn("dtach spawn failed, retrying",
				"session", sessionName, "attempt", i+1, "error", err)
			removeSessionFiles(sessionName)
			continue
		}

		slog.Info("spawned session", "session", sessionName, "cwd", absPath, "uuid", uuid)

		if relay := sm.newRelay(sessionName); relay != nil {
			sm.relays.set(sessionName, relay)
		}

		// Wait for the inner bash to write the PID sidecar (it does so before
		// exec'ing claude) so discovery sees the session right away — otherwise
		// the sidebar card lags the tab until a later poll.
		deadline := time.Now().Add(pidWaitTimeout)
		for time.Now().Before(deadline) {
			if _, statErr := os.Stat(pidPath(sessionName)); statErr == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		sm.invalidateCache()
		sm.broker.Publish()
		return sessionName, nil
	}

	return "", fmt.Errorf("failed to create session after %d attempts", maxSpawnRetries)
}

// Kill terminates a session by signalling its process group via the PID sidecar.
func (sm *SessionManager) Kill(sessionName string) error {
	if !sessionAlive(sessionName) {
		return fmt.Errorf("session not found: %s", sessionName)
	}

	if pid := sessionPID(sessionName); pid > 0 {
		// Signal the process group (negative pid); the inner bash is the session
		// leader, so this reaps claude and any children it spawned.
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		deadline := time.Now().Add(killGracePeriod)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}

	slog.Info("killed session", "session", sessionName)

	sm.relays.remove(sessionName)

	removeSessionFiles(sessionName)
	sm.invalidateCache()
	sm.broker.Publish()
	return nil
}
