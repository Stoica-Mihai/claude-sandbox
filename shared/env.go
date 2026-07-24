package api

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// InitLogging installs the standard slog handler every service's main shares.
//
// LOG_DIR unset (local go test, non-container runs): stderr text handler only —
// unchanged legacy behavior.
//
// LOG_DIR set (in-container): logs go to <LOG_DIR>/<service>.log as JSON AND fd
// 2 is redirected onto that file, so panics / log.Fatal / raw stderr writes are
// captured too — these bypass slog and are the lines you most need. A text
// mirror of slog output still goes to the original console so `docker logs`
// stays readable. If the file sink can't be set up, it degrades to stderr-only
// rather than crashing the service it exists to observe.
func InitLogging(service string) {
	dir := os.Getenv("LOG_DIR")
	if dir == "" {
		setStderrOnly()
		return
	}
	if err := initFileLogging(service, dir); err != nil {
		setStderrOnly()
		slog.Warn("file logging disabled, using stderr only", "error", err, "log_dir", dir)
	}
}

func setStderrOnly() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// Held for the process lifetime so the file whose fd aliases fd 2 (and the
// duplicated console) are never GC-finalized — a finalizer Close would break
// fd 2 for the whole process. Never closed.
var (
	logSink    *rotatingWriter
	logConsole *os.File
)

func initFileLogging(service, dir string) error {
	// Duplicate the console stderr before redirecting fd 2, so slog output can
	// still be mirrored to `docker logs`.
	origFd, err := unix.Dup(2)
	if err != nil {
		return err
	}
	console := os.NewFile(uintptr(origFd), "console-stderr")

	// On rotation, re-point fd 2 at the fresh file so raw stderr follows it.
	sink, err := newRotatingWriter(filepath.Join(dir, service+".log"), func(fd uintptr) {
		_ = unix.Dup2(int(fd), 2)
	})
	if err != nil {
		_ = console.Close()
		return err
	}

	// fd 2 becomes the log file: raw stderr (panics, log.Fatal) lands there.
	if err := unix.Dup2(int(sink.fd()), 2); err != nil {
		return err
	}

	logConsole = console
	logSink = sink

	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	svcAttr := []slog.Attr{slog.String("service", service)}
	fileHandler := slog.NewJSONHandler(sink, opts).WithAttrs(svcAttr)
	consoleHandler := slog.NewTextHandler(console, opts).WithAttrs(svcAttr)
	slog.SetDefault(slog.New(newMultiHandler(fileHandler, consoleHandler)))
	return nil
}

// rotatingWriter is the JSON file sink. Writes, size accounting, and rotation
// all happen under one mutex, so a concurrent slog call can't race a rotation.
// Rotation re-points fd 2 at the fresh file so raw stderr follows the rotation.
type rotatingWriter struct {
	mu          sync.Mutex
	path        string
	f           *os.File
	size        int64
	afterRotate func(newFd uintptr) // re-point fd 2 in production; nil in tests
}

func newRotatingWriter(path string, afterRotate func(uintptr)) (*rotatingWriter, error) {
	f, err := openLogFile(path)
	if err != nil {
		return nil, err
	}
	var size int64
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	return &rotatingWriter{path: path, f: f, size: size, afterRotate: afterRotate}, nil
}

func openLogFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

func (w *rotatingWriter) fd() uintptr {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Fd()
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > LogRotateCap {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate shifts generations and re-points fd 2 at a fresh file. Caller holds mu.
// Best-effort: on any failure it keeps the current sink rather than losing it.
func (w *rotatingWriter) rotate() {
	for i := LogGenerations - 1; i >= 1; i-- {
		_ = os.Rename(w.path+"."+strconv.Itoa(i), w.path+"."+strconv.Itoa(i+1))
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return
	}
	nf, err := openLogFile(w.path)
	if err != nil {
		return // current fd still valid (now pointing at .1); keep writing there
	}
	old := w.f
	w.f = nf
	w.size = 0
	if w.afterRotate != nil {
		w.afterRotate(nf.Fd())
	}
	_ = old.Close()
}

// multiHandler fans a record out to several slog handlers (there is no stdlib
// fan-out handler). Used here to write JSON to the file and text to the console.
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: next}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: next}
}

// ListenAddr resolves a ":port" listen address from envVar, falling back to
// defaultAddr (and warning) when the value is unset or not a valid port number.
func ListenAddr(envVar, defaultAddr string) string {
	v := os.Getenv(envVar)
	if v == "" {
		return defaultAddr
	}
	port := strings.TrimLeft(v, ":")
	if _, err := strconv.Atoi(port); err != nil {
		slog.Warn("ignoring invalid "+envVar, "value", v)
		return defaultAddr
	}
	return ":" + port
}

// URLFromEnv returns the trailing-slash-trimmed URL from envVar, or defaultURL
// when it is unset.
func URLFromEnv(envVar, defaultURL string) string {
	if v := os.Getenv(envVar); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultURL
}
