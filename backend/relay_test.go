package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTrackAltScreenRoutesSegments(t *testing.T) {
	r := NewRelay("claude-test")
	segments := r.trackAltScreen([]byte("conv\x1b[?1049hTUI\x1b[?1049lmore"))

	if r.inAltScreen {
		t.Fatal("inAltScreen should be false after exit sequence")
	}

	var ring bytes.Buffer
	for _, s := range segments {
		ring.Write(s)
	}
	// Normal-mode content goes to the ring buffer; TUI chrome does not.
	if !bytes.Contains(ring.Bytes(), []byte("conv")) || !bytes.Contains(ring.Bytes(), []byte("more")) {
		t.Fatalf("normal segments missing conversation content: %q", ring.Bytes())
	}
	if bytes.Contains(ring.Bytes(), []byte("TUI")) {
		t.Fatalf("alt-screen content leaked into ring segments: %q", ring.Bytes())
	}
}

func TestTrackAltScreenSplitSequence(t *testing.T) {
	r := NewRelay("claude-test")
	// Enter sequence split across two chunks.
	r.trackAltScreen([]byte("ab\x1b[?10"))
	if r.inAltScreen {
		t.Fatal("should not toggle on partial sequence")
	}
	r.trackAltScreen([]byte("49hTUI"))
	if !r.inAltScreen {
		t.Fatal("should be in alt screen after completing split sequence")
	}
}

// socketpairFiles returns a connected bidirectional *os.File pair standing in
// for the attach PTY (relay side, session side).
func socketpairFiles(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	a := os.NewFile(uintptr(fds[0]), "relay-side")
	b := os.NewFile(uintptr(fds[1]), "session-side")
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// startTestRelay runs a relay actor against an injected attach file and stops
// it on test cleanup.
func startTestRelay(t *testing.T, name string) (*Relay, *os.File) {
	t.Helper()
	relaySide, sessionSide := socketpairFiles(t)
	r := NewRelay(name)
	r.begin(relaySide, nil)
	t.Cleanup(func() {
		r.Stop()
		<-r.exited
	})
	return r, sessionSide
}

// dialTestViewer upgrades a real WebSocket pair and registers the server side
// with the relay. Returns the client side.
func dialTestViewer(t *testing.T, r *Relay) *websocket.Conn {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		if err := r.AddViewer(conn); err != nil {
			t.Errorf("AddViewer: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// readUntil reads client messages until the collected binary payload contains
// want, failing on timeout.
func readUntil(t *testing.T, client *websocket.Conn, want string) {
	t.Helper()
	var got bytes.Buffer
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = client.SetReadDeadline(deadline)
		msgType, data, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("read (have %q, want %q): %v", got.String(), want, err)
		}
		if msgType == websocket.BinaryMessage {
			got.Write(data)
		}
		if bytes.Contains(got.Bytes(), []byte(want)) {
			return
		}
	}
}

// TestRelayBroadcastAndInput drives the full path: session output reaches a
// viewer (after the reset replay) and viewer input reaches the session.
func TestRelayBroadcastAndInput(t *testing.T) {
	r, sessionSide := startTestRelay(t, "claude-test")
	client := dialTestViewer(t, r)

	if _, err := sessionSide.Write([]byte("hello from session")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, client, "hello from session")

	// Input path goes through the actor to the attach file.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64)
		n, err := sessionSide.Read(buf)
		if err != nil || string(buf[:n]) != "typed" {
			t.Errorf("session read = %q, %v", buf[:n], err)
		}
	}()
	// Feed input through the relay API as the ws handler would.
	if err := r.Input(nil, []byte("typed")); err != nil {
		t.Fatalf("Input: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session never received input")
	}
}

// TestRelayScrollbackReplay verifies a late viewer receives ring-buffer history.
func TestRelayScrollbackReplay(t *testing.T) {
	r, sessionSide := startTestRelay(t, "claude-test")

	if _, err := sessionSide.Write([]byte("early output")); err != nil {
		t.Fatal(err)
	}
	// Wait for the actor to ingest the output before attaching the viewer.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(r.ring.Bytes(), []byte("early output")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := dialTestViewer(t, r)
	readUntil(t, client, "early output")
}

// TestRelayStopClosesViewers verifies Stop sends a normal close to viewers.
func TestRelayStopClosesViewers(t *testing.T) {
	r, _ := startTestRelay(t, "claude-test")
	client := dialTestViewer(t, r)

	// Drain the replay, then stop.
	readUntil(t, client, termReset)
	r.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = client.SetReadDeadline(deadline)
		_, _, err := client.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatalf("expected normal closure, got %v", err)
			}
			return
		}
	}
}

// TestRelayStopsWhenSessionGone: attach EOF with no live session behind it
// must stop the relay and fire onExit exactly once.
func TestRelayStopsWhenSessionGone(t *testing.T) {
	setSessionDirs(t) // empty dirs: sessionAlive() is false
	relaySide, sessionSide := socketpairFiles(t)

	r := NewRelay("claude-gone")
	exitCalls := make(chan struct{}, 2)
	r.onExit = func() { exitCalls <- struct{}{} }
	r.begin(relaySide, nil)

	sessionSide.Close() // attach EOF → reconnect → session gone → stop

	select {
	case <-r.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not stop after session vanished")
	}
	if !r.IsStopped() {
		t.Fatal("IsStopped should be true")
	}
	select {
	case <-exitCalls:
	default:
		t.Fatal("onExit was not called")
	}
	select {
	case <-exitCalls:
		t.Fatal("onExit called more than once")
	default:
	}

	// Post-exit API calls fail cleanly rather than hanging.
	if err := r.Input(nil, []byte("x")); err == nil {
		t.Fatal("Input after exit should error")
	}
}

// TestRelayConcurrentAccessRaceFree hammers the public API from many
// goroutines; run with -race to validate the actor isolation.
func TestRelayConcurrentAccessRaceFree(t *testing.T) {
	r, sessionSide := startTestRelay(t, "claude-test")
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := sessionSide.Read(buf); err != nil {
				return
			}
		}
	}()

	fakeConns := []*websocket.Conn{{}, {}}
	const iters = 300
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = r.Input(fakeConns[i%2], []byte("x"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			r.Resize(fakeConns[i%2], uint16(80+i%20), 24)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if _, err := sessionSide.Write([]byte("out\x1b[?1049hT\x1b[?1049lz")); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			r.RemoveViewer(fakeConns[i%2])
			_ = r.IsStopped()
		}
	}()

	wg.Wait()
}
