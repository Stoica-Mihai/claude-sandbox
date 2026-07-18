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
	Name        string    `json:"name"`
	CWD         string    `json:"cwd"`
	DirName     string    `json:"dir_name"`
	CreatedAt   time.Time `json:"created_at"`
	Alive       bool      `json:"alive"`
	DisplayName string    `json:"display_name"`
	SessionID   string    `json:"-"`
}

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

// SpawnRequest is the body for POST /api/sessions: a new conversation in CWD,
// or a resumed one when Resume (a conversation uuid) is set.
type SpawnRequest struct {
	CWD    string `json:"cwd"`
	Resume string `json:"resume,omitempty"`
}

// SpawnResponse is the 201 payload; SessionName is the dtach terminal id.
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
