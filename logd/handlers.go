package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	api "claude-sandbox-api"
)

const (
	defaultLimit = 500
	maxLimit     = 5000
)

type server struct {
	store *store
}

func (s *server) routes(mux *http.ServeMux) {
	// Most-specific wins in Go 1.22 ServeMux, so the stream path is not shadowed
	// by the bare query path.
	mux.HandleFunc("GET "+api.RouteLogsStream, s.handleStream)
	mux.HandleFunc("GET "+api.RouteLogs, s.handleQuery)
	mux.HandleFunc("GET "+api.RouteHealthz, s.handleHealthz)
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	out := s.store.Query(parseQuery(r))
	if out == nil {
		out = []api.LogRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, ch, replay := s.store.subscribe(parseQuery(r))
	defer s.store.unsubscribe(id)

	for _, rec := range replay {
		writeSSE(w, rec)
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case rec := <-ch:
			writeSSE(w, rec)
			flusher.Flush()
		}
	}
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func writeSSE(w io.Writer, rec api.LogRecord) {
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func parseQuery(r *http.Request) api.LogQuery {
	q := r.URL.Query()
	return api.LogQuery{
		Service: q.Get("service"),
		Level:   q.Get("level"),
		Substr:  q.Get("q"),
		Since:   parseTime(q.Get("since")),
		Until:   parseTime(q.Get("until")),
		Limit:   parseLimit(q.Get("limit")),
	}
}

// parseTime accepts an RFC3339 timestamp or a Go duration relative to now
// (e.g. "-15m", "-1h"). Anything unparseable is treated as no constraint.
func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if d, err := time.ParseDuration(v); err == nil {
		return time.Now().Add(d)
	}
	return time.Time{}
}

func parseLimit(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
