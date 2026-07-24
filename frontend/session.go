package main

import api "claude-sandbox-api"

// DashboardData holds the data passed to the full layout template.
// Logs selects the logs surface (sub-label + logs body + main logs view);
// false renders the dashboard surface (sessions).
type DashboardData struct {
	Sessions []api.DisplaySession
	Logs     bool
}
