package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// backendDialer dials the backend WebSocket with a bounded handshake so a hung
// backend doesn't stall the client upgrade indefinitely.
var backendDialer = &websocket.Dialer{HandshakeTimeout: 5 * time.Second}

// pipeWs reads frames from src and writes them to dst until an error occurs.
// On a close frame from src, it forwards the close to dst. onDone is called
// when the loop exits.
func pipeWs(src, dst *websocket.Conn, label string, onDone func()) {
	defer onDone()
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("ws proxy: "+label+" read error", "error", err)
			}
			if ce, ok := err.(*websocket.CloseError); ok {
				dst.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(ce.Code, ce.Text))
			}
			return
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
			slog.Debug("ws proxy: "+label+" write error", "error", err)
			return
		}
	}
}

// wsProxy proxies a WebSocket connection from the client to the backend.
// It upgrades the client connection, dials the backend, and pipes frames
// in both directions until one side closes.
func wsProxy(w http.ResponseWriter, r *http.Request, backendURL string) {
	upgrader := websocket.Upgrader{
		CheckOrigin: checkWSOrigin,
	}

	// Build backend WebSocket URL.
	parsed, err := url.Parse(backendURL)
	if err != nil {
		http.Error(w, "bad backend URL", http.StatusInternalServerError)
		return
	}

	// Switch scheme to ws/wss.
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = r.URL.Path
	parsed.RawQuery = r.URL.RawQuery

	// Upgrade the client FIRST: the origin check (CSWSH guard) runs inside
	// Upgrade, so dialing the backend before it would open a backend attach —
	// with its scrollback replay side effects — for a request that gets
	// rejected.
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer clientConn.Close()

	backendConn, resp, err := backendDialer.Dial(parsed.String(), nil)
	if err != nil {
		if resp != nil {
			slog.Error("websocket dial to backend failed",
				"url", parsed.String(),
				"status", resp.StatusCode,
				"error", err,
			)
		} else {
			slog.Error("websocket dial to backend failed",
				"url", parsed.String(),
				"error", err,
			)
		}
		// Already upgraded: signal failure via a close frame. Not code 1000,
		// so the client treats it as abnormal and retries with backoff.
		msg := websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "backend connection failed")
		_ = clientConn.WriteMessage(websocket.CloseMessage, msg)
		return
	}
	defer backendConn.Close()

	done := make(chan struct{})
	go pipeWs(backendConn, clientConn, "backend→client", func() { close(done) })
	go pipeWs(clientConn, backendConn, "client→backend", func() { backendConn.Close() })
	<-done
}
