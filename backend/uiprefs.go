package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"slices"

	api "claude-sandbox-api"
)

// allowedAccents / allowedThemes are the accent and theme allowlists, sourced
// from the shared enums that also drive the frontend picker (single source).
var allowedAccents = api.AccentNames()
var allowedThemes = api.Themes

// uiPrefs is the dashboard's cross-device UI preference (accent + theme). These
// are dashboard chrome, not Claude container config, so they live in their own
// file rather than in container-settings.json.
type uiPrefs struct {
	Accent string `json:"accent"`
	Theme  string `json:"theme"`
}

// handleGetUIPrefs returns the stored UI prefs, or empty strings when unset
// (the client then keeps its local default).
func (s *Server) handleGetUIPrefs(w http.ResponseWriter, r *http.Request) {
	out := uiPrefs{}
	data, err := os.ReadFile(dashboardPrefsPath())
	if err == nil {
		_ = json.Unmarshal(data, &out)
	} else if !os.IsNotExist(err) {
		slog.Error("failed to read ui prefs", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to read ui prefs")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePutUIPrefs validates and persists the UI prefs (last write wins — the
// dashboard is single-tenant).
func (s *Server) handlePutUIPrefs(w http.ResponseWriter, r *http.Request) {
	var req uiPrefs
	if !decodeJSON(w, r, &req, "invalid JSON") {
		return
	}
	if !slices.Contains(allowedAccents, req.Accent) {
		writeErr(w, http.StatusBadRequest, "invalid accent")
		return
	}
	if !slices.Contains(allowedThemes, req.Theme) {
		writeErr(w, http.StatusBadRequest, "invalid theme")
		return
	}

	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to encode ui prefs")
		return
	}
	data = append(data, '\n')
	if err := writeFileAtomic(dashboardPrefsPath(), data); err != nil {
		slog.Error("failed to write ui prefs", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to write ui prefs")
		return
	}
	writeJSON(w, http.StatusOK, req)
}
