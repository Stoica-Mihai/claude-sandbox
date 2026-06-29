package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// setSessionDirs points the sockDir/metaDir package globals at fresh temp dirs
// for the duration of the test and restores them afterward.
func setSessionDirs(t *testing.T) (sock, meta string) {
	t.Helper()
	prevSock, prevMeta := sockDir, metaDir
	sock, meta = t.TempDir(), t.TempDir()
	sockDir, metaDir = sock, meta
	t.Cleanup(func() { sockDir, metaDir = prevSock, prevMeta })
	return sock, meta
}

// spawnLiveSession starts a real, killable child process in its own process
// group and writes the pid + meta sidecars so discoverSessions lists it as a
// live session whose SessionID is uuid. It returns the dtach session name and
// the running command (already started).
func spawnLiveSession(t *testing.T, uuid string) (string, *exec.Cmd) {
	t.Helper()
	name := generateSessionName()

	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	// Reap the child in the background so that once Kill terminates it the PID
	// is collected and processAlive (a signal-0 probe) stops seeing a zombie.
	// In production init reaps the inner bash; the test must do it itself.
	go func() { _, _ = cmd.Process.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })

	if err := os.WriteFile(pidPath(name), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid sidecar: %v", err)
	}
	if err := writeSessionMeta(name, "/workspace/a", uuid); err != nil {
		t.Fatalf("write meta sidecar: %v", err)
	}
	// A socket file so the relay/aliveness fallbacks have something to find.
	if err := os.WriteFile(sockPath(name), nil, 0o600); err != nil {
		t.Fatalf("write socket file: %v", err)
	}
	return name, cmd
}

// TestDeleteHistoryKillsLiveSession covers the branch where discoverSessions
// returns a live session whose SessionID matches the uuid: DeleteHistory must
// invoke Kill (terminating the process group), then drop the index entry and
// transcript.
func TestDeleteHistoryKillsLiveSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	setSessionDirs(t)

	uuid := "11111111-1111-4111-8111-111111111111"
	proj := filepath.Join(dir, "projects", "-workspace-a")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	tx := filepath.Join(proj, uuid+".jsonl")
	if err := os.WriteFile(tx, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	name, cmd := spawnLiveSession(t, uuid)

	idx := loadSessionIndex()
	idx.add(uuid, "/workspace/a", 100)
	sm := &SessionManager{index: idx, relays: map[string]*Relay{}, broker: NewBroker()}

	// Precondition: discovery sees the live session by its uuid.
	found := false
	for _, s := range discoverSessions() {
		if s.SessionID == uuid {
			found = true
		}
	}
	if !found {
		t.Fatal("setup: discoverSessions did not list the live session")
	}

	if err := sm.DeleteHistory(uuid); err != nil {
		t.Fatalf("DeleteHistory returned error: %v", err)
	}

	// The child's process group must have been signalled and exited.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(cmd.Process.Pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(cmd.Process.Pid) {
		t.Fatal("DeleteHistory did not kill the live session's process")
	}

	if _, ok := idx.cwd(uuid); ok {
		t.Fatal("DeleteHistory did not drop the index entry")
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatalf("DeleteHistory did not remove the transcript: stat err = %v", err)
	}
	// Sidecars for the killed session are gone.
	if _, err := os.Stat(pidPath(name)); !os.IsNotExist(err) {
		t.Fatalf("Kill did not remove the pid sidecar: stat err = %v", err)
	}
}
