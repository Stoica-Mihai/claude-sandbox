package main

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"claude-sandbox-sessiond/protocol"
)

// sockDir is the sessiond socket directory (shared volume with the sessions
// container).
var sockDir string

// initPaths resolves the sessiond socket directory from the protocol package
// (the single owner of the rendezvous path).
func initPaths() error {
	d, err := protocol.SockDir()
	if err != nil {
		return err
	}
	sockDir = d
	return nil
}

// claudeConfigDir returns Claude Code's config dir ($CLAUDE_CONFIG_DIR), which
// is host-mounted and persistent. Falls back to ~/.claude-sandbox.
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".claude-sandbox")
	}
	return "/home/claude/.claude-sandbox"
}

// sessionIndexPath is the dashboard-owned session index (uuid → cwd/created/name),
// in the persistent config dir so it survives container restarts.
func sessionIndexPath() string { return filepath.Join(claudeConfigDir(), "dashboard-sessions.json") }

// dashboardPrefsPath is the cross-device dashboard UI prefs (accent + theme),
// in the persistent config dir so they sync across devices and survive restarts.
func dashboardPrefsPath() string { return filepath.Join(claudeConfigDir(), "dashboard-ui.json") }

// containerSettingsPath is the authoritative container settings file the
// settings editor reads and writes ($CONTAINER_SETTINGS_PATH, default the
// compose bind-mount location).
func containerSettingsPath() string {
	if p := os.Getenv("CONTAINER_SETTINGS_PATH"); p != "" {
		return p
	}
	return "/home/claude/container-settings.json"
}

// hasTranscript reports whether claude has recorded a conversation transcript
// for this uuid (a `<uuid>.jsonl` under any projects/ subdir). A session that
// was spawned but never messaged has none and cannot be resumed — claude exits
// immediately, so such sessions are excluded from the resume list. Matched by
// our uuid filename, not the cwd encoding, so it survives claude layout changes
// (worst case: no matches -> the resume list is empty, never broken).
func hasTranscript(uuid string) bool {
	return len(transcriptPaths(uuid)) > 0
}

// transcriptPaths returns every `<uuid>.jsonl` transcript under projects/.
func transcriptPaths(uuid string) []string {
	matches, _ := filepath.Glob(filepath.Join(claudeConfigDir(), "projects", "*", uuid+".jsonl"))
	return matches
}

// deleteTranscript removes any `<uuid>.jsonl` transcript under projects/.
// Best-effort: zero matches and already-absent files are not errors (the
// authoritative removal is the index entry).
func deleteTranscript(uuid string) {
	for _, m := range transcriptPaths(uuid) {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove transcript", "path", m, "error", err)
		}
	}
}

// settingsJSONPath is the live settings file claude reads at session spawn.
// The sessions container's entrypoint seeds it from container-settings.json on
// boot; the settings editor refreshes it so saved changes apply without a
// container restart.
func settingsJSONPath() string { return filepath.Join(claudeConfigDir(), "settings.json") }

// randBytes returns n cryptographically-random bytes, panicking if the system
// RNG is unavailable (an unrecoverable condition).
func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return b
}

// uuidRe validates RFC-4122 lowercase uuids before they reach the claude
// command line (defense in depth behind index membership).
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// newUUID returns a random RFC 4122 v4 UUID string.
func newUUID() string {
	b := randBytes(16)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
