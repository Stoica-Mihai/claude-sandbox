package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// allowedWSOrigins is the set of extra origins permitted for WebSocket upgrades,
// loaded from ALLOWED_WS_ORIGINS. Same-origin requests are always allowed.
var allowedWSOrigins map[string]bool

// initAllowedWSOrigins parses ALLOWED_WS_ORIGINS (comma-separated) at startup.
func initAllowedWSOrigins() {
	allowedWSOrigins = make(map[string]bool)
	for _, o := range strings.Split(os.Getenv("ALLOWED_WS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowedWSOrigins[o] = true
		}
	}
}

// checkWSOrigin guards WebSocket upgrades against Cross-Site WebSocket Hijacking.
// It allows requests with no Origin (non-browser clients), same-origin requests
// (Origin host == Host), and origins in the ALLOWED_WS_ORIGINS allowlist; all
// others are rejected (gorilla returns HTTP 403).
func checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		slog.Warn("rejected ws upgrade: malformed Origin", "origin", origin)
		return false
	}
	if u.Host == r.Host || allowedWSOrigins[origin] {
		return true
	}
	slog.Warn("rejected ws upgrade: cross-origin", "origin", origin, "host", r.Host)
	return false
}
