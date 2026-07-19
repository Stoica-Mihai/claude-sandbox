package api

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// InitLogging installs the standard stderr text slog handler at LevelInfo — the
// logging setup every service's main shares.
func InitLogging() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

// ListenAddr resolves a ":port" listen address from envVar, falling back to
// defaultAddr (and warning) when the value is unset or not a valid port number.
func ListenAddr(envVar, defaultAddr string) string {
	v := os.Getenv(envVar)
	if v == "" {
		return defaultAddr
	}
	port := strings.TrimLeft(v, ":")
	if _, err := strconv.Atoi(port); err != nil {
		slog.Warn("ignoring invalid "+envVar, "value", v)
		return defaultAddr
	}
	return ":" + port
}

// URLFromEnv returns the trailing-slash-trimmed URL from envVar, or defaultURL
// when it is unset.
func URLFromEnv(envVar, defaultURL string) string {
	if v := os.Getenv(envVar); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultURL
}
