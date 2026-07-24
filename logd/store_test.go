package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "claude-sandbox-api"
)

func writeLine(t *testing.T, path string, ts time.Time, level, msg string) {
	t.Helper()
	line := fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q}`+"\n", ts.Format(time.RFC3339Nano), level, msg)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line)
	f.Close()
}

func msgsOf(recs []api.LogRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Msg
	}
	return out
}

func TestStoreQueryNewestFirstAndFilters(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	be := filepath.Join(dir, "backend.log")
	fe := filepath.Join(dir, "frontend.log")
	writeLine(t, be, base.Add(1*time.Second), "INFO", "b-old")
	writeLine(t, be, base.Add(3*time.Second), "ERROR", "b-timeout")
	writeLine(t, fe, base.Add(2*time.Second), "INFO", "f-mid")

	st := newStore(dir, 100)

	all := st.Query(api.LogQuery{Limit: 10})
	if got := msgsOf(all); len(got) != 3 || got[0] != "b-timeout" || got[2] != "b-old" {
		t.Fatalf("newest-first across services = %v, want [b-timeout f-mid b-old]", got)
	}

	byService := st.Query(api.LogQuery{Service: "backend", Limit: 10})
	if got := msgsOf(byService); len(got) != 2 {
		t.Fatalf("service filter = %v, want 2 backend records", got)
	}

	byLevel := st.Query(api.LogQuery{Level: "error", Limit: 10})
	if got := msgsOf(byLevel); len(got) != 1 || got[0] != "b-timeout" {
		t.Fatalf("level filter (case-insensitive) = %v, want [b-timeout]", got)
	}

	bySubstr := st.Query(api.LogQuery{Substr: "TIMEOUT", Limit: 10})
	if got := msgsOf(bySubstr); len(got) != 1 || got[0] != "b-timeout" {
		t.Fatalf("substr filter (case-insensitive over raw line) = %v, want [b-timeout]", got)
	}
}

func TestStoreQueryTimeRangeAndLimit(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	be := filepath.Join(dir, "backend.log")
	for i := 0; i < 5; i++ {
		writeLine(t, be, base.Add(time.Duration(i)*time.Second), "INFO", fmt.Sprintf("m%d", i))
	}
	st := newStore(dir, 100)

	limited := st.Query(api.LogQuery{Limit: 2})
	if got := msgsOf(limited); len(got) != 2 || got[0] != "m4" || got[1] != "m3" {
		t.Fatalf("limit=2 newest = %v, want [m4 m3]", got)
	}

	ranged := st.Query(api.LogQuery{Since: base.Add(1 * time.Second), Until: base.Add(3 * time.Second), Limit: 10})
	if got := msgsOf(ranged); len(got) != 3 || got[0] != "m3" || got[2] != "m1" {
		t.Fatalf("since/until = %v, want [m3 m2 m1]", got)
	}
}

func TestStoreQueryScansRotatedGenerations(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	live := filepath.Join(dir, "backend.log")
	writeLine(t, live+".1", base.Add(1*time.Second), "INFO", "rotated-old")
	writeLine(t, live, base.Add(2*time.Second), "INFO", "current-new")

	st := newStore(dir, 100)
	got := msgsOf(st.Query(api.LogQuery{Service: "backend", Limit: 10}))
	if len(got) != 2 || got[0] != "current-new" || got[1] != "rotated-old" {
		t.Fatalf("query across generations = %v, want [current-new rotated-old]", got)
	}
}

func TestStoreSubscribeReplayLiveAndFilter(t *testing.T) {
	st := newStore(t.TempDir(), 100)
	st.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "replayed"})

	id, ch, replay := st.subscribe(api.LogQuery{Service: "backend"})
	defer st.unsubscribe(id)
	if len(replay) != 1 || replay[0].Msg != "replayed" {
		t.Fatalf("replay = %v, want [replayed]", msgsOf(replay))
	}

	st.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "live"})
	select {
	case r := <-ch:
		if r.Msg != "live" {
			t.Fatalf("live record = %q, want live", r.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("live record not delivered")
	}

	// A record for another service must not reach this backend-filtered subscriber.
	st.add(api.LogRecord{Service: "frontend", Level: "INFO", Msg: "other"})
	select {
	case r := <-ch:
		t.Fatalf("unexpected cross-service delivery: %q", r.Msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStoreSlowSubscriberEvictedNotBlocking(t *testing.T) {
	st := newStore(t.TempDir(), 100)
	id, _, _ := st.subscribe(api.LogQuery{})
	defer st.unsubscribe(id)

	// Never drain the subscriber; add far more than the queue holds. add must
	// return promptly (evict-on-full), never block the caller (the tailer).
	done := make(chan struct{})
	go func() {
		for i := 0; i < subQueueSize*4; i++ {
			st.add(api.LogRecord{Service: "backend", Level: "INFO", Msg: "flood"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("add blocked on a full slow subscriber (tailer would stall)")
	}
}
