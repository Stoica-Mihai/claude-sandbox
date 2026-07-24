package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	api "claude-sandbox-api"
)

type fakeSink struct {
	mu   sync.Mutex
	recs []api.LogRecord
}

func (f *fakeSink) add(r api.LogRecord) {
	f.mu.Lock()
	f.recs = append(f.recs, r)
	f.mu.Unlock()
}

func (f *fakeSink) msgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.recs))
	for i, r := range f.recs {
		if r.Raw != "" {
			out[i] = r.Raw
		} else {
			out[i] = r.Msg
		}
	}
	return out
}

func appendStr(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	f.Close()
}

func jsonLine(msg string) string {
	return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":%q}`+"\n", time.Now().Format(time.RFC3339Nano), msg)
}

// rotateProducer mimics the shared sink's rotation: shift generations, rename
// the live file to .1. The next append recreates the live file (O_CREATE).
func rotateProducer(t *testing.T, path string) {
	t.Helper()
	for i := api.LogGenerations - 1; i >= 1; i-- {
		_ = os.Rename(path+"."+strconv.Itoa(i), path+"."+strconv.Itoa(i+1))
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
}

func TestTailerPartialLineAndNonJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.log")
	sink := &fakeSink{}
	tl := newTailer(dir, sink, loadOffsets(filepath.Join(dir, "off.json")))

	appendStr(t, path, "par") // unterminated
	tl.pollOnce(time.Now())
	if got := sink.msgs(); len(got) != 0 {
		t.Fatalf("unterminated line emitted early: %v", got)
	}

	appendStr(t, path, "tial\n") // completes to "partial" (non-JSON → raw)
	tl.pollOnce(time.Now())
	got := sink.msgs()
	if len(got) != 1 || got[0] != "partial" {
		t.Fatalf("got %v, want [partial]", got)
	}
	sink.mu.Lock()
	lvl := sink.recs[0].Level
	sink.mu.Unlock()
	if lvl != "error" {
		t.Errorf("non-JSON raw record level = %q, want error", lvl)
	}
}

func TestTailerZeroLossAcrossRotations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.log")
	sink := &fakeSink{}
	tl := newTailer(dir, sink, loadOffsets(filepath.Join(dir, "off.json")))

	var want []string
	seq := 0
	writeBatch := func() {
		for i := 0; i < 5; i++ {
			m := fmt.Sprintf("m%d", seq)
			appendStr(t, path, jsonLine(m))
			want = append(want, m)
			seq++
		}
	}

	for r := 0; r < 4; r++ {
		writeBatch()
		tl.pollOnce(time.Now()) // drain before rotating
		rotateProducer(t, path)
	}
	writeBatch()
	tl.pollOnce(time.Now())

	got := sink.msgs()
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d (loss across rotation)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTailerOffsetResumeAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.log")
	offPath := filepath.Join(dir, "off.json")

	sink1 := &fakeSink{}
	off1 := loadOffsets(offPath)
	tl1 := newTailer(dir, sink1, off1)
	appendStr(t, path, jsonLine("one"))
	appendStr(t, path, jsonLine("two"))
	tl1.pollOnce(time.Now())
	if err := off1.flush(); err != nil {
		t.Fatalf("flush offsets: %v", err)
	}
	if len(sink1.msgs()) != 2 {
		t.Fatalf("first run got %v, want 2", sink1.msgs())
	}

	// New line arrives, then "restart": fresh tailer + reloaded offsets.
	appendStr(t, path, jsonLine("three"))
	sink2 := &fakeSink{}
	tl2 := newTailer(dir, sink2, loadOffsets(offPath))
	tl2.pollOnce(time.Now())

	got := sink2.msgs()
	if len(got) != 1 || got[0] != "three" {
		t.Fatalf("after restart got %v, want [three] (offset not resumed)", got)
	}
}

func TestTailerIdleFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.log")
	sink := &fakeSink{}
	tl := newTailer(dir, sink, loadOffsets(filepath.Join(dir, "off.json")))
	tl.idleTimeout = 10 * time.Millisecond

	appendStr(t, path, "crash-truncated-tail") // no newline, never completed
	start := time.Now()
	tl.pollOnce(start)
	if len(sink.msgs()) != 0 {
		t.Fatalf("partial flushed too early")
	}
	tl.pollOnce(start.Add(50 * time.Millisecond)) // past idle timeout
	got := sink.msgs()
	if len(got) != 1 || got[0] != "crash-truncated-tail" {
		t.Fatalf("idle flush got %v, want [crash-truncated-tail]", got)
	}
}
