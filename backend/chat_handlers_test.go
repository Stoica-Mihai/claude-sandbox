package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	api "claude-sandbox-api"
)

// TestHandleSpawnInvalidKindReturns400: a kind other than terminal/chat is
// rejected before any spawn is attempted.
func TestHandleSpawnInvalidKindReturns400(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	body, _ := json.Marshal(map[string]string{"cwd": "/workspace/a", "kind": "bogus"})
	req := httptest.NewRequest(http.MethodPost, api.RouteSessions, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleSpawn(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an invalid kind", rec.Code)
	}
}

// TestSpawnChatKindDelegatesToHost: kind=chat flows through the internal
// spawn to the host spawn call and the resulting store record. (Exercised
// below handleSpawn's cwd-existence check — like TestSpawnDelegatesToHost in
// session_test.go — since /workspace does not exist on the host running these
// tests; HTTP-level kind validation is covered by
// TestHandleSpawnInvalidKindReturns400 above, which fails before any
// filesystem check.)
func TestSpawnChatKindDelegatesToHost(t *testing.T) {
	sm, fh := newTestManager(t, loadSessionIndexFresh(t))

	name, err := sm.spawn("/workspace/a", testUUID1, false, string(api.SessionKindChat))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	infos, _ := fh.List()
	if len(infos) != 1 || infos[0].Kind != string(api.SessionKindChat) {
		t.Fatalf("host spawn kind = %+v, want chat", infos)
	}
	rec, ok := sm.store.get(name)
	if !ok || rec.Kind != api.SessionKindChat {
		t.Fatalf("store record kind = %+v, want chat", rec)
	}
}

// TestSpawnDefaultKindIsTerminal: an absent kind spawns a terminal session,
// unchanged from before this field existed.
func TestSpawnDefaultKindIsTerminal(t *testing.T) {
	sm, fh := newTestManager(t, loadSessionIndexFresh(t))

	name, err := sm.spawn("/workspace/a", testUUID1, false, "")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	infos, _ := fh.List()
	if len(infos) != 1 || infos[0].Kind != "" {
		t.Fatalf("host spawn kind = %+v, want empty (terminal default)", infos)
	}
	rec, ok := sm.store.get(name)
	if !ok || rec.Kind != "" {
		t.Fatalf("store record kind = %+v, want empty (terminal default)", rec)
	}
}

// TestSwitchModeKillsAndRespawnsAsRequestedKind: mode switch kills the old
// session and respawns the same conversation uuid as the requested kind,
// leaving the session index untouched (kind is a live-child property, not a
// conversation property — design.md decision 1). Resume validates the cwd is
// a real directory under workspaceRoot, so this needs a writable /workspace
// (t.Skips on a host where it is absent, like writableWorkspaceParent's other
// callers).
func TestSwitchModeKillsAndRespawnsAsRequestedKind(t *testing.T) {
	rel := writableWorkspaceParent(t)
	cwd := filepath.Join(workspaceRoot, rel)

	sm, fh := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.index.add(testUUID1, cwd, 100)
	sm.store.add(sessionRecord{Name: "claude-term1", CWD: cwd, SessionID: testUUID1, Kind: api.SessionKindTerminal})

	name, err := sm.SwitchMode("claude-term1", string(api.SessionKindChat))
	if err != nil {
		t.Fatalf("SwitchMode: %v", err)
	}
	if got := fh.killedNames(); len(got) != 1 || got[0] != "claude-term1" {
		t.Fatalf("killed = %v, want [claude-term1]", got)
	}
	rec, ok := sm.store.get(name)
	if !ok || rec.Kind != api.SessionKindChat || rec.SessionID != testUUID1 {
		t.Fatalf("respawned record = %+v ok=%v, want chat kind + same uuid", rec, ok)
	}
	if gotCWD, ok := sm.index.cwd(testUUID1); !ok || gotCWD != cwd {
		t.Fatal("session index must be unaffected by a mode switch")
	}
}

// TestSwitchModeRejectsUnknownSession: switching a session name sessiond
// doesn't have a live record for is an error, not a silent no-op.
func TestSwitchModeRejectsUnknownSession(t *testing.T) {
	sm, _ := newTestManager(t, loadSessionIndexFresh(t))

	if _, err := sm.SwitchMode("claude-nope", string(api.SessionKindChat)); err == nil {
		t.Fatal("SwitchMode of an unknown session must error")
	}
}

// TestSwitchModeRejectsSameKind: switching to the kind already running is
// rejected rather than needlessly killing and respawning.
func TestSwitchModeRejectsSameKind(t *testing.T) {
	sm, _ := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.index.add(testUUID1, "/workspace/a", 100)
	sm.store.add(sessionRecord{Name: "claude-term1", CWD: "/workspace/a", SessionID: testUUID1, Kind: api.SessionKindTerminal})

	if _, err := sm.SwitchMode("claude-term1", string(api.SessionKindTerminal)); err == nil {
		t.Fatal("SwitchMode to the same kind must error, not silently kill+respawn")
	}
}

// TestHandleModeSwitchInvalidKindReturns400.
func TestHandleModeSwitchInvalidKindReturns400(t *testing.T) {
	sm, _ := newTestManager(t, &SessionIndex{entries: map[string]indexEntry{}})
	sm.index.add(testUUID1, "/workspace/a", 100)
	sm.store.add(sessionRecord{Name: "claude-term1", CWD: "/workspace/a", SessionID: testUUID1, Kind: api.SessionKindTerminal})
	s := &Server{sm: sm, broker: NewBroker()}

	body, _ := json.Marshal(map[string]string{"kind": "bogus"})
	req := httptest.NewRequest(http.MethodPost, api.SessionModePath("claude-term1"), bytes.NewReader(body))
	req.SetPathValue("terminalId", "claude-term1")
	rec := httptest.NewRecorder()
	s.handleModeSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleTranscriptUnknownSession404s: a terminalId with no live session
// 404s.
func TestHandleTranscriptUnknownSession404s(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	req := httptest.NewRequest(http.MethodGet, api.SessionTranscriptPath("claude-nope"), nil)
	req.SetPathValue("terminalId", "claude-nope")
	rec := httptest.NewRecorder()
	s.handleTranscript(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleTranscriptLiveSessionNoTranscriptYet: a live session with no
// recorded transcript file gets 200 with an empty body, not an error.
func TestHandleTranscriptLiveSessionNoTranscriptYet(t *testing.T) {
	testConfigDir(t)
	sm, _ := newTestManager(t, loadSessionIndexFresh(t))
	sm.store.add(sessionRecord{Name: "claude-c1", CWD: "/workspace/a", SessionID: testUUID1, Kind: api.SessionKindChat})
	s := &Server{sm: sm, broker: NewBroker()}

	req := httptest.NewRequest(http.MethodGet, api.SessionTranscriptPath("claude-c1"), nil)
	req.SetPathValue("terminalId", "claude-c1")
	rec := httptest.NewRecorder()
	s.handleTranscript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty (no transcript recorded yet)", rec.Body.String())
	}
}

// TestHandleTranscriptLiveSessionWithTranscript: an existing transcript file
// is streamed back verbatim.
func TestHandleTranscriptLiveSessionWithTranscript(t *testing.T) {
	_, txPath := seedTranscript(t, testUUID1, "/workspace/a")
	want := []byte("{}\n")
	if got, err := os.ReadFile(txPath); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("seedTranscript fixture: got %q err %v", got, err)
	}

	sm, _ := newTestManager(t, loadSessionIndexFresh(t))
	sm.store.add(sessionRecord{Name: "claude-c2", CWD: "/workspace/a", SessionID: testUUID1, Kind: api.SessionKindChat})
	s := &Server{sm: sm, broker: NewBroker()}

	req := httptest.NewRequest(http.MethodGet, api.SessionTranscriptPath("claude-c2"), nil)
	req.SetPathValue("terminalId", "claude-c2")
	rec := httptest.NewRecorder()
	s.handleTranscript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != string(want) {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
}
