package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testUUID1 = "11111111-1111-4111-8111-111111111111"
	testUUID2 = "22222222-2222-4222-8222-222222222222"
)

// testConfigDir is the single owner of CLAUDE_CONFIG_DIR test isolation:
// every index/transcript write in a test lands in a temp dir, never the real
// ~/.claude-sandbox. Idempotent — a test that already isolated keeps its dir.
func testConfigDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" && strings.HasPrefix(d, os.TempDir()) {
		return d
	}
	d := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", d)
	return d
}

// loadSessionIndexFresh returns an index backed by an isolated config dir.
func loadSessionIndexFresh(t *testing.T) *SessionIndex {
	t.Helper()
	testConfigDir(t)
	return loadSessionIndex()
}

// freezeIndexWrites makes the session-index file and its config dir unwritable
// so every persist path fails (temp+rename needs a writable dir, the in-place
// fallback needs a writable file), exercising the index's rollback-on-error
// paths. Restores permissions on cleanup.
func freezeIndexWrites(t *testing.T, dir string) {
	t.Helper()
	file := filepath.Join(dir, "dashboard-sessions.json")
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
		_ = os.Chmod(file, 0o600)
	})
}

// seedTranscript isolates the config dir, creates the project dir derived
// from cwd, and writes a stub transcript for uuid.
func seedTranscript(t *testing.T, uuid, cwd string) (projDir, txPath string) {
	t.Helper()
	dir := testConfigDir(t)
	projDir = filepath.Join(dir, "projects", strings.ReplaceAll(cwd, "/", "-"))
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	txPath = filepath.Join(projDir, uuid+".jsonl")
	if err := os.WriteFile(txPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return projDir, txPath
}

// assertErrorBody asserts the recorder's status code and its JSON {"error":…} body.
func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantErr string) {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status = %d, want %d", rec.Code, wantCode)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != wantErr {
		t.Fatalf("error = %q, want %q", body["error"], wantErr)
	}
}
