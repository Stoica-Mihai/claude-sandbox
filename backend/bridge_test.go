package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-sandbox-sessiond/protocol"

	"github.com/gorilla/websocket"
)

// fakeSessionSocket is a scripted sessiond session endpoint: it records the
// frames it receives and plays queued responses after the ATTACH handshake.
type fakeSessionSocket struct {
	t        *testing.T
	ln       net.Listener
	attached chan protocol.Attach
	frames   chan struct {
		typ     byte
		payload []byte
	}
	sendClose chan string // reason to send as a CLOSE frame
}

func newFakeSessionSocket(t *testing.T) *fakeSessionSocket {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "claude-fake.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSessionSocket{
		t:        t,
		ln:       ln,
		attached: make(chan protocol.Attach, 1),
		frames: make(chan struct {
			typ     byte
			payload []byte
		}, 16),
		sendClose: make(chan string, 1),
	}
	t.Cleanup(func() { ln.Close() })
	go f.serve()
	return f
}

func (f *fakeSessionSocket) dial(string) (net.Conn, error) {
	return net.DialTimeout("unix", f.ln.Addr().String(), 2*time.Second)
}

func (f *fakeSessionSocket) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	typ, payload, err := protocol.ReadFrame(conn)
	if err != nil || typ != protocol.FrameAttach {
		f.t.Errorf("first frame: typ=0x%02x err=%v, want ATTACH", typ, err)
		return
	}
	var att protocol.Attach
	if err := json.Unmarshal(payload, &att); err != nil {
		f.t.Errorf("attach payload: %v", err)
		return
	}
	f.attached <- att

	// Reply with a snapshot, as sessiond does.
	if err := protocol.WriteFrame(conn, protocol.FrameSnapshot, []byte("\x1bcSNAPSHOT-CONTENT")); err != nil {
		return
	}

	// Then: forward CLOSE when scripted, and record incoming frames.
	go func() {
		reason := <-f.sendClose
		payload, _ := json.Marshal(protocol.Close{Reason: reason})
		_ = protocol.WriteFrame(conn, protocol.FrameClose, payload)
	}()
	for {
		typ, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			return
		}
		f.frames <- struct {
			typ     byte
			payload []byte
		}{typ, payload}
	}
}

// dialBridgeWS serves the real handleWebSocket over httptest and dials it.
func dialBridgeWS(t *testing.T, s *Server) *websocket.Conn {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocket)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal/claude-b1"
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func newBridgeServer(t *testing.T, dial func(string) (net.Conn, error)) *Server {
	t.Helper()
	sm, fh := newTestManager(loadSessionIndexFresh(t))
	fh.dial = dial
	sm.store.add(sessionRecord{Name: "claude-b1", CWD: "/workspace/a", SessionID: testUUID1})
	return &Server{sm: sm, broker: NewBroker(), upgrader: websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}}
}

// TestBridgeAttachSnapshotInputClose drives the full bridge: resize → ATTACH
// with dims → snapshot arrives as binary; typed input → DATA frame; resize →
// CONTROL frame; scripted CLOSE → normal WS closure.
func TestBridgeAttachSnapshotInputClose(t *testing.T) {
	fake := newFakeSessionSocket(t)
	s := newBridgeServer(t, fake.dial)
	client := dialBridgeWS(t, s)

	// First resize becomes the ATTACH handshake.
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case att := <-fake.attached:
		if att.Cols != 120 || att.Rows != 30 {
			t.Fatalf("attach dims = %+v", att)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge never attached")
	}

	// Snapshot arrives as a binary message.
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	msgType, data, err := client.ReadMessage()
	if err != nil || msgType != websocket.BinaryMessage || !strings.Contains(string(data), "SNAPSHOT-CONTENT") {
		t.Fatalf("snapshot: type=%d data=%q err=%v", msgType, data, err)
	}

	// Input flows as DATA; a later resize flows as CONTROL.
	if err := client.WriteMessage(websocket.BinaryMessage, []byte("typed")); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":90,"rows":22}`)); err != nil {
		t.Fatal(err)
	}
	wantFrames := []struct {
		typ     byte
		payload string
	}{
		{protocol.FrameData, "typed"},
		{protocol.FrameControl, `{"type":"resize","cols":90,"rows":22}`},
	}
	for _, want := range wantFrames {
		select {
		case fr := <-fake.frames:
			if fr.typ != want.typ || string(fr.payload) != want.payload {
				t.Fatalf("frame = 0x%02x %q, want 0x%02x %q", fr.typ, fr.payload, want.typ, want.payload)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("frame %q never arrived", want.payload)
		}
	}

	// CLOSE maps to a normal WS closure.
	fake.sendClose <- protocol.CloseEnded
	for {
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := client.ReadMessage(); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatalf("expected normal closure, got %v", err)
			}
			return
		}
	}
}

// TestBridgeBuffersInputBeforeAttach: binary input sent before the first
// resize is held and flushed right after the ATTACH.
func TestBridgeBuffersInputBeforeAttach(t *testing.T) {
	fake := newFakeSessionSocket(t)
	s := newBridgeServer(t, fake.dial)
	client := dialBridgeWS(t, s)

	if err := client.WriteMessage(websocket.BinaryMessage, []byte("early")); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":80,"rows":24}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case <-fake.attached:
	case <-time.After(3 * time.Second):
		t.Fatal("bridge never attached")
	}
	select {
	case fr := <-fake.frames:
		if fr.typ != protocol.FrameData || string(fr.payload) != "early" {
			t.Fatalf("frame = 0x%02x %q, want buffered input", fr.typ, fr.payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("buffered input never flushed")
	}
}

// TestBridgeDialFailureClosesAbnormally: a failed session dial after the
// upgrade closes the WS with a non-1000 code so the client retries.
func TestBridgeDialFailureClosesAbnormally(t *testing.T) {
	s := newBridgeServer(t, func(string) (net.Conn, error) {
		return nil, errors.New("sessiond down")
	})
	client := dialBridgeWS(t, s)

	for {
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := client.ReadMessage(); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatal("dial failure must not close normally (client would not retry)")
			}
			return
		}
	}
}

// TestBridgeUnknownSession404s before upgrading.
func TestBridgeUnknownSession404s(t *testing.T) {
	sm, _ := newTestManager(loadSessionIndexFresh(t))
	s := &Server{sm: sm, broker: NewBroker()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocket)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal/claude-nope"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("dial to unknown session must fail")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %v, want 404", resp)
	}
}
