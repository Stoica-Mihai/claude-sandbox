package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "claude-sandbox-api"
	"claude-sandbox-sessiond/protocol"

	"github.com/gorilla/websocket"
)

// fakeChatSessionSocket is a scripted sessiond chat session endpoint: unlike
// fakeSessionSocket (terminal), it sends no snapshot after ATTACH — the chat
// bridge's ATTACH carries no dimensions and expects none in reply.
type fakeChatSessionSocket struct {
	t         *testing.T
	ln        net.Listener
	attached  chan struct{}
	frames    chan frame
	out       chan []byte // lines the fake pushes to the bridge as FrameData
	sendClose chan string // reason to send as a CLOSE frame
}

func newFakeChatSessionSocket(t *testing.T) *fakeChatSessionSocket {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "claude-chat-fake.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeChatSessionSocket{
		t:         t,
		ln:        ln,
		attached:  make(chan struct{}, 1),
		frames:    make(chan frame, 16),
		out:       make(chan []byte, 16),
		sendClose: make(chan string, 1),
	}
	t.Cleanup(func() { ln.Close() })
	go f.serve()
	return f
}

func (f *fakeChatSessionSocket) dial(string) (net.Conn, error) {
	return net.DialTimeout("unix", f.ln.Addr().String(), 2*time.Second)
}

func (f *fakeChatSessionSocket) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	typ, _, err := protocol.ReadFrame(conn)
	if err != nil || typ != protocol.FrameAttach {
		f.t.Errorf("first frame: typ=0x%02x err=%v, want ATTACH", typ, err)
		return
	}
	f.attached <- struct{}{}

	go func() {
		for line := range f.out {
			if err := protocol.WriteFrame(conn, protocol.FrameData, line); err != nil {
				return
			}
		}
	}()
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
		f.frames <- frame{typ, payload}
	}
}

func newChatBridgeServer(t *testing.T, dial func(string) (net.Conn, error)) (*Server, *SessionManager) {
	t.Helper()
	sm, fh := newTestManager(t, loadSessionIndexFresh(t))
	fh.dial = dial
	sm.store.add(sessionRecord{Name: "claude-c1", CWD: "/workspace/a", SessionID: testUUID1, Kind: api.SessionKindChat})
	return &Server{sm: sm, broker: NewBroker(), upgrader: websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}}, sm
}

func dialChatBridgeWS(t *testing.T, s *Server) *websocket.Conn {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/terminal/{terminalId}", s.handleWebSocket)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal/claude-c1"
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func waitAttached(t *testing.T, attached chan struct{}) {
	t.Helper()
	select {
	case <-attached:
	case <-time.After(3 * time.Second):
		t.Fatal("chat bridge never attached")
	}
}

// TestChatBridgeAttachesImmediatelyNoSnapshot: the chat bridge sends ATTACH
// right away (no dims to wait for) and the client gets no snapshot frame —
// the first (and only) thing it sees is the event line itself, as text.
func TestChatBridgeAttachesImmediatelyNoSnapshot(t *testing.T) {
	fake := newFakeChatSessionSocket(t)
	s, _ := newChatBridgeServer(t, fake.dial)
	client := dialChatBridgeWS(t, s)
	waitAttached(t, fake.attached)

	fake.out <- []byte(`{"type":"assistant","text":"hi"}`)
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	msgType, data, err := client.ReadMessage()
	if err != nil || msgType != websocket.TextMessage || !strings.Contains(string(data), "hi") {
		t.Fatalf("event: type=%d data=%q err=%v, want TextMessage containing the event", msgType, data, err)
	}
}

// TestChatBridgeInputForwardsAsTextData: a WS TextMessage from the client
// (chat input) reaches sessiond as a FrameData frame.
func TestChatBridgeInputForwardsAsTextData(t *testing.T) {
	fake := newFakeChatSessionSocket(t)
	s, _ := newChatBridgeServer(t, fake.dial)
	client := dialChatBridgeWS(t, s)
	waitAttached(t, fake.attached)

	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"user","text":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case fr := <-fake.frames:
		if fr.typ != protocol.FrameData || !strings.Contains(string(fr.payload), "hello") {
			t.Fatalf("frame = 0x%02x %q, want FrameData containing input", fr.typ, fr.payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("input never forwarded")
	}
}

// TestChatBridgeReKeysOnConversationReset: a conversation_reset event followed
// by a system/init event must re-key both the session index and the live
// store record, using the init event's session_id — never new_conversation_id
// (verified unreliable against the pinned engine, see design.md decision 7).
func TestChatBridgeReKeysOnConversationReset(t *testing.T) {
	fake := newFakeChatSessionSocket(t)
	s, sm := newChatBridgeServer(t, fake.dial)
	sm.index.add(testUUID1, "/workspace/a", 100)
	client := dialChatBridgeWS(t, s)
	waitAttached(t, fake.attached)

	newUUID := "33333333-3333-4333-8333-333333333333"
	unreliableUUID := "99999999-9999-4999-8999-999999999999"
	fake.out <- []byte(`{"type":"conversation_reset","session_id":"` + testUUID1 + `","new_conversation_id":"` + unreliableUUID + `"}`)
	fake.out <- []byte(`{"type":"system","subtype":"init","session_id":"` + newUUID + `"}`)

	// Drain the two forwarded lines from the client side.
	for i := 0; i < 2; i++ {
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := client.ReadMessage(); err != nil {
			t.Fatalf("read event %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := sm.store.byUUID(newUUID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("live store record was never re-keyed to the new uuid")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := sm.store.byUUID(testUUID1); ok {
		t.Fatal("live store record still keyed to the old uuid")
	}
	if _, ok := sm.store.byUUID(unreliableUUID); ok {
		t.Fatal("must never re-key to new_conversation_id — it is not reliable")
	}
	if cwd, ok := sm.index.cwd(newUUID); !ok || cwd != "/workspace/a" {
		t.Fatalf("session index not re-keyed: cwd=%q ok=%v", cwd, ok)
	}
	if _, ok := sm.index.cwd(testUUID1); ok {
		t.Fatal("session index still has the old uuid entry")
	}
}

// TestChatBridgeUnrelatedEventsDoNotRekey: an ordinary event (not
// conversation_reset/init) must never trigger any index/store mutation.
func TestChatBridgeUnrelatedEventsDoNotRekey(t *testing.T) {
	fake := newFakeChatSessionSocket(t)
	s, sm := newChatBridgeServer(t, fake.dial)
	sm.index.add(testUUID1, "/workspace/a", 100)
	client := dialChatBridgeWS(t, s)
	waitAttached(t, fake.attached)

	fake.out <- []byte(`{"type":"assistant","text":"unrelated"}`)
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := client.ReadMessage(); err != nil {
		t.Fatalf("read: %v", err)
	}

	if _, ok := sm.store.byUUID(testUUID1); !ok {
		t.Fatal("unrelated event must not disturb the live record's uuid")
	}
	if cwd, ok := sm.index.cwd(testUUID1); !ok || cwd != "/workspace/a" {
		t.Fatal("unrelated event must not disturb the session index")
	}
}

// TestChatBridgeCloseMapsToNormalClosure: a CLOSE frame from sessiond closes
// the chat WS normally, same as the terminal bridge.
func TestChatBridgeCloseMapsToNormalClosure(t *testing.T) {
	fake := newFakeChatSessionSocket(t)
	s, _ := newChatBridgeServer(t, fake.dial)
	client := dialChatBridgeWS(t, s)
	waitAttached(t, fake.attached)

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
