package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	api "claude-sandbox-api"
)

const (
	pollInterval  = 300 * time.Millisecond
	flushInterval = 1 * time.Second
)

func main() {
	// logd logs to its console only (no LOG_DIR) — it never writes into the
	// shared logs volume, so it cannot tail itself.
	api.InitLogging("logd")

	logsDir := envOr("LOGS_DIR", "/logs")
	stateDir := envOr("STATE_DIR", "/state")
	listenAddr := api.ListenAddr("LOGD_PORT", ":8082")

	offsets := loadOffsets(filepath.Join(stateDir, "offsets.json"))
	st := newStore(logsDir, defaultRingCap)
	tail := newTailer(logsDir, st, offsets)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	go tail.Run(ctx, pollInterval, flushInterval)

	srv := &server{store: st}
	mux := http.NewServeMux()
	srv.routes(mux)
	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShut()
		if err := httpServer.Shutdown(shutCtx); err != nil {
			slog.Error("http server shutdown error", "error", err)
		}
	}()

	slog.Info("logd listening", "addr", listenAddr, "logs_dir", logsDir, "state_dir", stateDir)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("logd stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
