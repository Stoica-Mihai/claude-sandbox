package main

import (
	"context"
	"net/http"
)

// Tunnel-origin detection. The holesail sidecar's relay targets a dedicated
// frontend listener (TUNNEL_PORT), so "did this request arrive through the
// share tunnel" is a property of the listening socket — topology, not a
// runtime DNS/IP heuristic. Every request on that listener is stamped via
// markTunnel; direct/LAN traffic on the main listener never is.

// ctxKeyTunnel marks requests that arrived on the tunnel listener.
type ctxKeyTunnel struct{}

// markTunnel wraps the mux for the tunnel listener, stamping every request.
func markTunnel(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyTunnel{}, true)))
	})
}

// isTunnelRequest reports whether r arrived through the share tunnel.
func isTunnelRequest(r *http.Request) bool {
	v, _ := r.Context().Value(ctxKeyTunnel{}).(bool)
	return v
}
