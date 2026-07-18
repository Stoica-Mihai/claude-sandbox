package main

import (
	"bytes"
	"io"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// scrollbackLines caps the emulator's main-screen scrollback history.
const scrollbackLines = 2000

// altScreenEnterSeq switches a viewer into alternate screen mode during replay.
var altScreenEnterSeq = "\x1b[?1049h"

// drainSentinel unblocks the response-drain goroutine before Close. It rides
// the emulator's unbuffered response pipe, so it always arrives as one read.
const drainSentinel = "\x00claude-sandbox-drain-stop\x00"

// termState mirrors the session's terminal contents in a vt emulator so a
// joining viewer gets an exact styled snapshot instead of a raw byte replay.
type termState struct {
	mu            sync.Mutex
	emu           *vt.Emulator
	cursorVisible bool
	closeOnce     sync.Once
	drained       chan struct{} // closed when the drain goroutine exits
}

func newTermState(cols, rows int) *termState {
	ts := &termState{
		emu:           vt.NewEmulator(cols, rows),
		cursorVisible: true,
		drained:       make(chan struct{}),
	}
	ts.emu.SetScrollbackSize(scrollbackLines)
	// Fires inside emu.Write, so ts.mu is already held.
	ts.emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { ts.cursorVisible = v },
	})
	// The emulator answers terminal queries (DA/DSR/…) into an io.Pipe whose
	// writer blocks until read. Viewers answer queries, so drain the responses.
	go func() {
		defer close(ts.drained)
		buf := make([]byte, 4096)
		for {
			n, err := ts.emu.Read(buf)
			if err != nil || bytes.Contains(buf[:n], []byte(drainSentinel)) {
				return
			}
		}
	}()
	return ts
}

// Write feeds PTY output into the emulator.
func (ts *termState) Write(p []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	_, _ = ts.emu.Write(p)
}

// Resize resizes both emulator screens.
func (ts *termState) Resize(cols, rows uint16) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.emu.Resize(int(cols), int(rows))
}

// Close ends the response-drain goroutine, then releases the emulator. The
// sentinel-then-wait order exists because Emulator.Close races with a
// concurrent Emulator.Read (unsynchronized closed flag in the library).
func (ts *termState) Close() {
	ts.closeOnce.Do(func() {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		_, _ = io.WriteString(ts.emu.InputPipe(), drainSentinel)
		<-ts.drained
		_ = ts.emu.Close()
	})
}

// screenLine assembles row y of the active screen as a uv.Line.
func (ts *termState) screenLine(y int) uv.Line {
	w := ts.emu.Width()
	line := make(uv.Line, w)
	for x := range w {
		if c := ts.emu.CellAt(x, y); c != nil {
			line[x] = *c
		} else {
			line[x] = uv.EmptyCell
		}
	}
	return line
}

// Snapshot renders the terminal state as one replayable byte sequence: reset,
// scrollback history scrolled fully out of the viewport, alt-screen re-enter
// when active, the screen painted row-by-row at absolute positions, and the
// cursor restored (position and visibility).
func (ts *termState) Snapshot() []byte {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	var b strings.Builder
	b.WriteString(termReset)

	h := ts.emu.Height()
	// After n history lines (each followed by CRLF), h-1 extra newlines scroll
	// exactly those n lines out, leaving a blank viewport for the screen paint.
	if n := ts.emu.ScrollbackLen(); n > 0 {
		sb := ts.emu.Scrollback()
		for i := range n {
			b.WriteString(sb.Line(i).Render())
			b.WriteString("\r\n")
		}
		b.WriteString(strings.Repeat("\r\n", h-1))
	}

	if ts.emu.IsAltScreen() {
		b.WriteString(altScreenEnterSeq)
	}

	for y := range h {
		b.WriteString(ansi.CursorPosition(1, y+1))
		b.WriteString(ts.screenLine(y).Render())
	}

	pos := ts.emu.CursorPosition()
	b.WriteString(ansi.CursorPosition(pos.X+1, pos.Y+1))
	if !ts.cursorVisible {
		b.WriteString(ansi.HideCursor)
	}
	return []byte(b.String())
}
