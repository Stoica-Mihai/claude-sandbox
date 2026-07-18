package main

import (
	"bytes"
	"fmt"
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

// TestSnapshotPlainText: written text shows up in the snapshot, which starts
// with a terminal reset.
func TestSnapshotPlainText(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("hello"))

	snap := ts.Snapshot()
	if !bytes.HasPrefix(snap, []byte(termReset)) {
		t.Fatalf("snapshot must start with a reset, got %q", snap)
	}
	if !bytes.Contains(snap, []byte("hello")) {
		t.Fatalf("snapshot missing written text: %q", snap)
	}
}

// TestSnapshotAltScreen: while the alt screen is active the snapshot re-enters
// it and paints alt content; after exit it paints main content again.
func TestSnapshotAltScreen(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("conv\x1b[?1049hTUI"))

	snap := ts.Snapshot()
	if !bytes.Contains(snap, []byte(altScreenEnterSeq)) {
		t.Fatalf("snapshot must re-enter the alt screen: %q", snap)
	}
	if !bytes.Contains(snap, []byte("TUI")) {
		t.Fatalf("snapshot missing alt-screen content: %q", snap)
	}
	if bytes.Contains(snap, []byte("conv")) {
		t.Fatalf("main-screen content painted while in alt screen: %q", snap)
	}

	ts.Write([]byte("\x1b[?1049lmore"))
	snap = ts.Snapshot()
	if bytes.Contains(snap, []byte(altScreenEnterSeq)) || bytes.Contains(snap, []byte("TUI")) {
		t.Fatalf("alt screen leaked into a main-screen snapshot: %q", snap)
	}
	if !bytes.Contains(snap, []byte("conv")) || !bytes.Contains(snap, []byte("more")) {
		t.Fatalf("snapshot missing main-screen content: %q", snap)
	}
}

// TestSnapshotSplitSequence: an escape sequence split across writes is parsed
// incrementally (the parser keeps state between chunks).
func TestSnapshotSplitSequence(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("ab\x1b[?10"))
	ts.Write([]byte("49hTUI"))

	if !bytes.Contains(ts.Snapshot(), []byte(altScreenEnterSeq)) {
		t.Fatal("split alt-screen enter sequence not recognized")
	}
}

// TestSnapshotScrollback: lines scrolled off the screen come back in the
// snapshot, before the screen paint.
func TestSnapshotScrollback(t *testing.T) {
	ts := newTermState(20, 3)
	defer ts.Close()
	for i := 1; i <= 6; i++ {
		ts.Write([]byte(fmt.Sprintf("line%d\r\n", i)))
	}

	snap := ts.Snapshot()
	for i := 1; i <= 6; i++ {
		if !bytes.Contains(snap, []byte(fmt.Sprintf("line%d", i))) {
			t.Fatalf("snapshot missing line%d: %q", i, snap)
		}
	}
	if bytes.Index(snap, []byte("line1")) > bytes.Index(snap, []byte("line6")) {
		t.Fatalf("scrollback must precede screen content: %q", snap)
	}
}

// TestSnapshotStyledOutput: SGR styling survives the render round-trip.
func TestSnapshotStyledOutput(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("\x1b[31mred\x1b[0m plain"))

	snap := ts.Snapshot()
	if !bytes.Contains(snap, []byte("red")) || !bytes.Contains(snap, []byte("plain")) {
		t.Fatalf("snapshot missing text: %q", snap)
	}
	if !bytes.Contains(snap, []byte("31")) {
		t.Fatalf("snapshot lost the color attribute: %q", snap)
	}
}

// TestSnapshotCursorHidden: DECTCEM hide is restored by the snapshot.
func TestSnapshotCursorHidden(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()

	if bytes.Contains(ts.Snapshot(), []byte("\x1b[?25l")) {
		t.Fatal("cursor hidden in a fresh snapshot")
	}
	ts.Write([]byte("\x1b[?25l"))
	if !bytes.Contains(ts.Snapshot(), []byte("\x1b[?25l")) {
		t.Fatal("snapshot must restore the hidden cursor")
	}
}

// TestSnapshotResize: the emulator follows resizes; content written after a
// resize lands on the wider screen.
func TestSnapshotResize(t *testing.T) {
	ts := newTermState(10, 3)
	defer ts.Close()
	ts.Resize(40, 10)
	ts.Write([]byte("a line wider than ten"))

	if !bytes.Contains(ts.Snapshot(), []byte("a line wider than ten")) {
		t.Fatalf("content lost after resize: %q", ts.Snapshot())
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

// TestRelayScrollbackReplay verifies a late viewer receives earlier output via
// the terminal snapshot.
func TestRelayScrollbackReplay(t *testing.T) {
	r, sessionSide := startTestRelay(t, "claude-test")

	if _, err := sessionSide.Write([]byte("early output")); err != nil {
		t.Fatal(err)
	}
	// Wait for the actor to ingest the output before attaching the viewer.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(r.term.Snapshot(), []byte("early output")) {
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
	case <-time.After(2 * time.Second):
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
