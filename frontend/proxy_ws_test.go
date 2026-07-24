package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialWSClose connects a WS client through the frontend proxy (backed by
// backendURL) and returns the close code the proxy sends after upgrade.
func dialWSClose(t *testing.T, backendURL string) int {
	t.Helper()
	mux := newMuxServer(t, backendURL, noHolesail)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + "/ws/terminal/claude-x"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("client WS dial (upgrade should succeed): %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, rerr := conn.ReadMessage()
	ce, ok := rerr.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected a websocket close, got %v", rerr)
	}
	return ce.Code
}

// TestWSProxySessionGoneClosesNormally locks the flood fix: a backend 404
// (session gone) must close the client with 1000 (normal) so it reports
// "[Session ended]" and stops, rather than auto-reconnecting a dead session.
func TestWSProxySessionGoneClosesNormally(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // 404 on the WS upgrade
	}))
	defer backend.Close()

	if code := dialWSClose(t, backend.URL); code != websocket.CloseNormalClosure {
		t.Errorf("session-gone close code = %d, want %d (normal, so the client stops)", code, websocket.CloseNormalClosure)
	}
}

// TestWSProxyBackendUnreachableClosesAbnormally locks that a genuinely
// unreachable backend still closes abnormally (1011), so the client keeps its
// backoff reconnect — correct when the session may still exist (e.g. a rebuild).
func TestWSProxyBackendUnreachableClosesAbnormally(t *testing.T) {
	if code := dialWSClose(t, "http://127.0.0.1:1"); code != websocket.CloseInternalServerErr {
		t.Errorf("unreachable-backend close code = %d, want %d (abnormal, so the client retries)", code, websocket.CloseInternalServerErr)
	}
}
