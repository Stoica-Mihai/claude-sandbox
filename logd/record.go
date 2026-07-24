package main

import (
	"encoding/json"
	"time"

	api "claude-sandbox-api"
)

// reservedKeys are the flat slog fields promoted to LogRecord fields; everything
// else on a JSON line collects into Attrs.
var reservedKeys = map[string]bool{"time": true, "level": true, "msg": true, "service": true}

// parseLine normalizes one on-disk log line into a LogRecord. A valid slog JSON
// object maps its known keys to fields (remaining keys → Attrs); anything else
// (panic text, third-party stderr) becomes a raw record at level "error" with
// the ingest time, so crash output is preserved and queryable.
func parseLine(service string, line string, ingest time.Time) api.LogRecord {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
		return api.LogRecord{TS: ingest, Service: service, Level: "error", Raw: line}
	}

	rec := api.LogRecord{Service: service, TS: ingest}
	if s, ok := m["service"].(string); ok && s != "" {
		rec.Service = s
	}
	if s, ok := m["level"].(string); ok {
		rec.Level = s
	}
	if s, ok := m["msg"].(string); ok {
		rec.Msg = s
	}
	if s, ok := m["time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			rec.TS = t
		}
	}
	var attrs map[string]any
	for k, v := range m {
		if reservedKeys[k] {
			continue
		}
		if attrs == nil {
			attrs = make(map[string]any)
		}
		attrs[k] = v
	}
	rec.Attrs = attrs
	return rec
}
