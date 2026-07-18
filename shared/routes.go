package api

// API route path patterns shared by the backend (which registers the handlers)
// and the frontend (which registers the mirrored proxy routes and builds
// request URLs to the backend). Single source, so adding or renaming a route is
// one edit rather than two files kept in sync by hand. Method verbs stay at the
// registration sites; only the path is shared. The share-tunnel routes
// (/api/share/*) live only in the frontend proxy + holesail sidecar and are not
// here.
// HeaderTerminalID carries the spawned session's terminal id on the frontend's
// spawn response, so the dashboard JS can open the tab (picker.js reads it).
const HeaderTerminalID = "X-Terminal-Id"

const (
	RouteSessions        = "/api/sessions"
	RouteSessionsHistory = "/api/sessions/history"
	RouteSession         = "/api/sessions/{terminalId}"
	RouteSessionName     = "/api/sessions/{terminalId}/name"
	RouteSessionUpload   = "/api/sessions/{terminalId}/upload"
	RouteHistoryItem     = "/api/sessions/history/{uuid}"
	RouteDirectories     = "/api/directories"
	RouteSettings        = "/api/settings"
	RouteUIPrefs         = "/api/ui-prefs"
	RouteEvents          = "/events"
	RouteWSTerminal      = "/ws/terminal/{terminalId}"
	RouteHealthz         = "/healthz"
)
