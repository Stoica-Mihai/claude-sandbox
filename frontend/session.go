package main

import api "claude-sandbox-api"

// DashboardData holds the data passed to the full layout template.
type DashboardData struct {
	Sessions []api.DisplaySession
}
