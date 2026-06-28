package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	sockDir string
	metaDir string
)

// initPaths resolves and creates the session socket and metadata directories.
// It honors CLAUDE_SOCK_DIR / CLAUDE_META_DIR so the Go backend and the `claude`
// shell function agree on locations; otherwise it defaults under XDG_RUNTIME_DIR
// (fallback ~/.local/state). Directories are created 0700 (not world-readable).
func initPaths() error {
	sockDir = os.Getenv("CLAUDE_SOCK_DIR")
	metaDir = os.Getenv("CLAUDE_META_DIR")

	if sockDir == "" || metaDir == "" {
		base := os.Getenv("XDG_RUNTIME_DIR")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			base = filepath.Join(home, ".local", "state")
		}
		root := filepath.Join(base, "claude")
		if sockDir == "" {
			sockDir = filepath.Join(root, "sock")
		}
		if metaDir == "" {
			metaDir = filepath.Join(root, "meta")
		}
	}

	for _, d := range []string{sockDir, metaDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
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
	matches, _ := filepath.Glob(filepath.Join(claudeConfigDir(), "projects", "*", uuid+".jsonl"))
	return len(matches) > 0
}

// settingsJSONPath is the live settings file claude reads at session spawn.
// entrypoint.sh seeds it from container-settings.json on boot; the settings
// editor refreshes it so saved changes apply without a container restart.
func settingsJSONPath() string { return filepath.Join(claudeConfigDir(), "settings.json") }

// newUUID returns a random RFC 4122 v4 UUID string.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// sockPath returns the dtach socket path for a session.
func sockPath(name string) string { return filepath.Join(sockDir, name) }

// metaPath returns the JSON metadata sidecar path for a session.
func metaPath(name string) string { return filepath.Join(metaDir, name+".json") }

// pidPath returns the PID sidecar path for a session.
func pidPath(name string) string { return filepath.Join(metaDir, name+".pid") }

// sessionPID reads the inner process PID from a session's PID sidecar.
// Returns 0 if the sidecar is missing or unparseable.
func sessionPID(name string) int {
	data, err := os.ReadFile(pidPath(name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// processAlive reports whether a process is running via a signal-0 probe.
func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// sessionAlive reports whether a session's inner process is still running.
// It prefers the PID sidecar (signal 0 probe); if absent, it falls back to the
// socket file's existence.
func sessionAlive(name string) bool {
	if pid := sessionPID(name); pid > 0 {
		return processAlive(pid)
	}
	_, err := os.Stat(sockPath(name))
	return err == nil
}

// removeSessionFiles unlinks a session's socket and metadata sidecars.
func removeSessionFiles(name string) {
	os.Remove(sockPath(name))
	os.Remove(metaPath(name))
	os.Remove(pidPath(name))
}

// Session contract (SessionManager.Spawn is the only creator; direct CLI
// `claude` is disabled):
//   1. metadata sidecar: this sessionMeta JSON shape at metaPath(name)
//   2. pid sidecar: the inner `bash -c` writes its own $$ to pidPath(name),
//      then execs `claude --dangerously-skip-permissions` (so the PID is the
//      session-group leader for kill)
//   3. dtach flags: -z and -E (the backend owns the byte stream)
//
// sessionMeta is the per-session metadata sidecar contents.
type sessionMeta struct {
	CWD       string `json:"cwd"`
	Created   int64  `json:"created"`
	SessionID string `json:"session_id,omitempty"` // claude conversation uuid (--session-id)
}

// createdTime returns the session creation time, falling back to the socket
// file's modification time when the sidecar lacks a timestamp.
func (m sessionMeta) createdTime(name string) time.Time {
	if m.Created > 0 {
		return time.Unix(m.Created, 0)
	}
	if fi, err := os.Stat(sockPath(name)); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}
