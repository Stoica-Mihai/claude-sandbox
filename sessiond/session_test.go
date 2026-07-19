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
	// Non-blocking so os.File deadlines work: session.begin rewraps the PTY
	// side, but the program side is used directly by tests that set read
	// deadlines on it.
	_ = syscall.SetNonblock(fds[1], true)
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

// drainFrames discards a viewer's frames in the background until its
// connection errors (test end or eviction).
func drainFrames(conn net.Conn) {
	go func() {
		for {
			_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
			if _, _, err := protocol.ReadFrame(conn); err != nil {
				return
			}
		}
	}()
}

// assertMaxRenderedWidth strips escapes from a rendered snapshot (each escape
// becomes a row delimiter) and fails if any painted row exceeds cols.
func assertMaxRenderedWidth(t *testing.T, snap []byte, cols int) {
	t.Helper()
	stripped := regexp.MustCompile(`\x1b(\[[0-9;:?]*[a-zA-Z]|.)`).ReplaceAllString(string(snap), "\x00")
	for _, row := range strings.Split(stripped, "\x00") {
		for _, line := range strings.Split(row, "\r\n") {
			if len([]rune(line)) > cols {
				t.Fatalf("snapshot row wider than viewer (%d > %d): %q", len([]rune(line)), cols, line)
			}
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
	drainFrames(attachTestViewer(t, s, 100, 30)) // wide viewer stays drained

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
	assertMaxRenderedWidth(t, snap, 44)
}

// TestReactivateTakesLiveViewWithoutInput: a suspended viewer sending a
// reactivate control frame becomes active and gets a fresh snapshot, the
// previously active viewer is deactivated, and no byte reaches the PTY.
func TestReactivateTakesLiveViewWithoutInput(t *testing.T) {
	s, programSide := startTestSession(t)

	a := attachTestViewer(t, s, 100, 30)
	if typ, _ := readOneFrame(t, a); typ != protocol.FrameSnapshot {
		t.Fatalf("A: expected snapshot, got 0x%02x", typ)
	}

	// B attaching makes B active and suspends A (A gets a deactivated frame).
	b := attachTestViewer(t, s, 80, 24)
	if typ, _ := readOneFrame(t, b); typ != protocol.FrameSnapshot {
		t.Fatalf("B: expected snapshot, got 0x%02x", typ)
	}
	if typ, payload := readOneFrame(t, a); typ != protocol.FrameControl || !bytes.Contains(payload, []byte(protocol.ControlDeactivated)) {
		t.Fatalf("A: expected deactivated, got 0x%02x %q", typ, payload)
	}

	// A reactivates by focus — a control frame, never input.
	if err := protocol.WriteJSONFrame(a, protocol.FrameControl, protocol.Control{Type: protocol.ControlReactivate}); err != nil {
		t.Fatal(err)
	}

	if typ, _ := readOneFrame(t, a); typ != protocol.FrameSnapshot {
		t.Fatalf("A reactivate: expected fresh snapshot, got 0x%02x", typ)
	}
	if typ, payload := readOneFrame(t, b); typ != protocol.FrameControl || !bytes.Contains(payload, []byte(protocol.ControlDeactivated)) {
		t.Fatalf("B: expected deactivated after A reactivates, got 0x%02x %q", typ, payload)
	}

	// Nothing was ever written to the PTY.
	_ = programSide.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 16)
	if n, err := programSide.Read(buf); err == nil && n > 0 {
		t.Fatalf("reactivate wrote %d bytes to the PTY: %q", n, buf[:n])
	}
}

// TestSurvivingViewerResizeTakesOverLive: when the active viewer detaches, the
// remaining (suspended) viewer that resizes must take over live — it gets a
// fresh snapshot AND subsequent program output. Before the fix it went active
// but stayed suspended, so broadcast skipped it and the screen blackholed.
func TestSurvivingViewerResizeTakesOverLive(t *testing.T) {
	s, programSide := startTestSession(t)

	a := attachTestViewer(t, s, 100, 30)
	if typ, _ := readOneFrame(t, a); typ != protocol.FrameSnapshot {
		t.Fatalf("A: expected snapshot, got 0x%02x", typ)
	}

	// B attaching makes B active and suspends A.
	b := attachTestViewer(t, s, 80, 24)
	if typ, _ := readOneFrame(t, b); typ != protocol.FrameSnapshot {
		t.Fatalf("B: expected snapshot, got 0x%02x", typ)
	}
	if typ, payload := readOneFrame(t, a); typ != protocol.FrameControl || !bytes.Contains(payload, []byte(protocol.ControlDeactivated)) {
		t.Fatalf("A: expected deactivated, got 0x%02x %q", typ, payload)
	}

	// The active viewer leaves; A (still suspended) is the only viewer left.
	b.Close()

	// A resizes. Once the detach lands (active == nil) the resize makes A active
	// again and must produce a fresh snapshot. Retry to absorb detach/resize
	// ordering — resizes before the detach lands update size but send no frame.
	deadline := time.Now().Add(3 * time.Second)
	gotSnapshot := false
	for time.Now().Before(deadline) && !gotSnapshot {
		_ = protocol.WriteJSONFrame(a, protocol.FrameControl, protocol.Control{Type: protocol.ControlResize, Cols: 90, Rows: 28})
		_ = a.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if typ, _, err := protocol.ReadFrame(a); err == nil && typ == protocol.FrameSnapshot {
			gotSnapshot = true
		}
	}
	if !gotSnapshot {
		t.Fatal("A never got a snapshot after taking over via resize")
	}

	// The clincher: live output after the takeover must reach A.
	if _, err := programSide.Write([]byte("after takeover")); err != nil {
		t.Fatal(err)
	}
	readFrameUntil(t, a, "after takeover")
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
	drainFrames(viewer)

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
	assertMaxRenderedWidth(t, s.term.Snapshot(), 44)

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

// TestInputWriteDeadlineKeepsActorAlive: a program that stops reading stdin
// (unread PTY slave) must fail the input write with an error CONTROL frame to
// the sender while the actor keeps serving output.
func TestInputWriteDeadlineKeepsActorAlive(t *testing.T) {
	prev := inputWriteTimeout
	inputWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { inputWriteTimeout = prev })

	ptmx, pts, err := ptyOpen()
	if err != nil {
		t.Skipf("pty open: %v", err)
	}
	t.Cleanup(func() { ptmx.Close(); pts.Close() })

	s := newSession("claude-stuck", "/workspace/x", "uuid-s", time.Now())
	s.begin(ptmx, nil)
	t.Cleanup(func() {
		s.Kill()
		<-s.Exited()
	})

	client := attachTestViewer(t, s, 80, 24)
	if typ, _ := readOneFrame(t, client); typ != protocol.FrameSnapshot {
		t.Fatalf("expected snapshot, got 0x%02x", typ)
	}

	// Nobody reads the slave. One oversize newline-terminated burst (the PTY
	// boots in canonical mode, where only line-terminated input queues)
	// overfills the input queue; the deadline fires and the error comes back
	// as a CONTROL frame.
	if err := protocol.WriteFrame(client, protocol.FrameData, bytes.Repeat([]byte("xxxxxxx\n"), 8<<10)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = client.SetReadDeadline(deadline)
		typ, payload, err := protocol.ReadFrame(client)
		if err != nil {
			t.Fatalf("input-deadline error never surfaced: %v", err)
		}
		if typ == protocol.FrameControl && bytes.Contains(payload, []byte(`"error"`)) {
			break
		}
	}

	// Actor must still be responsive: slave-side output reaches a fresh viewer.
	if _, err := pts.Write([]byte("still alive")); err != nil {
		t.Fatal(err)
	}
	fresh := attachTestViewer(t, s, 80, 24)
	readFrameUntil(t, fresh, "still alive")
}

// TestSlowViewerEvicted: a viewer that never drains its connection is evicted
// once its queue fills, and the actor keeps serving other viewers.
func TestSlowViewerEvicted(t *testing.T) {
	s, programSide := startTestSession(t)

	// stuck viewer: attach, then never read — its writer blocks on the pipe,
	// its queue fills, the actor evicts it.
	stuck := attachTestViewer(t, s, 80, 24)
	_ = stuck

	healthy := attachTestViewer(t, s, 80, 24)
	drainFrames(healthy)

	// Flood well past viewerQueueSize; the actor must never block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < viewerQueueSize+64; i++ {
			if _, err := programSide.Write([]byte("spam\n")); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("actor wedged while a viewer was not draining")
	}

	// Input from a live viewer still round-trips (actor responsive).
	got := make(chan struct{})
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := programSide.Read(buf)
			if err != nil {
				return
			}
			if bytes.Contains(buf[:n], []byte("ping")) {
				close(got)
				return
			}
		}
	}()
	if err := protocol.WriteFrame(healthy, protocol.FrameData, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("input from healthy viewer did not reach the program")
	}
}
