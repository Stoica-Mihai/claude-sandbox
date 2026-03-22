package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	// Structured logging to stderr.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	broker := NewBroker()
	sm := NewSessionManager(broker)
	mux := http.NewServeMux()

	srv, err := NewServer(sm, broker, mux)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}
	_ = srv // routes registered on mux via NewServer

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)

		// Kill all managed sessions first.
		sm.Shutdown()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("http server shutdown error", "error", err)
		}
	}()

	slog.Info("server listening", "addr", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
