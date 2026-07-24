package main

import api "claude-sandbox-api"

// DashboardData holds the data passed to the full layout template.
// Logs selects the logs surface (sub-label + logs body + main logs view);
// false renders the dashboard surface (sessions).
// HideLogs omits the Logs nav item for this render — set when the request is
// tunnel-originated AND log sharing is off. Host renders always show Logs;
// tunnel renders show it only once the host enables log sharing.
type DashboardData struct {
	Sessions []api.DisplaySession
	Logs     bool
	HideLogs bool
}
