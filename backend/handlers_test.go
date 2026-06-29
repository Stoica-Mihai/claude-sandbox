package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestServer builds a Server wired to a real Broker and a SessionManager
// backed by the given index, with no polling goroutine.
func newTestServer(idx *SessionIndex) *Server {
	return &Server{
		sm:     &SessionManager{index: idx, relays: map[string]*Relay{}},
		broker: NewBroker(),
	}
}

func TestHandleDeleteHistoryMissingUUID(t *testing.T) {
	s := newTestServer(loadSessionIndexFresh(t))

	// No PathValue set on the request: the {uuid} segment is empty.
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/history/", nil)
	rec := httptest.NewRecorder()
	s.handleDeleteHistory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "missing uuid" {
		t.Fatalf("error = %q, want %q", body["error"], "missing uuid")
	}
}

func TestHandleDeleteHistoryUnknownUUID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	idx := loadSessionIndex()
	s := newTestServer(idx)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/sessions/history/{uuid}", s.handleDeleteHistory)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/history/22222222-2222-4222-8222-222222222222", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "unknown session: 22222222-2222-4222-8222-222222222222" {
		t.Fatalf("error = %q", body["error"])
	}
}

func TestHandleDeleteHistorySuccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	// No CLAUDE_META_DIR: discoverSessions finds nothing, so the kill step is a
	// no-op and the handler exercises the 204 + Publish path.

	uuid := "11111111-1111-4111-8111-111111111111"
	proj := filepath.Join(dir, "projects", "-workspace-a")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	tx := filepath.Join(proj, uuid+".jsonl")
	if err := os.WriteFile(tx, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := loadSessionIndex()
	idx.add(uuid, "/workspace/a", 100)
	s := newTestServer(idx)

	// Subscribe so we can assert the handler published an SSE update.
	subID, ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(subID)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/sessions/history/{uuid}", s.handleDeleteHistory)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/history/"+uuid, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 response should have empty body, got %q", rec.Body.String())
	}
	if _, ok := idx.cwd(uuid); ok {
		t.Fatal("index entry was not removed")
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatalf("transcript was not deleted: stat err = %v", err)
	}

	select {
	case <-ch:
	default:
		t.Fatal("handler did not Publish() an SSE update on success")
	}
}

// loadSessionIndexFresh returns an index backed by an isolated temp config dir,
// for tests that don't otherwise set CLAUDE_CONFIG_DIR.
func loadSessionIndexFresh(t *testing.T) *SessionIndex {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	return loadSessionIndex()
}
