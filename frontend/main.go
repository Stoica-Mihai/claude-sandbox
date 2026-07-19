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

	listenAddr := api.ListenAddr("DASHBOARD_PORT", ":8080")
	// Backend API URL (where sessions, relays, SSE, WebSocket live).
	backendURL := api.URLFromEnv("BACKEND_URL", "http://backend:8081")
	// Holesail sidecar control URL (share tunnel).
	holesailURL := api.URLFromEnv("HOLESAIL_URL", "http://holesail:9000")

	initAllowedWSOrigins()

	mux := http.NewServeMux()

	srv, err := NewServer(backendURL, holesailURL, mux)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}
	_ = srv // routes registered on mux via NewServer

	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Tunnel listener: the holesail sidecar relays share-tunnel traffic here,
	// so tunnel origin is a property of the socket (see shareguard.go). Not
	// published to the host; reachable only on the compose network.
	tunnelAddr := api.ListenAddr("TUNNEL_PORT", ":8090")
	tunnelServer := &http.Server{
		Addr:              tunnelAddr,
		Handler:           markTunnel(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := tunnelServer.Shutdown(ctx); err != nil {
			slog.Error("tunnel server shutdown error", "error", err)
		}
		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("http server shutdown error", "error", err)
		}
	}()

	go func() {
		slog.Info("tunnel listener up", "addr", tunnelAddr)
		if err := tunnelServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("tunnel server error", "error", err)
		}
	}()

	slog.Info("dashboard server listening", "addr", listenAddr, "backend", backendURL)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("dashboard server stopped")
}
