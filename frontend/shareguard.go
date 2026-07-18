package main

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// tunnelGuard identifies requests that arrived through the share tunnel.
// Tunneled traffic enters the frontend with source IP = the holesail
// container, so matching RemoteAddr against the sidecar's resolved IPs
// tells tunnel visitors apart from LAN users.
type tunnelGuard struct {
	host    string
	resolve func(string) ([]net.IP, error)
	ttl     time.Duration

	mu        sync.Mutex
	ips       map[string]bool // nil until the first successful resolution
	refreshed time.Time
	expires   time.Time
}

// newTunnelGuard builds a guard for the holesail control URL's hostname.
func newTunnelGuard(holesailURL string) *tunnelGuard {
	host := ""
	if u, err := url.Parse(holesailURL); err == nil {
		host = u.Hostname()
	}
	return &tunnelGuard{
		host:    host,
		resolve: net.LookupIP,
		ttl:     10 * time.Second,
	}
}

// isTunnelRequest reports whether r's source IP belongs to the holesail
// container. Fail-open before the first successful resolution: an
// unresolvable sidecar means no tunnel can exist.
func (g *tunnelGuard) isTunnelRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if time.Now().After(g.expires) {
		g.refresh()
	}
	if g.ips == nil {
		return false
	}
	if g.ips[host] {
		return true
	}
	// Miss against a cache that may predate a sidecar restart (new container
	// IP mid-TTL): force one re-resolve before allowing.
	if time.Since(g.refreshed) > time.Second {
		g.refresh()
		return g.ips[host]
	}
	return false
}

// refresh re-resolves the sidecar hostname. On failure the previous IP set is
// kept (stale beats open) and expires is left in the past so the next request
// retries; refreshed still advances to throttle the mismatch double-check.
func (g *tunnelGuard) refresh() {
	g.refreshed = time.Now()
	addrs, err := g.resolve(g.host)
	if err != nil {
		if g.ips == nil {
			slog.Warn("share guard: sidecar hostname has never resolved; allowing share controls",
				"host", g.host, "error", err)
		}
		return
	}
	ips := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		ips[a.String()] = true
	}
	g.ips = ips
	g.expires = time.Now().Add(g.ttl)
}
