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

// seedTranscript points CLAUDE_CONFIG_DIR at a fresh temp dir, creates the
// project dir derived from cwd, and writes a stub transcript for uuid.
func seedTranscript(t *testing.T, uuid, cwd string) (projDir, txPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
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
