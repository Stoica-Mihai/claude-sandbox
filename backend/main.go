package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	listenAddr := ":8081"
	if envPort := os.Getenv("BACKEND_PORT"); envPort != "" {
		port := strings.TrimLeft(envPort, ":")
		if _, err := strconv.Atoi(port); err == nil {
			listenAddr = ":" + port
		} else {
			slog.Warn("ignoring invalid BACKEND_PORT", "value", envPort)
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

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
