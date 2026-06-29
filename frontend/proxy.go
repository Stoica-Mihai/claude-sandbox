package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

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

// buildBackendURL constructs the full backend URL from a base URL and the
// incoming request's path and query string.
func buildBackendURL(base string, r *http.Request) string {
	u := base + r.URL.Path
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
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

	// Dial backend.
	backendConn, resp, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
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
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// Upgrade client connection.
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer clientConn.Close()

	done := make(chan struct{})
	go pipeWs(backendConn, clientConn, "backend→client", func() { close(done) })
	go pipeWs(clientConn, backendConn, "client→backend", func() { backendConn.Close() })
	<-done
}

// sseProxy proxies an SSE stream from the backend to the client.
// It copies the response headers and streams the body.
func sseProxy(w http.ResponseWriter, r *http.Request, backendURL string) {
	targetURL := buildBackendURL(backendURL, r)

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create backend request", http.StatusInternalServerError)
		return
	}

	// Copy relevant headers.
	for _, h := range []string{"Accept", "Cache-Control", "Last-Event-ID"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		badGateway(w, "SSE proxy: backend request failed", err)
		return
	}
	defer resp.Body.Close()

	// Copy response headers.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	// Stream the body. For SSE, we read chunks and flush.
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// httpProxy forwards an HTTP request to the backend and copies the response
// back to the client. Used for simple API proxy routes.
func httpProxy(w http.ResponseWriter, r *http.Request, backendURL string) {
	targetURL := buildBackendURL(backendURL, r)

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "failed to create backend request", http.StatusInternalServerError)
		return
	}

	// Copy request headers (Content-Type, Accept, etc.).
	for k, vv := range r.Header {
		// Skip hop-by-hop headers.
		lower := strings.ToLower(k)
		if lower == "connection" || lower == "upgrade" || lower == "host" {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("proxy: backend request failed", "url", targetURL, "error", err)
		http.Error(w, "backend connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
