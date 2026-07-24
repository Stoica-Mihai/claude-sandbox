package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	api "claude-sandbox-api"
)

// recordSink receives every ingested record (ring buffer + SSE fan-out).
type recordSink interface {
	add(api.LogRecord)
}

const (
	defaultMaxPartial  = 1 << 20 // 1 MB unterminated-line cap
	defaultIdleTimeout = 2 * time.Second
	readChunk          = 32 * 1024
)

// fileState is the tailer's per-file cursor. It holds the file open so a
// rename (rotation) doesn't lose the descriptor — the held fd keeps reading the
// rotated inode until drained, then switches to the new file. offset is the byte
// position where the buffered partial begins (the resumable checkpoint).
type fileState struct {
	f        *os.File
	inode    uint64
	offset   int64
	partial  []byte
	resync   bool // dropping bytes until the next newline after an over-cap line
	lastData time.Time
}

// tailer polls the log directory, follows rotation by inode, and feeds complete
// lines to the sink. One goroutine owns all state; tests drive pollOnce directly.
type tailer struct {
	dir         string
	sink        recordSink
	offsets     *offsetStore
	files       map[string]*fileState
	maxPartial  int
	idleTimeout time.Duration
}

func newTailer(dir string, sink recordSink, offsets *offsetStore) *tailer {
	return &tailer{
		dir:         dir,
		sink:        sink,
		offsets:     offsets,
		files:       map[string]*fileState{},
		maxPartial:  defaultMaxPartial,
		idleTimeout: defaultIdleTimeout,
	}
}

// Run polls on the poll ticker and flushes offsets on the flush ticker until ctx
// is cancelled, then does a final drain + offset flush.
func (t *tailer) Run(ctx context.Context, poll, flush time.Duration) {
	pt := time.NewTicker(poll)
	ft := time.NewTicker(flush)
	defer pt.Stop()
	defer ft.Stop()
	for {
		select {
		case <-ctx.Done():
			t.pollOnce(time.Now())
			_ = t.offsets.flush()
			return
		case now := <-pt.C:
			t.pollOnce(now)
		case <-ft.C:
			_ = t.offsets.flush()
		}
	}
}

func (t *tailer) pollOnce(now time.Time) {
	for _, p := range logFiles(t.dir) {
		t.pollFile(p, now)
	}
	t.idleFlush(now)
}

func (t *tailer) pollFile(path string, now time.Time) {
	st, err := os.Stat(path)
	if err != nil {
		return // absent/removed — tolerate; pick up if it reappears
	}
	service := serviceFromPath(path)
	ino := inodeOf(st)
	fs := t.files[path]

	switch {
	case fs == nil:
		f, err := os.Open(path)
		if err != nil {
			return
		}
		fs = &fileState{f: f, inode: ino, lastData: now}
		if o, ok := t.offsets.get(path); ok && o.Inode == ino {
			if _, err := f.Seek(o.Offset, io.SeekStart); err == nil {
				fs.offset = o.Offset
			}
		}
		t.files[path] = fs
	case fs.inode != ino:
		// Rotation: drain the still-open old inode to EOF, flush any frozen
		// tail, then switch to the freshly-created file at offset 0.
		t.drain(fs, service, now)
		if len(fs.partial) > 0 {
			t.emit(service, string(fs.partial), now)
		}
		_ = fs.f.Close()
		f, err := os.Open(path)
		if err != nil {
			delete(t.files, path)
			return
		}
		fs.f = f
		fs.inode = ino
		fs.offset = 0
		fs.partial = nil
		fs.resync = false
		fs.lastData = now
	}

	t.drain(fs, service, now)
	t.offsets.set(path, fileOffset{Inode: fs.inode, Offset: fs.offset})
}

// drain reads the held fd from its current position to EOF, feeding bytes to the
// line processor. The fd position persists across polls, so this resumes exactly
// where the last poll stopped.
func (t *tailer) drain(fs *fileState, service string, now time.Time) {
	buf := make([]byte, readChunk)
	for {
		n, err := fs.f.Read(buf)
		if n > 0 {
			t.processData(fs, service, buf[:n], now)
		}
		if err != nil {
			return
		}
	}
}

func (t *tailer) processData(fs *fileState, service string, data []byte, now time.Time) {
	fs.lastData = now
	if fs.resync {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			fs.offset += int64(len(data))
			return
		}
		fs.resync = false
		fs.offset += int64(i + 1)
		data = data[i+1:]
	}
	fs.partial = append(fs.partial, data...)

	// Over-cap unterminated line: emit truncated raw, then resync at next newline.
	if len(fs.partial) > t.maxPartial && bytes.IndexByte(fs.partial, '\n') < 0 {
		t.emit(service, string(fs.partial), now)
		fs.offset += int64(len(fs.partial))
		fs.partial = nil
		fs.resync = true
		return
	}

	for {
		i := bytes.IndexByte(fs.partial, '\n')
		if i < 0 {
			break
		}
		t.emit(service, string(fs.partial[:i]), now)
		fs.offset += int64(i + 1)
		fs.partial = fs.partial[i+1:]
	}
	if len(fs.partial) == 0 {
		fs.partial = nil
	}
}

// idleFlush emits a buffered partial that has gone quiet (a crash-truncated tail
// on the live file), so it surfaces as raw rather than buffering forever.
func (t *tailer) idleFlush(now time.Time) {
	for path, fs := range t.files {
		if len(fs.partial) > 0 && now.Sub(fs.lastData) >= t.idleTimeout {
			t.emit(serviceFromPath(path), string(fs.partial), now)
			fs.offset += int64(len(fs.partial))
			fs.partial = nil
			t.offsets.set(path, fileOffset{Inode: fs.inode, Offset: fs.offset})
		}
	}
}

func (t *tailer) emit(service, line string, now time.Time) {
	if line == "" {
		return
	}
	t.sink.add(parseLine(service, line, now))
}

// logFiles returns the *.log paths in dir, excluding logd's own logs so it
// never tails itself.
func logFiles(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	files := make([]string, 0, len(matches))
	for _, p := range matches {
		if !strings.HasPrefix(filepath.Base(p), "logd") {
			files = append(files, p)
		}
	}
	return files
}

func serviceFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".log")
}

func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}
