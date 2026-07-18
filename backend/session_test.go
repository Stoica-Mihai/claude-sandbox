package main

import (
	"os"
	"os/exec"
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
// group and writes the pid + meta sidecars so adoptSessions lists it as a
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

// TestRelayExitedCleansUpDeadSession: a relay stopping on its own with no live
// session behind it must drop out of the registry, clean the session's files,
// and publish an SSE update.
func TestRelayExitedCleansUpDeadSession(t *testing.T) {
	setSessionDirs(t)
	sm := &SessionManager{
		relays: newRelayRegistry(),
		store:  newSessionStore(),
		index:  &SessionIndex{entries: map[string]indexEntry{}},
		broker: NewBroker(),
	}
	_, ch := sm.broker.Subscribe()

	name := generateSessionName()
	if err := os.WriteFile(metaPath(name), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	sm.store.add(sessionRecord{Name: name, CWD: "/workspace/a"})

	relaySide, sessionSide := socketpairFiles(t)
	relay := NewRelay(name)
	relay.onExit = func() { sm.relayExited(name, relay) }
	relay.begin(relaySide, nil)
	sm.relays.set(name, relay)

	sessionSide.Close() // attach EOF → session gone → relay stops → relayExited

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sm.relays.get(name) == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sm.relays.get(name) != nil {
		t.Fatal("relayExited did not drop the relay from the registry")
	}
	<-relay.exited
	if _, err := os.Stat(metaPath(name)); !os.IsNotExist(err) {
		t.Fatalf("relayExited did not remove session files: %v", err)
	}
	if _, ok := sm.store.get(name); ok {
		t.Fatal("relayExited did not drop the store record")
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("relayExited did not publish an SSE update")
	}
}

// TestDeleteHistoryKillsLiveSession covers the branch where the store holds a
// live session whose SessionID matches the uuid: DeleteHistory must invoke
// Kill (terminating the process group), then drop the index entry and
// transcript. The store is seeded through adoptSessions, covering the boot
// adoption path against real sidecars too.
func TestDeleteHistoryKillsLiveSession(t *testing.T) {
	setSessionDirs(t)
	uuid := testUUID1
	_, tx := seedTranscript(t, uuid, "/workspace/a")

	name, cmd := spawnLiveSession(t, uuid)

	idx := loadSessionIndex()
	idx.add(uuid, "/workspace/a", 100)
	sm := &SessionManager{index: idx, relays: newRelayRegistry(), store: newSessionStore(), broker: NewBroker()}
	for _, rec := range adoptSessions() {
		sm.store.add(rec)
	}

	// Precondition: adoption saw the live session by its uuid.
	if rec, ok := sm.store.byUUID(uuid); !ok || rec.Name != name || rec.PID != cmd.Process.Pid {
		t.Fatalf("setup: adoptSessions did not record the live session: %+v ok=%v", rec, ok)
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
