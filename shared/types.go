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
