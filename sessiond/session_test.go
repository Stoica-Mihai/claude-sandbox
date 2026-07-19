package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// socketpairFiles returns a connected bidirectional *os.File pair standing in
// for the PTY (session side plays the program).
func socketpairFiles(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	a := os.NewFile(uintptr(fds[0]), "pty-side")
	b := os.NewFile(uintptr(fds[1]), "program-side")
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// startTestSession runs a session actor against an injected PTY stand-in.
func startTestSession(t *testing.T) (*session, *os.File) {
	t.Helper()
	ptySide, programSide := socketpairFiles(t)
	s := newSession("claude-test", "/workspace/x", "uuid-test", time.Now())
	s.begin(ptySide, nil)
	t.Cleanup(func() {
		s.Kill()
		<-s.Exited()
	})
	return s, programSide
}

// attachTestViewer performs the ATTACH handshake over a net.Pipe and returns
// the client end.
func attachTestViewer(t *testing.T, s *session, cols, rows uint16) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go s.readConn(server)
	if err := protocol.WriteJSONFrame(client, protocol.FrameAttach, protocol.Attach{Cols: cols, Rows: rows}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// readFrameUntil reads frames until the collected DATA/SNAPSHOT payload
// contains want, failing on timeout.
func readFrameUntil(t *testing.T, conn net.Conn, want string) {
	t.Helper()
	var got bytes.Buffer
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		typ, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read (have %q, want %q): %v", got.String(), want, err)
		}
		if typ == protocol.FrameData || typ == protocol.FrameSnapshot {
			got.Write(payload)
		}
		if bytes.Contains(got.Bytes(), []byte(want)) {
			return
		}
	}
}

// readOneFrame reads a single frame with a deadline.
func readOneFrame(t *testing.T, conn net.Conn) (byte, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	typ, payload, err := protocol.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return typ, payload
}

// TestSessionBroadcastAndInput drives the full path: program output reaches a
// viewer (after the snapshot) and viewer input reaches the program.
func TestSessionBroadcastAndInput(t *testing.T) {
	s, programSide := startTestSession(t)
	client := attachTestViewer(t, s, 100, 30)

	if _, err := programSide.Write([]byte("hello from session")); err != nil {
		t.Fatal(err)
	}
	readFrameUntil(t, client, "hello from session")

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64)
		n, err := programSide.Read(buf)
		if err != nil || string(buf[:n]) != "typed" {
			t.Errorf("program read = %q, %v", buf[:n], err)
		}
	}()
	if err := protocol.WriteFrame(client, protocol.FrameData, []byte("typed")); err != nil {
		t.Fatalf("input write: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("program never received input")
	}
}

// TestSessionScrollbackReplay verifies a late viewer receives earlier output
// via the terminal snapshot.
func TestSessionScrollbackReplay(t *testing.T) {
	s, programSide := startTestSession(t)

	if _, err := programSide.Write([]byte("early output")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(s.term.Snapshot(), []byte("early output")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := attachTestViewer(t, s, 100, 30)
	readFrameUntil(t, client, "early output")
}

// TestSnapshotRenderedAtJoiningViewerWidth: a narrower viewer joining a
// session sized for a wider one gets a snapshot at its own width — no painted
// row may exceed it, or the client terminal wraps every row.
func TestSnapshotRenderedAtJoiningViewerWidth(t *testing.T) {
	s, programSide := startTestSession(t)
	wideViewer := attachTestViewer(t, s, 100, 30)
	go func() { // keep the wide viewer's pipe drained
		for {
			_ = wideViewer.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := protocol.ReadFrame(wideViewer); err != nil {
				return
			}
		}
	}()

	wide := strings.Repeat("X", 80)
	if _, err := programSide.Write([]byte(wide)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !bytes.Contains(s.term.Snapshot(), []byte("XXX")) {
		time.Sleep(10 * time.Millisecond)
	}

	narrow := attachTestViewer(t, s, 44, 48)
	typ, snap := readOneFrame(t, narrow)
	if typ != protocol.FrameSnapshot {
		t.Fatalf("first frame must be the snapshot, got type 0x%02x", typ)
	}
	if !bytes.HasPrefix(snap, []byte(termReset)) {
		t.Fatalf("snapshot must start with a reset, got %q", snap[:min(len(snap), 40)])
	}
	// Strip escapes (each becomes a row delimiter); every painted row must fit
	// the narrow width.
	stripped := regexp.MustCompile(`\x1b(\[[0-9;:?]*[a-zA-Z]|.)`).ReplaceAllString(string(snap), "\x00")
	for _, row := range strings.Split(stripped, "\x00") {
		for _, line := range strings.Split(row, "\r\n") {
			if len([]rune(line)) > 44 {
				t.Fatalf("snapshot row wider than viewer (%d > 44): %q", len([]rune(line)), line)
			}
		}
	}
}

// TestSessionKillSendsClose verifies Kill delivers a CLOSE frame with reason
// "killed" and tears the session down.
func TestSessionKillSendsClose(t *testing.T) {
	s, _ := startTestSession(t)
	client := attachTestViewer(t, s, 80, 24)

	typ, _ := readOneFrame(t, client)
	if typ != protocol.FrameSnapshot {
		t.Fatalf("expected snapshot first, got 0x%02x", typ)
	}

	s.Kill()

	for {
		typ, payload := readOneFrame(t, client)
		if typ != protocol.FrameClose {
			continue
		}
		var cl protocol.Close
		if err := json.Unmarshal(payload, &cl); err != nil || cl.Reason != protocol.CloseKilled {
			t.Fatalf("close payload = %s, err %v", payload, err)
		}
		break
	}
	select {
	case <-s.Exited():
	case <-time.After(3 * time.Second):
		t.Fatal("session did not exit after Kill")
	}
}

// TestSessionEndsOnPTYEOF: the PTY closing (program gone) ends the session
// with reason "ended", fires onExit exactly once, and post-exit sends fail.
func TestSessionEndsOnPTYEOF(t *testing.T) {
	ptySide, programSide := socketpairFiles(t)
	s := newSession("claude-gone", "/workspace/x", "uuid-x", time.Now())
	exitCalls := make(chan struct{}, 2)
	s.onExit = func() { exitCalls <- struct{}{} }
	s.begin(ptySide, nil)

	client := attachTestViewer(t, s, 80, 24)
	typ, _ := readOneFrame(t, client)
	if typ != protocol.FrameSnapshot {
		t.Fatalf("expected snapshot, got 0x%02x", typ)
	}

	programSide.Close()

	for {
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		typ, payload, err := protocol.ReadFrame(client)
		if err != nil {
			t.Fatalf("expected CLOSE frame before stream end: %v", err)
		}
		if typ != protocol.FrameClose {
			continue
		}
		var cl protocol.Close
		if err := json.Unmarshal(payload, &cl); err != nil || cl.Reason != protocol.CloseEnded {
			t.Fatalf("close payload = %s, err %v", payload, err)
		}
		break
	}

	select {
	case <-s.Exited():
	case <-time.After(3 * time.Second):
		t.Fatal("session did not exit after PTY EOF")
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
	if s.send(cmdInput{data: []byte("x")}) {
		t.Fatal("send after exit should report failure")
	}
}

// TestStaleResizeCannotSteerPTY is the lastResizer regression test: a resize
// command from a connection that is not (or no longer) a registered viewer
// must neither become the active viewer nor change the terminal size.
func TestStaleResizeCannotSteerPTY(t *testing.T) {
	s, programSide := startTestSession(t)

	viewer := attachTestViewer(t, s, 44, 10)
	go func() { // drain the real viewer
		for {
			_ = viewer.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := protocol.ReadFrame(viewer); err != nil {
				return
			}
		}
	}()

	// A conn that was never registered (same shape as one already evicted:
	// the detach ran, its resize command is still in flight).
	stale, _ := net.Pipe()
	defer stale.Close()
	s.send(cmdResize{conn: stale, cols: 200, rows: 50})

	// Give the actor time to process, then verify rendering width: 100 X's on
	// a 44-col emulator must wrap; at 200 cols they would fit one row.
	if _, err := programSide.Write([]byte(strings.Repeat("X", 100))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !bytes.Contains(s.term.Snapshot(), []byte("X")) {
		time.Sleep(10 * time.Millisecond)
	}
	stripped := regexp.MustCompile(`\x1b(\[[0-9;:?]*[a-zA-Z]|.)`).ReplaceAllString(string(s.term.Snapshot()), "\x00")
	for _, row := range strings.Split(stripped, "\x00") {
		for _, line := range strings.Split(row, "\r\n") {
			if len([]rune(line)) > 44 {
				t.Fatalf("stale resize changed the emulator width (row len %d): %q", len([]rune(line)), line)
			}
		}
	}

	// The registered viewer must still be able to resize (it is the active one).
	if err := protocol.WriteJSONFrame(viewer, protocol.FrameControl, protocol.Control{Type: "resize", Cols: 60, Rows: 20}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if w := s.termWidth(); w == 60 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("registered active viewer's resize was not applied")
}

// termWidth probes the emulator's current width (term is mutex-guarded).
func (s *session) termWidth() int {
	s.term.mu.Lock()
	defer s.term.mu.Unlock()
	return s.term.emu.Width()
}

// TestSessionConcurrentAccessRaceFree hammers the actor API from many
// goroutines; run with -race to validate the actor isolation.
func TestSessionConcurrentAccessRaceFree(t *testing.T) {
	s, programSide := startTestSession(t)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := programSide.Read(buf); err != nil {
				return
			}
		}
	}()

	fakeConns := make([]net.Conn, 2)
	for i := range fakeConns {
		c, _ := net.Pipe()
		defer c.Close()
		fakeConns[i] = c
	}

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			s.send(cmdInput{conn: fakeConns[i%2], data: []byte("x")})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			s.send(cmdResize{conn: fakeConns[i%2], cols: uint16(80 + i%20), rows: 24})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if _, err := programSide.Write([]byte("out\x1b[?1049hT\x1b[?1049lz")); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			s.send(cmdDetach{conn: fakeConns[i%2]})
		}
	}()
	wg.Wait()
}
