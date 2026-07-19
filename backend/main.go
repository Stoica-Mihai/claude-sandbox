package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "claude-sandbox-api"
)

func main() {
	api.InitLogging()
	listenAddr := api.ListenAddr("BACKEND_PORT", ":8081")

	if err := initPaths(); err != nil {
		slog.Error("failed to initialize session directories", "error", err)
		os.Exit(1)
	}

	broker := NewBroker()
	sm := NewSessionManager(broker)
	mux := http.NewServeMux()

	NewServer(sm, broker, mux)

	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		sm.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("http server shutdown error", "error", err)
		}
	}()

	slog.Info("backend server listening", "addr", listenAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("backend server stopped")
}
