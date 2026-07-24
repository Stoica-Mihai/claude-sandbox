package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "claude-sandbox-api"
)

func newTestServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	srv := &server{store: newStore(dir, 100)}
	mux := http.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleHealthz(t *testing.T) {
	ts := newTestServer(t, t.TempDir())
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("healthz = %d %q", resp.StatusCode, body)
	}
}

func TestHandleQueryParamsAndEmptyArray(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeLine(t, filepath.Join(dir, "backend.log"), base.Add(time.Second), "INFO", "hello-query")
	ts := newTestServer(t, dir)

	// Empty result set encodes as [] (not null).
	resp, err := http.Get(ts.URL + "/api/logs?service=nonesuch")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("empty query body = %q, want []", body)
	}

	resp, err = http.Get(ts.URL + "/api/logs?service=backend&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var recs []api.LogRecord
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 1 || recs[0].Msg != "hello-query" {
		t.Fatalf("query = %v, want one hello-query record", msgsOf(recs))
	}
}

func TestHandleStreamReplayThenLive(t *testing.T) {
	dir := t.TempDir()
	srv := &server{store: newStore(dir, 100)}
	mux := http.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	srv.store.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "replay-line"})

	resp, err := http.Get(ts.URL + "/api/logs/stream?service=backend")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	lines := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				lines <- strings.TrimPrefix(l, "data: ")
			}
		}
	}()

	// The replayed record arrives from the ring; then a live add streams through.
	if got := waitData(t, lines); !strings.Contains(got, "replay-line") {
		t.Fatalf("first SSE data = %q, want replay-line", got)
	}
	srv.store.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "live-line"})
	if got := waitData(t, lines); !strings.Contains(got, "live-line") {
		t.Fatalf("second SSE data = %q, want live-line", got)
	}
}

func waitData(t *testing.T, lines <-chan string) string {
	t.Helper()
	select {
	case l := <-lines:
		return l
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE data")
		return ""
	}
}
