// Package api defines the JSON contract shared between the backend (which
// produces these payloads) and the frontend (which decodes and renders them).
// It is the single source of truth for the wire shapes; both modules depend on
// it via a local replace directive.
package api

import "time"

// DisplaySession is a session as exposed by the backend API and rendered by the
// frontend. SessionID is backend-internal (the claude conversation uuid) and
// never crosses the wire (json:"-").
type DisplaySession struct {
	Name        string      `json:"name"`
	CWD         string      `json:"cwd"`
	DirName     string      `json:"dir_name"`
	CreatedAt   time.Time   `json:"created_at"`
	Alive       bool        `json:"alive"`
	DisplayName string      `json:"display_name"`
	Kind        SessionKind `json:"kind"`
	SessionID   string      `json:"-"`
}

// SessionKind is which engine transport a live session uses: a PTY-backed
// terminal or a stream-json pipe-backed chat. It is a property of the live
// child process, not the persisted conversation (see the session index) — the
// same conversation uuid can run as either kind across a mode switch.
type SessionKind string

const (
	SessionKindTerminal SessionKind = "terminal"
	SessionKindChat     SessionKind = "chat"
)

// ChatConversationResetEvent is a chat session's `conversation_reset`
// stream-json event: emitted when `/clear` (or an equivalent reset) drops
// conversation context. SessionID is the OLD conversation uuid — empirically
// verified (2026-07-22, engine 2.1.215) against the pinned engine.
// NewConversationID is present on the wire but does NOT reliably identify the
// uuid the conversation continues under afterward (verified: it does not match
// the session_id of the system/init event that follows); it is kept only for
// diagnostic logging, never as the re-key target. The actual new uuid is the
// session_id of the next ChatSystemEvent with Subtype "init" — see the
// chat-relay capability's two-step re-key tap.
type ChatConversationResetEvent struct {
	Type              string `json:"type"` // "conversation_reset"
	SessionID         string `json:"session_id"`
	NewConversationID string `json:"new_conversation_id,omitempty"`
}

// ChatSystemEvent is the subset of a chat session's `system` stream-json event
// the backend's re-key tap reads (only Subtype=="init" carries the fresh
// session_id the tap needs). The backend does not parse the full event
// vocabulary — that is the frontend's job, per the chat-relay design.
type ChatSystemEvent struct {
	Type      string `json:"type"` // "system"
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
}

// Chat stream-json event type/subtype values the backend's re-key tap
// recognizes; any other event SHALL pass through the bridge unparsed.
const (
	ChatEventConversationReset = "conversation_reset"
	ChatEventSystem            = "system"
	ChatSystemSubtypeInit      = "init"
)

// Breadcrumb is one path segment in the directory-picker breadcrumb.
type Breadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirectoryData is the directory-picker payload for a single folder.
type DirectoryData struct {
	Path        string       `json:"path"`
	FullPath    string       `json:"full_path"`
	Dirs        []string     `json:"dirs"`
	Breadcrumbs []Breadcrumb `json:"breadcrumbs"`
}

// TranscriptPage is one window of a conversation transcript, newest-last.
// Offset is the absolute index of Lines[0] within the whole transcript, so a
// client pages older history with before=Offset until Offset reaches 0.
type TranscriptPage struct {
	Total  int      `json:"total"`
	Offset int      `json:"offset"`
	Lines  []string `json:"lines"`
}

// SpawnRequest is the body for POST /api/sessions: a new conversation in CWD,
// or a resumed one when Resume (a conversation uuid) is set. Kind selects the
// engine transport (terminal PTY or chat stream-json); empty/absent means
// terminal, so requests from before this field existed keep their behavior.
type SpawnRequest struct {
	CWD    string      `json:"cwd"`
	Resume string      `json:"resume,omitempty"`
	Kind   SessionKind `json:"kind,omitempty"`
}

// SpawnResponse is the 201 payload; SessionName is the terminal id.
type SpawnResponse struct {
	SessionName string `json:"session_name"`
}

// ShareState is the share-tunnel lifecycle state. The holesail sidecar
// (holesail/server.js) produces these values; this enum is the contract.
type ShareState string

const (
	SharePrivate    ShareState = "private"
	SharePublishing ShareState = "publishing"
	SharePublic     ShareState = "public"
	ShareError      ShareState = "error"
)

// ShareStatus is the share-control JSON envelope: served by the holesail
// sidecar for every /api/share/* response and echoed by the frontend's
// tunnel-guard 403. URL and Error are null unless applicable.
type ShareStatus struct {
	State ShareState `json:"state"`
	URL   *string    `json:"url"`
	Error *string    `json:"error"`
}

// CreateDirectoryRequest is the body for creating a new project folder under /workspace.
type CreateDirectoryRequest struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	GitInit bool   `json:"gitInit"`
}

// CreateDirectoryResponse is the 201 payload for a created project folder.
type CreateDirectoryResponse struct {
	Path    string `json:"path"` // new folder relative to /workspace
	Warning string `json:"warning,omitempty"`
}
