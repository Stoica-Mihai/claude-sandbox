package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// fakeHost is an in-memory sessionHost for tests.
type fakeHost struct {
	mu       sync.Mutex
	sessions []protocol.SessionInfo
	killed   []string
	spawnErr error
	killErr  error
	dial     func(name string) (net.Conn, error)
	listFn   func() ([]protocol.SessionInfo, error) // overrides sessions when set
}

func (f *fakeHost) Spawn(cwd, uuid string, resume bool, kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spawnErr != nil {
		return "", f.spawnErr
	}
	name := fmt.Sprintf("claude-fake%04d", len(f.sessions))
	f.sessions = append(f.sessions, protocol.SessionInfo{Name: name, CWD: cwd, UUID: uuid, Created: time.Now().Unix(), Kind: kind})
	return name, nil
}

func (f *fakeHost) List() ([]protocol.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listFn != nil {
		return f.listFn()
	}
	return append([]protocol.SessionInfo(nil), f.sessions...), nil
}

func (f *fakeHost) Kill(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, name)
	return f.killErr
}

func (f *fakeHost) DialSession(name string) (net.Conn, error) {
	if f.dial != nil {
		return f.dial(name)
	}
	return nil, errors.New("no dial configured")
}

func (f *fakeHost) killedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killed...)
}

// newTestManager builds a SessionManager on a fakeHost with no poller, on an
// isolated config dir (SessionIndex.save() writes there; without isolation a
// test mutation would clobber the developer's real dashboard-sessions.json).
func newTestManager(t *testing.T, idx *SessionIndex) (*SessionManager, *fakeHost) {
	t.Helper()
	testConfigDir(t)
	fh := &fakeHost{}
	sm := &SessionManager{
		sd:     fh,
		store:  newSessionStore(),
		index:  idx,
		broker: NewBroker(),
	}
	return sm, fh
}

// TestResumeAlreadyLive: resuming a conversation that already has a live
// session returns that session instead of spawning a second claude --resume
// (two writers on one transcript).
func TestResumeAlreadyLive(t *testing.T) {
	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.index.add(testUUID1, "/workspace/a", 100)
	sm.store.add(sessionRecord{Name: "claude-live1234", CWD: "/workspace/a", SessionID: testUUID1})

	name, err := sm.Resume(testUUID1, "")
	if err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}
	if name != "claude-live1234" {
		t.Fatalf("Resume = %q, want the live session name", name)
	}
	if s, _ := fh.List(); len(s) != 0 {
		t.Fatal("Resume of a live conversation must not spawn")
	}
}

// TestResumeRejectsMalformedUUID: an index entry whose key is not an RFC-4122
// uuid must never reach the claude command line.
func TestResumeRejectsMalformedUUID(t *testing.T) {
	bad := `x"; rm -rf /; echo "`
	sm, _ := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{bad: {CWD: "/workspace/a"}}})

	if _, err := sm.Resume(bad, ""); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("Resume(malformed) err = %v, want ErrUnknownSession", err)
	}
}

// TestSpawnDelegatesToHost: the inner spawn records the sessiond reply in the
// store and publishes an SSE update.
func TestSpawnDelegatesToHost(t *testing.T) {
	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	_, ch := sm.broker.Subscribe()

	name, err := sm.spawn("/workspace/a", testUUID1, false, "")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	rec, ok := sm.store.get(name)
	if !ok || rec.SessionID != testUUID1 || rec.CWD != "/workspace/a" {
		t.Fatalf("store record = %+v ok=%v", rec, ok)
	}
	if s, _ := fh.List(); len(s) != 1 {
		t.Fatalf("host sessions = %d, want 1", len(s))
	}
	select {
	case <-ch:
	default:
		t.Fatal("spawn did not publish an SSE update")
	}
}

// TestKillDelegatesToHost: Kill asks sessiond, drops the record, publishes.
func TestKillDelegatesToHost(t *testing.T) {
	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.store.add(sessionRecord{Name: "claude-k1", CWD: "/workspace/a", SessionID: testUUID1})
	_, ch := sm.broker.Subscribe()

	if err := sm.Kill("claude-k1"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if got := fh.killedNames(); len(got) != 1 || got[0] != "claude-k1" {
		t.Fatalf("host killed = %v", got)
	}
	if _, ok := sm.store.get("claude-k1"); ok {
		t.Fatal("Kill did not drop the store record")
	}
	select {
	case <-ch:
	default:
		t.Fatal("Kill did not publish an SSE update")
	}

	if err := sm.Kill("claude-nope"); err == nil {
		t.Fatal("Kill of unknown session must error")
	}
}

// TestKillTreatsUnknownHostSessionAsDead: sessiond not knowing the session is
// not a failure — the record is dropped anyway.
func TestKillTreatsUnknownHostSessionAsDead(t *testing.T) {
	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	fh.killErr = fmt.Errorf("%w: session not found", errHostSession)
	sm.store.add(sessionRecord{Name: "claude-gone", CWD: "/workspace/a"})

	if err := sm.Kill("claude-gone"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, ok := sm.store.get("claude-gone"); ok {
		t.Fatal("record not dropped for a host-unknown session")
	}
}

// TestDeleteHistoryKillsLiveSession covers the branch where the store holds a
// live session whose SessionID matches the uuid: DeleteHistory must kill it
// via sessiond, then drop the index entry and transcript.
func TestDeleteHistoryKillsLiveSession(t *testing.T) {
	uuid := testUUID1
	idx, tx := seedIndexedTranscript(t, uuid, "/workspace/a")
	sm, fh := newTestManager(t, idx)
	sm.store.add(sessionRecord{Name: "claude-live9999", CWD: "/workspace/a", SessionID: uuid})

	if err := sm.DeleteHistory(uuid); err != nil {
		t.Fatalf("DeleteHistory returned error: %v", err)
	}

	if got := fh.killedNames(); len(got) != 1 || got[0] != "claude-live9999" {
		t.Fatalf("host killed = %v, want the live session", got)
	}
	if _, ok := idx.cwd(uuid); ok {
		t.Fatal("DeleteHistory did not drop the index entry")
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatalf("DeleteHistory did not remove the transcript: stat err = %v", err)
	}
	if _, ok := sm.store.byUUID(uuid); ok {
		t.Fatal("DeleteHistory left the store record")
	}
}

// TestRefreshFromList reconciles the store against the host's live list in
// both directions and reports change correctly.
func TestRefreshFromList(t *testing.T) {
	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.store.add(sessionRecord{Name: "claude-stale", CWD: "/workspace/old"})
	fh.sessions = []protocol.SessionInfo{{Name: "claude-new", CWD: "/workspace/new", UUID: testUUID2, Created: 1234}}

	if !sm.refreshFromList() {
		t.Fatal("refresh with drift must report change")
	}
	if _, ok := sm.store.get("claude-stale"); ok {
		t.Fatal("stale record survived reconciliation")
	}
	rec, ok := sm.store.get("claude-new")
	if !ok || rec.SessionID != testUUID2 || rec.CWD != "/workspace/new" || rec.Created.Unix() != 1234 {
		t.Fatalf("new record = %+v ok=%v", rec, ok)
	}

	if sm.refreshFromList() {
		t.Fatal("refresh with no drift must report no change")
	}
}

// TestSweepOrphanUploads: a boot-time sweep removes upload dirs with no live
// session (leaked when a session died while the backend was down) and keeps a
// live session's dir.
func TestSweepOrphanUploads(t *testing.T) {
	sm, _ := newTestManager(t, loadSessionIndexFresh(t))
	sm.store.add(sessionRecord{Name: "claude-live01", CWD: "/workspace/a", SessionID: testUUID1})

	tmp := t.TempDir()
	orig := uploadDir
	uploadDir = tmp
	t.Cleanup(func() { uploadDir = orig })

	liveDir := filepath.Join(tmp, "claude-live01")
	orphanDir := filepath.Join(tmp, "claude-dead99")
	for _, d := range []string{liveDir, orphanDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	sm.sweepOrphanUploads()

	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Fatalf("orphan upload dir not removed: stat err = %v", err)
	}
	if _, err := os.Stat(liveDir); err != nil {
		t.Fatalf("live session upload dir must be kept: %v", err)
	}
}

// TestDeleteHistoryWaitsForSessionExit: a live conversation's transcript must
// not be deleted until its session has left sessiond's list (the process
// exited and finished flushing), or the dying process would re-create it.
func TestDeleteHistoryWaitsForSessionExit(t *testing.T) {
	uuid := testUUID1
	idx, tx := seedIndexedTranscript(t, uuid, "/workspace/a")
	sm, fh := newTestManager(t, idx)
	sm.store.add(sessionRecord{Name: "claude-live", CWD: "/workspace/a", SessionID: uuid})

	// The session lingers in the list for the first two polls, then exits. While
	// it is still listed the transcript must remain on disk. (listFn runs under
	// the fake's own lock, so calls is serialized.)
	calls := 0
	fh.listFn = func() ([]protocol.SessionInfo, error) {
		calls++
		if calls < 3 {
			if _, err := os.Stat(tx); err != nil {
				t.Errorf("transcript deleted while session still live (poll %d)", calls)
			}
			return []protocol.SessionInfo{{Name: "claude-live", UUID: uuid}}, nil
		}
		return nil, nil // exited
	}

	if err := sm.DeleteHistory(uuid); err != nil {
		t.Fatalf("DeleteHistory: %v", err)
	}
	if calls < 3 {
		t.Fatalf("did not wait for the session to leave the list (calls=%d)", calls)
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatalf("transcript not deleted after the session exited: %v", err)
	}
}
