package api

import "time"

// Log file rotation parameters, shared by the producer sink (shared/env.go) and
// the logd reader so both agree on how many generations exist.
const (
	LogRotateCap   = 20 << 20 // rotate a service log once it passes ~20 MB
	LogGenerations = 5        // keep <svc>.log.1 .. .5
)

// LogRecord is the normalized log line logd serves from the query API and the
// SSE stream. Producers write flat slog JSON to their per-service log file;
// logd parses each line into this shape. A line that is not valid JSON becomes
// a raw record (Raw set, Level "error", TS = ingest time).
type LogRecord struct {
	TS      time.Time      `json:"ts"`
	Service string         `json:"service"`
	Level   string         `json:"level"`
	Msg     string         `json:"msg,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Raw     string         `json:"raw,omitempty"`
}

// LogQuery holds the parsed, validated filters for a log query. Empty fields
// mean "no constraint"; Limit is always resolved to a bounded positive value.
type LogQuery struct {
	Service string
	Level   string
	Since   time.Time
	Until   time.Time
	Substr  string
	Limit   int
}
