package main

import api "claude-sandbox-api"

// DashboardData holds the data passed to the full layout template.
// Logs selects the logs surface (sub-label + logs body + main logs view);
// false renders the dashboard surface (sessions).
// IsTunnel marks a share-tunnel-originated render so the Logs surface is hidden
// from tunnel visitors (host-only); the log API stays separately opt-in-gated.
type DashboardData struct {
	Sessions []api.DisplaySession
	Logs     bool
	IsTunnel bool
}
