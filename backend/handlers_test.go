package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "claude-sandbox-api"
)

// newTestServer builds a Server wired to a real Broker and a SessionManager
// backed by the given index, with no polling goroutine.
func newTestServer(t *testing.T, idx *SessionIndex) *Server {
	t.Helper()
	sm, _ := newTestManager(t, idx)
	return &Server{
		sm:     sm,
		broker: NewBroker(),
	}
}

func TestHandleDeleteHistoryMissingUUID(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	// No PathValue set on the request: the {uuid} segment is empty.
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/history/", nil)
	rec := httptest.NewRecorder()
	s.handleDeleteHistory(rec, req)

	assertErrorBody(t, rec, http.StatusBadRequest, "missing uuid")
}

func TestHandleDeleteHistoryUnknownUUID(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/sessions/history/{uuid}", s.handleDeleteHistory)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/history/"+testUUID2, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorBody(t, rec, http.StatusNotFound, "unknown session: "+testUUID2)
}

func TestHandleDeleteHistorySuccess(t *testing.T) {
	// Empty store: the kill step is a no-op and the handler exercises the
	// 204 + Publish path.
	uuid := testUUID1
	_, tx := seedTranscript(t, uuid, "/workspace/a")

	idx := loadSessionIndex()
	idx.add(uuid, "/workspace/a", 100)
	s := newTestServer(t, idx)

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

func TestHandleUploadMissingSession(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	req := httptest.NewRequest(http.MethodPost, "/api/sessions//upload", nil)
	rec := httptest.NewRecorder()
	s.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleUploadPathTraversal(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{terminalId}/upload", s.handleUpload)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/..%2f..%2fetc/upload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleUploadUnknownSession(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{terminalId}/upload", s.handleUpload)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/no-such-session/upload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleCreateDirectoryValidation exercises handleCreateDirectory's
// validation branches: name regex, parent resolve/prefix, and parent existence.
//
// The 409 (EEXIST), 500 (mkdir failure), 201 (success), and git-init branches
// require a writable path under workspaceRoot, a hardcoded const (Decision 6 —
// not injectable). The tests below reach them wherever workspaceRoot is writable
// (the container) and t.Skip on hosts where /workspace is absent/read-only.
func TestHandleCreateDirectoryValidation(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/directories", s.handleCreateDirectory)

	tests := []struct {
		name     string
		reqName  string
		reqPath  string
		wantCode int
		wantErr  string
	}{
		{name: "dotdot name", reqName: "..", wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "slash in name", reqName: "a/b", wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "leading dot name", reqName: ".hidden", wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "empty name", reqName: "", wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "65-char name", reqName: strings.Repeat("a", 65), wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "separator in name", reqName: "a" + string(filepath.Separator) + "b", wantCode: http.StatusBadRequest, wantErr: "Invalid name"},
		{name: "valid name parent gone", reqName: "proj", reqPath: "nope-does-not-exist", wantCode: http.StatusBadRequest, wantErr: "directory not found"},
		{name: "traversal escapes root", reqName: "proj", reqPath: "../../etc", wantCode: http.StatusBadRequest, wantErr: "invalid path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, err := json.Marshal(api.CreateDirectoryRequest{Name: tt.reqName, Path: tt.reqPath})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/directories", bytes.NewReader(bodyBytes))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertErrorBody(t, rec, tt.wantCode, tt.wantErr)
		})
	}
}

// TestHandleCreateDirectoryInvalidBody covers the 400 branch when the request
// body is not valid JSON (the decode fails before any filesystem access).
func TestHandleCreateDirectoryInvalidBody(t *testing.T) {
	s := newTestServer(t, loadSessionIndexFresh(t))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/directories", s.handleCreateDirectory)
	req := httptest.NewRequest(http.MethodPost, "/api/directories", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorBody(t, rec, http.StatusBadRequest, "invalid request body")
}

// writableWorkspaceParent creates a unique, empty parent directory under
// workspaceRoot and returns its path relative to workspaceRoot (the value a
// request's Path field takes). It t.Skips when workspaceRoot is absent or
// read-only (the host), and registers cleanup so /workspace stays clean.
func writableWorkspaceParent(t *testing.T) string {
	t.Helper()
	rel := "createdir-test-" + t.Name()
	rel = strings.NewReplacer("/", "_", " ", "_").Replace(rel)
	abs := filepath.Join(workspaceRoot, rel)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Skipf("workspaceRoot %q not writable: %v", workspaceRoot, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return rel
}

func postCreateDirectory(t *testing.T, s *Server, req api.CreateDirectoryRequest) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/directories", s.handleCreateDirectory)
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/directories", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httpReq)
	return rec
}

// TestHandleCreateDirectorySuccess covers the 201 path: os.Mkdir succeeds, the
// rel path is computed, and CreateDirectoryResponse is written with no warning.
func TestHandleCreateDirectorySuccess(t *testing.T) {
	parent := writableWorkspaceParent(t)
	s := newTestServer(t, loadSessionIndexFresh(t))

	rec := postCreateDirectory(t, s, api.CreateDirectoryRequest{Name: "proj", Path: parent})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp api.CreateDirectoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	wantRel := filepath.Join(parent, "proj")
	if resp.Path != wantRel {
		t.Fatalf("path = %q, want %q", resp.Path, wantRel)
	}
	if resp.Warning != "" {
		t.Fatalf("warning = %q, want empty", resp.Warning)
	}
	if info, err := os.Stat(filepath.Join(workspaceRoot, wantRel)); err != nil || !info.IsDir() {
		t.Fatalf("new dir not created on disk: info=%v err=%v", info, err)
	}
}

// TestHandleCreateDirectoryConflict covers the 409 branch: os.Mkdir returns
// os.ErrExist when the target folder already exists.
func TestHandleCreateDirectoryConflict(t *testing.T) {
	parent := writableWorkspaceParent(t)
	if err := os.Mkdir(filepath.Join(workspaceRoot, parent, "dup"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, loadSessionIndexFresh(t))

	rec := postCreateDirectory(t, s, api.CreateDirectoryRequest{Name: "dup", Path: parent})

	assertErrorBody(t, rec, http.StatusConflict, "Folder already exists")
}

// TestHandleCreateDirectoryMkdirError covers the 500 branch: os.Mkdir fails for
// a reason other than EEXIST (here EACCES from a read-only parent).
func TestHandleCreateDirectoryMkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission bits")
	}
	parent := writableWorkspaceParent(t)
	absParent := filepath.Join(workspaceRoot, parent)
	if err := os.Chmod(absParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(absParent, 0o755) })
	s := newTestServer(t, loadSessionIndexFresh(t))

	rec := postCreateDirectory(t, s, api.CreateDirectoryRequest{Name: "proj", Path: parent})

	assertErrorBody(t, rec, http.StatusInternalServerError, "failed to create directory")
}

// TestHandleCreateDirectoryGitInit covers the git-init block: with GitInit set,
// a successful `git init` leaves the folder with a .git dir and no warning.
func TestHandleCreateDirectoryGitInit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := writableWorkspaceParent(t)
	s := newTestServer(t, loadSessionIndexFresh(t))

	rec := postCreateDirectory(t, s, api.CreateDirectoryRequest{Name: "repo", Path: parent, GitInit: true})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp api.CreateDirectoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Warning != "" {
		t.Fatalf("warning = %q, want empty (git init should succeed)", resp.Warning)
	}
	gitDir := filepath.Join(workspaceRoot, parent, "repo", ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		t.Fatalf("git init did not create .git: info=%v err=%v", info, err)
	}
}

// TestHandleCreateDirectoryGitInitFailure covers the git-init failure branch:
// the folder is still created (201) but a warning is returned. A stub `git`
// that exits non-zero is placed first on PATH so the exec fails deterministically.
func TestHandleCreateDirectoryGitInitFailure(t *testing.T) {
	parent := writableWorkspaceParent(t)

	binDir := t.TempDir()
	stub := filepath.Join(binDir, "git")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, _ := exec.LookPath("git"); got != stub {
		t.Skipf("stub git not first on PATH (resolved %q); cannot force failure", got)
	}

	s := newTestServer(t, loadSessionIndexFresh(t))
	rec := postCreateDirectory(t, s, api.CreateDirectoryRequest{Name: "repo", Path: parent, GitInit: true})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (folder kept on git failure); body=%s", rec.Code, rec.Body.String())
	}
	var resp api.CreateDirectoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Warning != "git init failed" {
		t.Fatalf("warning = %q, want %q", resp.Warning, "git init failed")
	}
	if info, err := os.Stat(filepath.Join(workspaceRoot, parent, "repo")); err != nil || !info.IsDir() {
		t.Fatalf("folder should survive git failure: info=%v err=%v", info, err)
	}
}
