package main

import (
	"bytes"
	"os"
	"sync"
	"testing"
)

func TestTrackAltScreenRoutesSegments(t *testing.T) {
	r := NewRelay("claude-test")
	segments := r.trackAltScreen([]byte("conv\x1b[?1049hTUI\x1b[?1049lmore"))

	if r.inAltScreen.Load() {
		t.Fatal("inAltScreen should be false after exit sequence")
	}

	var ring bytes.Buffer
	for _, s := range segments {
		ring.Write(s)
	}
	// Normal-mode content goes to the ring buffer; TUI chrome does not.
	if !bytes.Contains(ring.Bytes(), []byte("conv")) || !bytes.Contains(ring.Bytes(), []byte("more")) {
		t.Fatalf("normal segments missing conversation content: %q", ring.Bytes())
	}
	if bytes.Contains(ring.Bytes(), []byte("TUI")) {
		t.Fatalf("alt-screen content leaked into ring segments: %q", ring.Bytes())
	}
}

func TestTrackAltScreenSplitSequence(t *testing.T) {
	r := NewRelay("claude-test")
	// Enter sequence split across two chunks.
	r.trackAltScreen([]byte("ab\x1b[?10"))
	if r.inAltScreen.Load() {
		t.Fatal("should not toggle on partial sequence")
	}
	r.trackAltScreen([]byte("49hTUI"))
	if !r.inAltScreen.Load() {
		t.Fatal("should be in alt screen after completing split sequence")
	}
}

// TestRelayConcurrentAccessRaceFree drives the relay's cross-goroutine fields
// concurrently; run with -race to validate the synchronization.
func TestRelayConcurrentAccessRaceFree(t *testing.T) {
	r := NewRelay("claude-test")

	pr1, pw1, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pr2, pw2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr1.Close()
	defer pw1.Close()
	defer pr2.Close()
	defer pw2.Close()

	drain := func(f *os.File) {
		b := make([]byte, 4096)
		for {
			if _, err := f.Read(b); err != nil {
				return
			}
		}
	}
	go drain(pr1)
	go drain(pr2)

	r.ptmx.Store(pw1)
	files := []*os.File{pw1, pw2}

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(5)

	go func() { defer wg.Done(); for i := 0; i < iters; i++ { _ = r.SendInput([]byte("x")) } }()
	go func() { defer wg.Done(); for i := 0; i < iters; i++ { r.applyResize(uint16(80+i%20), 24) } }()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			r.processOutput([]byte("out\x1b[?1049hT\x1b[?1049lz"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			r.ptmx.Store(files[i%2])
			r.generation.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = r.GetLastActivity()
			_ = r.inAltScreen.Load()
		}
	}()

	wg.Wait()
}
