package main

import (
	"bytes"
	"fmt"
	"testing"
)

// TestSnapshotPlainText: written text shows up in the snapshot, which starts
// with a terminal reset.
func TestSnapshotPlainText(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("hello"))

	snap := ts.Snapshot()
	if !bytes.HasPrefix(snap, []byte(termReset)) {
		t.Fatalf("snapshot must start with a reset, got %q", snap)
	}
	if !bytes.Contains(snap, []byte("hello")) {
		t.Fatalf("snapshot missing written text: %q", snap)
	}
}

// TestSnapshotAltScreen: while the alt screen is active the snapshot re-enters
// it and paints alt content; after exit it paints main content again.
func TestSnapshotAltScreen(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("conv\x1b[?1049hTUI"))

	snap := ts.Snapshot()
	if !bytes.Contains(snap, []byte(altScreenEnterSeq)) {
		t.Fatalf("snapshot must re-enter the alt screen: %q", snap)
	}
	if !bytes.Contains(snap, []byte("TUI")) {
		t.Fatalf("snapshot missing alt-screen content: %q", snap)
	}
	if bytes.Contains(snap, []byte("conv")) {
		t.Fatalf("main-screen content painted while in alt screen: %q", snap)
	}

	ts.Write([]byte("\x1b[?1049lmore"))
	snap = ts.Snapshot()
	if bytes.Contains(snap, []byte(altScreenEnterSeq)) || bytes.Contains(snap, []byte("TUI")) {
		t.Fatalf("alt screen leaked into a main-screen snapshot: %q", snap)
	}
	if !bytes.Contains(snap, []byte("conv")) || !bytes.Contains(snap, []byte("more")) {
		t.Fatalf("snapshot missing main-screen content: %q", snap)
	}
}

// TestSnapshotSplitSequence: an escape sequence split across writes is parsed
// incrementally (the parser keeps state between chunks).
func TestSnapshotSplitSequence(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("ab\x1b[?10"))
	ts.Write([]byte("49hTUI"))

	if !bytes.Contains(ts.Snapshot(), []byte(altScreenEnterSeq)) {
		t.Fatal("split alt-screen enter sequence not recognized")
	}
}

// TestSnapshotScrollback: lines scrolled off the screen come back in the
// snapshot, before the screen paint.
func TestSnapshotScrollback(t *testing.T) {
	ts := newTermState(20, 3)
	defer ts.Close()
	for i := 1; i <= 6; i++ {
		ts.Write([]byte(fmt.Sprintf("line%d\r\n", i)))
	}

	snap := ts.Snapshot()
	for i := 1; i <= 6; i++ {
		if !bytes.Contains(snap, []byte(fmt.Sprintf("line%d", i))) {
			t.Fatalf("snapshot missing line%d: %q", i, snap)
		}
	}
	if bytes.Index(snap, []byte("line1")) > bytes.Index(snap, []byte("line6")) {
		t.Fatalf("scrollback must precede screen content: %q", snap)
	}
}

// TestSnapshotStyledOutput: SGR styling survives the render round-trip.
func TestSnapshotStyledOutput(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()
	ts.Write([]byte("\x1b[31mred\x1b[0m plain"))

	snap := ts.Snapshot()
	if !bytes.Contains(snap, []byte("red")) || !bytes.Contains(snap, []byte("plain")) {
		t.Fatalf("snapshot missing text: %q", snap)
	}
	if !bytes.Contains(snap, []byte("31")) {
		t.Fatalf("snapshot lost the color attribute: %q", snap)
	}
}

// TestSnapshotCursorHidden: DECTCEM hide is restored by the snapshot.
func TestSnapshotCursorHidden(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()

	if bytes.Contains(ts.Snapshot(), []byte("\x1b[?25l")) {
		t.Fatal("cursor hidden in a fresh snapshot")
	}
	ts.Write([]byte("\x1b[?25l"))
	if !bytes.Contains(ts.Snapshot(), []byte("\x1b[?25l")) {
		t.Fatal("snapshot must restore the hidden cursor")
	}
}

// TestSnapshotResize: the emulator follows resizes; content written after a
// resize lands on the wider screen.
func TestSnapshotResize(t *testing.T) {
	ts := newTermState(10, 3)
	defer ts.Close()
	ts.Resize(40, 10)
	ts.Write([]byte("a line wider than ten"))

	if !bytes.Contains(ts.Snapshot(), []byte("a line wider than ten")) {
		t.Fatalf("content lost after resize: %q", ts.Snapshot())
	}
}

// TestSnapshotModes: tracked DEC modes set by the program are re-asserted by
// the snapshot, and dropped again once the program resets them.
func TestSnapshotModes(t *testing.T) {
	ts := newTermState(20, 5)
	defer ts.Close()

	for _, seq := range []string{"\x1b[?2004h", "\x1b[?1000h", "\x1b[?1006h", "\x1b[?1h"} {
		if bytes.Contains(ts.Snapshot(), []byte(seq)) {
			t.Fatalf("fresh snapshot must not assert %q", seq)
		}
	}

	ts.Write([]byte("\x1b[?2004h\x1b[?1000h\x1b[?1006h\x1b[?1h"))
	snap := ts.Snapshot()
	for _, seq := range []string{"\x1b[?2004h", "\x1b[?1000h", "\x1b[?1006h", "\x1b[?1h"} {
		if !bytes.Contains(snap, []byte(seq)) {
			t.Fatalf("snapshot must re-assert %q: %q", seq, snap)
		}
	}

	ts.Write([]byte("\x1b[?2004l\x1b[?1000l"))
	snap = ts.Snapshot()
	for _, seq := range []string{"\x1b[?2004h", "\x1b[?1000h"} {
		if bytes.Contains(snap, []byte(seq)) {
			t.Fatalf("snapshot re-asserted a reset mode %q: %q", seq, snap)
		}
	}
	if !bytes.Contains(snap, []byte("\x1b[?1006h")) {
		t.Fatalf("still-set mode dropped from snapshot: %q", snap)
	}
}
