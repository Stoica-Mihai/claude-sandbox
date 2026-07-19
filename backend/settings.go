package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	api "claude-sandbox-api"
)

// modelRank ranks a model family by capability so the advisor can be required
// to be strictly more capable than the main model (claude rejects the request
// otherwise). Works on both main aliases ("opus[1m]") and advisor ids
// ("claude-opus-4-8"). Ranks derive from api.ModelFamilies (most-capable
// first), the same list canonicalModelID's alternation uses. Unknown: 0.
func modelRank(s string) int {
	s = strings.ToLower(s)
	for i, fam := range api.ModelFamilies {
		if strings.Contains(s, fam) {
			return len(api.ModelFamilies) - i
		}
	}
	return 0
}

// allowedModels / allowedEffort are the model and effort-level allowlists,
// sourced from the shared enums so validation and the rendered settings modal
// stay in sync (add a value in shared/enums.go, both update).
var allowedModels = api.ModelValues()
var allowedEffort = api.EffortValues()

// canonicalModelID matches a full Claude model id (e.g. claude-opus-4-8),
// the shape the advisor accepts — version-agnostic so it survives new releases.
// The family alternation is built from api.ModelFamilies (the list modelRank
// also uses) so the two never disagree on which families exist.
var canonicalModelID = regexp.MustCompile(
	`^claude-(` + strings.Join(api.ModelFamilies, "|") + `)-[0-9][0-9-]*$`,
)

// editableSettings is the whitelisted preference subset the dashboard may
// read and write. All other keys in container-settings.json are off-limits.
type editableSettings struct {
	Model                 string `json:"model"`
	EffortLevel           string `json:"effortLevel"`
	AlwaysThinkingEnabled bool   `json:"alwaysThinkingEnabled"`
	Language              string `json:"language"`
	AdvisorModel          string `json:"advisorModel"`
}

// readContainerSettings loads container-settings.json into a generic map so
// non-editable keys survive a round-trip. A missing file yields an empty map.
func readContainerSettings() (map[string]any, error) {
	data, err := os.ReadFile(containerSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// handleGetSettings returns only the editable preference subset.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	m, err := readContainerSettings()
	if err != nil {
		slog.Error("failed to read container settings", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to read settings")
		return
	}

	// Project the generic map onto the editable subset via a JSON round-trip;
	// unknown keys are dropped, fields with the wrong type fall back to zero.
	out := editableSettings{}
	raw, _ := json.Marshal(m)
	_ = json.Unmarshal(raw, &out)
	writeJSON(w, http.StatusOK, out)
}

// validLanguage reports whether s is a short string with no control characters.
func validLanguage(s string) bool {
	if len(s) > 40 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// handlePutSettings validates the editable subset, merges it into the existing
// container-settings.json (preserving every other key), writes it atomically,
// then refreshes the live settings.json so new sessions pick it up.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req editableSettings
	if !decodeJSON(w, r, &req, "invalid JSON") {
		return
	}

	// Validate against the allowlists; reject without touching any file.
	if !slices.Contains(allowedModels, req.Model) {
		writeErr(w, http.StatusBadRequest, "invalid model")
		return
	}
	// advisorModel must be empty (off) or a canonical Claude model id
	// (e.g. claude-opus-4-8) — the /advisor command writes ids in this shape;
	// main-model aliases like "opus[1m]" are NOT valid advisor values.
	if req.AdvisorModel != "" && !canonicalModelID.MatchString(req.AdvisorModel) {
		writeErr(w, http.StatusBadRequest, "invalid advisorModel")
		return
	}
	// The advisor must be strictly more capable than the main model, else claude
	// rejects every request ("cannot be used as an advisor when the request model
	// is ..."). e.g. Sonnet main + Opus advisor is valid; Opus main needs no advisor.
	if req.AdvisorModel != "" && modelRank(req.AdvisorModel) <= modelRank(req.Model) {
		writeErr(w, http.StatusBadRequest, "advisor model must be more capable than the main model (e.g. Sonnet main + Opus advisor); with Opus as the main model, set the advisor to none")
		return
	}
	if !slices.Contains(allowedEffort, req.EffortLevel) {
		writeErr(w, http.StatusBadRequest, "invalid effortLevel")
		return
	}
	if !validLanguage(req.Language) {
		writeErr(w, http.StatusBadRequest, "invalid language")
		return
	}

	// Merge only the whitelisted keys into the existing file.
	m, err := readContainerSettings()
	if err != nil {
		slog.Error("failed to read container settings", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to read settings")
		return
	}
	m["model"] = req.Model
	m["effortLevel"] = req.EffortLevel
	m["alwaysThinkingEnabled"] = req.AlwaysThinkingEnabled
	m["language"] = req.Language
	if req.AdvisorModel == "" {
		delete(m, "advisorModel")
	} else {
		m["advisorModel"] = req.AdvisorModel
	}

	merged, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to encode settings")
		return
	}
	merged = append(merged, '\n')

	if err := writeFileAtomic(containerSettingsPath(), merged); err != nil {
		slog.Error("failed to write container settings", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to write settings")
		return
	}
	// Refresh the live settings.json so the next spawned session uses it.
	if err := writeFileAtomic(settingsJSONPath(), merged); err != nil {
		slog.Error("failed to refresh live settings.json", "error", err)
		writeErr(w, http.StatusInternalServerError, "failed to refresh live settings")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// writeFileAtomic writes data to path via a temp file + rename (mode 0600) so a
// reader never sees a partial file. When path is a single-file bind mount (e.g.
// the compose-mounted container-settings.json), rename onto the mount point
// fails (EBUSY/EXDEV); it then falls back to an in-place write through the mount.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return writeInPlace(path, data)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	// fsync the temp before the rename publishes it, so a crash can't leave a
	// zero-length/truncated file in place of the old one.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return writeInPlace(path, data)
	}
	// fsync the parent dir so the rename itself survives a crash (POSIX: a
	// renamed file in an un-fsynced dir can revert). Best-effort.
	syncDir(dir)
	return nil
}

// syncDir fsyncs a directory so a rename within it is durable. Best-effort:
// some filesystems reject dir fsync, and the rename already succeeded.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

// writeInPlace overwrites path's contents directly (needed for bind-mounted
// files, which cannot be replaced by rename). It fsyncs before returning so a
// completed write is durable.
func writeInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
