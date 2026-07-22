package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os/exec"
	"sync"
	"testing"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// startTestChatSession starts a chatSession actor against a real child
// process. Using /bin/cat (rather than the claude binary) keeps these tests
// fast and hermetic: cat echoes each stdin line back on stdout unchanged, so
// writing a line through one viewer and reading it back proves both the
// input→stdin and stdout→broadcast paths without any JSON interpretation.
func startTestChatSession(t *testing.T, cmd *exec.Cmd) *chatSession {
	t.Helper()
	s := newChatSession("claude-chat-test", "/workspace/x", "uuid-chat-test", time.Now())
	if err := s.begin(cmd); err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		s.Kill()
		<-s.Exited()
	})
	return s
}

func catCmd() *exec.Cmd { return exec.Command("cat") }

// attachTestChatViewer performs the ATTACH handshake over a net.Pipe and
// returns the client end.
func attachTestChatViewer(t *testing.T, s *chatSession, cols, rows uint16) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go s.readConn(server)
	if err := protocol.WriteJSONFrame(client, protocol.FrameAttach, protocol.Attach{Cols: cols, Rows: rows}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func readOneChatFrame(t *testing.T, conn net.Conn, timeout time.Duration) (byte, []byte, error) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	return protocol.ReadFrame(conn)
}

// TestChatSessionAttachSendsNoSnapshot: unlike the terminal kind, a chat
// ATTACH must not be answered with a SNAPSHOT frame — nothing at all should
// arrive until the child actually produces output.
func TestChatSessionAttachSendsNoSnapshot(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	client := attachTestChatViewer(t, s, 80, 24)

	if _, _, err := readOneChatFrame(t, client, 200*time.Millisecond); err == nil {
		t.Fatal("expected no frame immediately after attach (no snapshot for chat sessions)")
	}
}

// TestChatSessionZeroDimsAttachSucceeds: ATTACH with cols=0,rows=0 must still
// register the viewer (chat sessions have no PTY dimensions).
func TestChatSessionZeroDimsAttachSucceeds(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	client := attachTestChatViewer(t, s, 0, 0)

	if err := protocol.WriteFrame(client, protocol.FrameData, []byte(`{"line":"probe"}`)); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := readOneChatFrame(t, client, 3*time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != protocol.FrameData || !bytes.Contains(payload, []byte("probe")) {
		t.Fatalf("got type 0x%02x payload %q, want echoed DATA", typ, payload)
	}
}

// TestChatSessionBroadcastToMultipleViewers: one viewer's input, echoed by the
// child, must reach every attached viewer — pure broadcast, no suspension.
func TestChatSessionBroadcastToMultipleViewers(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	a := attachTestChatViewer(t, s, 80, 24)
	b := attachTestChatViewer(t, s, 80, 24)

	if err := protocol.WriteFrame(a, protocol.FrameData, []byte(`{"type":"user","text":"hi"}`)); err != nil {
		t.Fatal(err)
	}

	for _, v := range []net.Conn{a, b} {
		typ, payload, err := readOneChatFrame(t, v, 3*time.Second)
		if err != nil {
			t.Fatalf("viewer read: %v", err)
		}
		if typ != protocol.FrameData || !bytes.Contains(payload, []byte("hi")) {
			t.Fatalf("viewer got type 0x%02x payload %q, want echoed DATA", typ, payload)
		}
	}
}

// TestChatSessionInputOrderPreserved: multiple lines submitted in quick
// succession must reach the child (and so the viewer, once cat echoes them)
// in submission order — the ordering guarantee the actor's single command
// loop provides without a separate queue.
func TestChatSessionInputOrderPreserved(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	viewer := attachTestChatViewer(t, s, 80, 24)

	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	for _, l := range lines {
		if err := protocol.WriteFrame(viewer, protocol.FrameData, []byte(l)); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	deadline := time.Now().Add(5 * time.Second)
	for len(got) < len(lines) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out with only %v (want %v)", got, lines)
		}
		_, payload, err := readOneChatFrame(t, viewer, 3*time.Second)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got = append(got, string(payload))
	}
	for i, want := range lines {
		if got[i] != want {
			t.Fatalf("out-of-order relay: got %v, want %v", got, lines)
		}
	}
}

// TestChatSessionKillSendsClose verifies Kill delivers a CLOSE frame with
// reason "killed" and tears the session down.
func TestChatSessionKillSendsClose(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	client := attachTestChatViewer(t, s, 80, 24)

	s.Kill()

	for {
		typ, payload, err := readOneChatFrame(t, client, 3*time.Second)
		if err != nil {
			t.Fatalf("expected CLOSE frame: %v", err)
		}
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
		t.Fatal("chat session did not exit after Kill")
	}
}

// TestChatSessionEndsOnChildExit: when the child exits on its own (not via
// Kill), the session ends with reason "ended" and onExit fires.
func TestChatSessionEndsOnChildExit(t *testing.T) {
	s := newChatSession("claude-chat-gone", "/workspace/x", "uuid-gone", time.Now())
	exitCalls := make(chan struct{}, 2)
	s.onExit = func() { exitCalls <- struct{}{} }
	if err := s.begin(exec.Command("true")); err != nil {
		t.Fatalf("begin: %v", err)
	}

	client := attachTestChatViewer(t, s, 80, 24)

	for {
		typ, payload, err := readOneChatFrame(t, client, 5*time.Second)
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
		t.Fatal("chat session did not exit after child exit")
	}
	select {
	case <-exitCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("onExit was not called")
	}
	if s.send(chatCmdInput{data: []byte("x")}) {
		t.Fatal("send after exit should report failure")
	}
}

// TestChatSessionInfoReportsKind verifies the registry-visible metadata
// reports kind "chat".
func TestChatSessionInfoReportsKind(t *testing.T) {
	s := startTestChatSession(t, catCmd())
	info := s.info()
	if info.Kind != protocol.KindChat {
		t.Fatalf("info.Kind = %q, want %q", info.Kind, protocol.KindChat)
	}
	if info.Name != "claude-chat-test" || info.CWD != "/workspace/x" || info.UUID != "uuid-chat-test" {
		t.Fatalf("info = %+v", info)
	}
}

// TestChatSessionConcurrentAccessRaceFree hammers the actor API from many
// goroutines; run with -race to validate the actor isolation.
func TestChatSessionConcurrentAccessRaceFree(t *testing.T) {
	s := startTestChatSession(t, catCmd())

	fakeConns := make([]net.Conn, 2)
	for i := range fakeConns {
		c, _ := net.Pipe()
		defer c.Close()
		fakeConns[i] = c
	}

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			s.send(chatCmdInput{conn: fakeConns[i%2], data: []byte("x")})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			s.send(chatCmdDetach{conn: fakeConns[i%2]})
		}
	}()
	wg.Wait()
}
