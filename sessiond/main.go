// sessiond hosts claude sessions: it owns each session's PTY and terminal
// state and serves attach/input/resize/spawn/list/kill over unix sockets.
// The API backend is a client; sessions survive its restarts.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// cleanStaleSockets unlinks leftover sockets from a previous run.
func cleanStaleSockets(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sock") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func main() {
	ping := flag.Bool("ping", false, "probe the control socket and exit (healthcheck)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	sockDir, err := protocol.SockDir()
	if err != nil {
		slog.Error("cannot resolve socket dir", "error", err)
		os.Exit(1)
	}

	if *ping {
		resp, err := protocol.Do(sockDir, protocol.Request{Op: protocol.OpPing})
		if err != nil || !resp.OK {
			fmt.Fprintf(os.Stderr, "ping failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		slog.Error("cannot create socket dir", "dir", sockDir, "error", err)
		os.Exit(1)
	}
	// MkdirAll no-ops on an existing dir (image/volume pre-create it with the
	// build umask), so enforce the 0700 contract explicitly.
	if err := os.Chmod(sockDir, 0o700); err != nil {
		slog.Error("cannot chmod socket dir", "dir", sockDir, "error", err)
		os.Exit(1)
	}
	cleanStaleSockets(sockDir)

	reg := newRegistry(sockDir)
	ln, err := net.Listen("unix", protocol.ControlSock(sockDir))
	if err != nil {
		slog.Error("control socket listen failed", "error", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down sessions", "signal", sig)
		_ = ln.Close()
		reg.shutdown(5 * time.Second)
		os.Exit(0)
	}()

	slog.Info("sessiond listening", "control", protocol.ControlSock(sockDir))
	reg.serveControl(ln)
}
