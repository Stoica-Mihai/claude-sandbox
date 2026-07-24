package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	api "claude-sandbox-api"
	"claude-sandbox-sessiond/protocol"
)

// pingControl probes the control socket (the same check behind `sessiond
// -ping`) — the single source for both the CLI healthcheck and /healthz.
func pingControl(sockDir string) error {
	resp, err := protocol.Do(sockDir, protocol.Request{Op: protocol.OpPing})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("control socket not ok")
	}
	return nil
}

// healthHandler serves a shallow liveness check: 200 when check() passes, 503
// otherwise. check probes only sessiond itself (its control socket) — no
// dependency, so it can't cascade-fail.
func healthHandler(check func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := check(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// serveHealth runs sessiond's internal health HTTP listener. A bind failure is
// non-fatal (health is not sessiond's real job) — it warns and returns.
func serveHealth(addr string, check func() error) {
	mux := http.NewServeMux()
	mux.Handle("GET "+api.RouteHealthz, healthHandler(check))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Warn("sessiond health server stopped", "error", err)
	}
}
