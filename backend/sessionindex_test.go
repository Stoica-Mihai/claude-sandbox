package main

import (
	"os"
	"regexp"
	"testing"
)

func TestHasTranscript(t *testing.T) {
	withTx := testUUID1
	without := testUUID2
	seedTranscript(t, withTx, "/workspace/foo")
	if !hasTranscript(withTx) {
		t.Error("expected transcript to be found")
	}
	if hasTranscript(without) {
		t.Error("expected no transcript for never-messaged uuid")
	}
}

func TestNewUUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 100; i++ {
		u := newUUID()
		if !re.MatchString(u) {
			t.Fatalf("invalid UUIDv4: %q", u)
		}
	}
	if newUUID() == newUUID() {
		t.Fatal("uuids should be unique")
	}
}

func TestSessionIndex(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	idx := loadSessionIndex()
	idx.add("u1", "/workspace/a", 100)
	idx.add("u2", "/workspace/a", 200)
	idx.add("u3", "/workspace/b", 150)
	idx.add("u1", "/workspace/a", 999) // duplicate: ignored

	if got := idx.name("u1"); got != "" {
		t.Fatalf("expected no name, got %q", got)
	}
	idx.setName("u1", "first")
	if got := idx.name("u1"); got != "first" {
		t.Fatalf("name = %q, want first", got)
	}

	if cwd, ok := idx.cwd("u3"); !ok || cwd != "/workspace/b" {
		t.Fatalf("cwd(u3) = %q,%v", cwd, ok)
	}
	if _, ok := idx.cwd("nope"); ok {
		t.Fatal("cwd(nope) should be missing")
	}

	a := idx.listByCwd("/workspace/a")
	if len(a) != 2 || a[0].UUID != "u2" || a[1].UUID != "u1" {
		t.Fatalf("listByCwd not newest-first: %+v", a)
	}
	if a[1].Name != "first" {
		t.Fatalf("name not carried into list: %+v", a[1])
	}
	if a[0].Created != 200 { // duplicate add must not have updated created
		t.Fatalf("created mutated: %d", a[0].Created)
	}

	// Persistence: a fresh load sees the same data.
	idx2 := loadSessionIndex()
	if idx2.name("u1") != "first" || len(idx2.listByCwd("/workspace/a")) != 2 {
		t.Fatal("index did not persist to disk")
	}
}

func TestSessionIndexRemovePersists(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	idx := loadSessionIndex()
	idx.add("u1", "/workspace/a", 100)
	idx.add("u2", "/workspace/a", 200)

	idx.remove("u1")
	if _, ok := idx.cwd("u1"); ok {
		t.Fatal("remove did not drop the entry")
	}
	if _, ok := idx.cwd("u2"); !ok {
		t.Fatal("remove dropped an unrelated entry")
	}

	idx2 := loadSessionIndex()
	if _, ok := idx2.cwd("u1"); ok {
		t.Fatal("removal did not persist across reload")
	}
	if _, ok := idx2.cwd("u2"); !ok {
		t.Fatal("remaining entry did not persist across reload")
	}
}

func TestDeleteHistoryRemovesEntryAndTranscript(t *testing.T) {
	// No meta dir in the tempdir, so DeleteHistory's discover/kill step is a
	// no-op and only the index entry + transcript are touched.
	uuid := testUUID1
	_, tx := seedTranscript(t, uuid, "/workspace/a")

	idx := loadSessionIndex()
	idx.add(uuid, "/workspace/a", 100)
	sm := &SessionManager{index: idx, store: newSessionStore()}

	if err := sm.DeleteHistory(uuid); err != nil {
		t.Fatalf("DeleteHistory returned error: %v", err)
	}
	if _, ok := idx.cwd(uuid); ok {
		t.Fatal("DeleteHistory did not drop the index entry")
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatalf("DeleteHistory did not remove the transcript: stat err = %v", err)
	}
}

func TestDeleteHistoryUnknownUUIDErrors(t *testing.T) {
	known := testUUID1
	unknown := testUUID2
	_, tx := seedTranscript(t, known, "/workspace/a")

	idx := loadSessionIndex()
	idx.add(known, "/workspace/a", 100)
	sm := &SessionManager{index: idx, store: newSessionStore()}

	if err := sm.DeleteHistory(unknown); err == nil {
		t.Fatal("DeleteHistory(unknown) should return an error")
	}
	if _, err := os.Stat(tx); err != nil {
		t.Fatalf("DeleteHistory(unknown) must not delete any transcript: stat err = %v", err)
	}
	if _, ok := idx.cwd(known); !ok {
		t.Fatal("DeleteHistory(unknown) must not touch the index")
	}
}
