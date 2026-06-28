package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestHasTranscript(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	proj := filepath.Join(dir, "projects", "-workspace-foo")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	withTx := "11111111-1111-4111-8111-111111111111"
	without := "22222222-2222-4222-8222-222222222222"
	if err := os.WriteFile(filepath.Join(proj, withTx+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
