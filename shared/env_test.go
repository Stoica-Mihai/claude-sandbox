package api

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMultiHandlerFanOut(t *testing.T) {
	var a, b bytes.Buffer
	h := newMultiHandler(
		slog.NewJSONHandler(&a, nil),
		slog.NewTextHandler(&b, nil),
	)
	logger := slog.New(h)
	logger.Info("hello", "k", "v")

	if !strings.Contains(a.String(), `"msg":"hello"`) || !strings.Contains(a.String(), `"k":"v"`) {
		t.Errorf("JSON handler missing record: %q", a.String())
	}
	if !strings.Contains(b.String(), "msg=hello") || !strings.Contains(b.String(), "k=v") {
		t.Errorf("text handler missing record: %q", b.String())
	}
}

func TestRotatingWriterRotatesAndShiftsGenerations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svc.log")
	w, err := newRotatingWriter(path, nil) // nil hook: no fd-2 redirect in tests
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}

	// Write, rotate, write again: the first line lands in .1, the second in live.
	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.mu.Lock()
	w.rotate()
	w.mu.Unlock()
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := readFile(t, path+".1"); got != "first\n" {
		t.Errorf(".1 = %q, want %q", got, "first\n")
	}
	if got := readFile(t, path); got != "second\n" {
		t.Errorf("live = %q, want %q", got, "second\n")
	}

	// Rotate up to the generation cap; the oldest must be discarded, not kept.
	for i := 0; i < LogGenerations+2; i++ {
		w.mu.Lock()
		w.rotate()
		w.mu.Unlock()
	}
	if _, err := os.Stat(path + "." + strconv.Itoa(LogGenerations+1)); !os.IsNotExist(err) {
		t.Errorf("generation beyond %d should not exist", LogGenerations)
	}
}

func TestInitLoggingStderrOnlyWhenLogDirUnset(t *testing.T) {
	t.Setenv("LOG_DIR", "")
	os.Unsetenv("LOG_DIR")
	logSink = nil
	InitLogging("unittest")
	if logSink != nil {
		t.Errorf("logSink should be nil when LOG_DIR is unset")
	}
}

func TestInitLoggingDegradesOnSinkFailure(t *testing.T) {
	// A path whose parent is a regular file: opening <file>/x.log fails with
	// ENOTDIR before fd 2 is ever redirected, so InitLogging must degrade.
	f := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG_DIR", filepath.Join(f, "sub"))
	logSink = nil
	InitLogging("unittest") // must not panic
	if logSink != nil {
		t.Errorf("logSink should be nil after a sink-open failure (degraded to stderr)")
	}
}

// TestFileLoggingCapturesRawStderr runs a child process (so the real fd-2
// redirect doesn't hijack the test runner's stderr) and asserts that both a
// structured slog line and a raw os.Stderr write land in the service log file.
func TestFileLoggingCapturesRawStderr(t *testing.T) {
	if os.Getenv("LOGD_CHILD") == "1" {
		InitLogging("childsvc")
		os.Stderr.WriteString("RAW_STDERR_MARKER\n")
		slog.Info("structured-marker", "answer", 42)
		return
	}

	dir := t.TempDir()
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestFileLoggingCapturesRawStderr$", "-test.v")
	cmd.Env = append(os.Environ(), "LOGD_CHILD=1", "LOG_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}

	got := readFile(t, filepath.Join(dir, "childsvc.log"))
	if !strings.Contains(got, "RAW_STDERR_MARKER") {
		t.Errorf("log file missing raw stderr write:\n%s", got)
	}
	if !strings.Contains(got, `"msg":"structured-marker"`) || !strings.Contains(got, `"service":"childsvc"`) {
		t.Errorf("log file missing structured slog JSON:\n%s", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
