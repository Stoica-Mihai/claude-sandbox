package main

import "time"

// DisplaySession matches the JSON returned by the backend API.
// Used by templates to render session cards.
type DisplaySession struct {
	Name           string    `json:"name"`
	CWD            string    `json:"cwd"`
	DirName        string    `json:"dir_name"`
	CreatedAt      time.Time `json:"created_at"`
	Duration       string    `json:"duration"`
	Alive          bool      `json:"alive"`
	LastActivity   time.Time `json:"last_activity"`
	LastActiveStr  string    `json:"last_active_str,omitempty"`
	RecentActivity bool      `json:"recent_activity"`
	DisplayName    string    `json:"display_name"`
	Hue            int       `json:"hue"`
}

// DashboardData holds the data passed to the full layout template.
type DashboardData struct {
	Sessions []DisplaySession
}

// Breadcrumb represents a path segment in the directory picker breadcrumb.
type Breadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirectoryData holds the data passed to the directory picker fragment.
type DirectoryData struct {
	Path        string       `json:"path"`
	FullPath    string       `json:"full_path"`
	Dirs        []string     `json:"dirs"`
	Breadcrumbs []Breadcrumb `json:"breadcrumbs"`
}
