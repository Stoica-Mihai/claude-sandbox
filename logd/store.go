package main

import (
	"bufio"
	"container/heap"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	api "claude-sandbox-api"
)

const (
	defaultRingCap = 5000
	subQueueSize   = 256
	maxScanLine    = 8 << 20 // allow scanning lines up to 8 MB
)

// store is both the recent-line ring buffer (fast SSE replay + fan-out) and the
// query engine. Queries read the log files directly — the files are the source
// of truth, so a query never misses a line on disk; the ring is only a cache.
type store struct {
	dir     string
	ringCap int

	mu     sync.Mutex
	ring   []api.LogRecord
	subs   map[int]*subscriber
	nextID int
}

type subscriber struct {
	ch chan api.LogRecord
	q  api.LogQuery
}

func newStore(dir string, ringCap int) *store {
	if ringCap <= 0 {
		ringCap = defaultRingCap
	}
	return &store{dir: dir, ringCap: ringCap, subs: map[int]*subscriber{}}
}

// add appends to the ring (bounded) and fans out to matching subscribers. A
// subscriber whose queue is full is skipped (view-drop) — this never blocks the
// tailer and never affects the durable files.
func (s *store) add(rec api.LogRecord) {
	s.mu.Lock()
	s.ring = append(s.ring, rec)
	if len(s.ring) > s.ringCap {
		s.ring = s.ring[len(s.ring)-s.ringCap:]
	}
	subs := make([]*subscriber, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		if !matchLive(rec, sub.q) {
			continue
		}
		select {
		case sub.ch <- rec:
		default: // evict-on-full: dropped for this viewer only
		}
	}
}

// subscribe registers an SSE viewer and returns its id, channel, and the recent
// matching records to replay before live streaming.
func (s *store) subscribe(q api.LogQuery) (int, <-chan api.LogRecord, []api.LogRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan api.LogRecord, subQueueSize)
	s.subs[id] = &subscriber{ch: ch, q: q}
	var replay []api.LogRecord
	for _, r := range s.ring {
		if matchLive(r, q) {
			replay = append(replay, r)
		}
	}
	return id, ch, replay
}

func (s *store) unsubscribe(id int) {
	s.mu.Lock()
	delete(s.subs, id)
	s.mu.Unlock()
}

// Query returns up to q.Limit records matching the filters, newest-first, by
// scanning the log files (live + rotated generations) for the in-scope services.
// Memory is bounded to O(limit) via a min-heap of the newest matches.
func (s *store) Query(q api.LogQuery) []api.LogRecord {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	sub := strings.ToLower(q.Substr)
	h := &recHeap{}
	heap.Init(h)
	for _, svc := range s.servicesFor(q.Service) {
		for _, path := range s.filesFor(svc) {
			s.scanFile(path, svc, q, sub, h)
		}
	}
	out := make([]api.LogRecord, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(api.LogRecord) // min-heap pops ascending → fill from end → newest-first
	}
	return out
}

func (s *store) scanFile(path, svc string, q api.LogQuery, sub string, h *recHeap) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	// A raw line has no embedded timestamp; carry the last real timestamp seen
	// (file modtime as the initial floor) so crash output sorts near its context
	// instead of at the epoch.
	lastTS := time.Time{}
	if fi, e := f.Stat(); e == nil {
		lastTS = fi.ModTime()
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxScanLine)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		rec := parseLine(svc, line, lastTS)
		lastTS = rec.TS
		if sub != "" && !strings.Contains(strings.ToLower(line), sub) {
			continue
		}
		if !matchQuery(rec, q) {
			continue
		}
		heap.Push(h, rec)
		if h.Len() > q.Limit {
			heap.Pop(h)
		}
	}
}

func (s *store) servicesFor(service string) []string {
	if service != "" {
		return []string{service}
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range logFiles(s.dir) {
		svc := serviceFromPath(p)
		if !seen[svc] {
			seen[svc] = true
			out = append(out, svc)
		}
	}
	return out
}

// filesFor returns the existing files for a service, oldest generation first.
func (s *store) filesFor(svc string) []string {
	live := filepath.Join(s.dir, svc+".log")
	var files []string
	for i := api.LogGenerations; i >= 1; i-- {
		p := live + "." + strconv.Itoa(i)
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	if _, err := os.Stat(live); err == nil {
		files = append(files, live)
	}
	return files
}

// matchQuery applies the full filter set (used for file-scan queries).
func matchQuery(rec api.LogRecord, q api.LogQuery) bool {
	if q.Level != "" && !strings.EqualFold(rec.Level, q.Level) {
		return false
	}
	if !q.Since.IsZero() && rec.TS.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && rec.TS.After(q.Until) {
		return false
	}
	return true
}

// matchLive filters a record for a live SSE subscriber (no time-range: the
// stream is a tail). Substring matches the record's message + raw text, since
// the original on-disk line is not retained in the ring.
func matchLive(rec api.LogRecord, q api.LogQuery) bool {
	if q.Service != "" && !strings.EqualFold(rec.Service, q.Service) {
		return false
	}
	if q.Level != "" && !strings.EqualFold(rec.Level, q.Level) {
		return false
	}
	if q.Substr != "" {
		hay := strings.ToLower(rec.Msg + " " + rec.Raw)
		if !strings.Contains(hay, strings.ToLower(q.Substr)) {
			return false
		}
	}
	return true
}

// recHeap is a min-heap by timestamp, so Query keeps only the newest q.Limit
// matches (evicting the oldest when it overflows).
type recHeap []api.LogRecord

func (h recHeap) Len() int            { return len(h) }
func (h recHeap) Less(i, j int) bool  { return h[i].TS.Before(h[j].TS) }
func (h recHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *recHeap) Push(x any)         { *h = append(*h, x.(api.LogRecord)) }
func (h *recHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
