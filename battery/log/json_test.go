package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
)

// fanoutEntry encodes one record exactly the way fanoutHandler.Handle
// does: a single slog JSON line with the trailing newline stripped —
// the byte shape Sink.Write receives in production.
func fanoutEntry(t *testing.T, msg string, args ...any) []byte {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info(msg, args...)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// splitLines splits sink output into one element per entry. The sink
// appends '\n' per entry, so valid output has no blank lines and ends
// with exactly one trailing newline.
func splitLines(t *testing.T, s string) []string {
	t.Helper()
	if s != "" && !strings.HasSuffix(s, "\n") {
		t.Fatalf("output does not end in newline: %q", s)
	}
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// errWriter fails every Write, to check error propagation.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestJSONSinkWritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	s := JSONSink(JSONOpts{Writer: &buf})
	entries := [][]byte{
		fanoutEntry(t, "app.start", "app", "myapp", "go", "go1.24.1"),
		fanoutEntry(t, "http.access", "method", "GET", "path", "/users/1", "status", 200),
		fanoutEntry(t, "http.panic", "panic", "nil pointer dereference", "status", 500),
	}
	for _, e := range entries {
		if err := s.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	lines := splitLines(t, buf.String())
	if len(lines) != len(entries) {
		t.Fatalf("got %d lines, want %d (one per entry, no interleaving)", len(lines), len(entries))
	}
	for i, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line %d is not valid JSON: %v: %q", i, err, ln)
		}
		for _, key := range []string{"time", "level", "msg"} {
			if _, ok := m[key]; !ok {
				t.Errorf("line %d missing key %q: %q", i, key, ln)
			}
		}
	}
}

func TestJSONSinkPassesEntriesThroughVerbatim(t *testing.T) {
	var buf bytes.Buffer
	s := JSONSink(JSONOpts{Writer: &buf})
	raw := fanoutEntry(t, "http.access", "method", "GET", "path", "/users/1")
	// Extra capacity so an in-place append(entry, '\n') would be
	// visible in the caller's backing array.
	entry := make([]byte, len(raw), len(raw)+8)
	copy(entry, raw)

	if err := s.Write(entry); err != nil {
		t.Fatal(err)
	}
	want := string(raw) + "\n"
	if buf.String() != want {
		t.Fatalf("sink re-encoded the entry:\n got %q\nwant %q", buf.String(), want)
	}
	for i, b := range entry[len(entry):cap(entry)] {
		if b != 0 {
			t.Fatalf("Write mutated the caller's backing array at %d: %q", i, entry[:cap(entry)])
		}
	}
}

func TestJSONSinkDefaultWriterIsStdout(t *testing.T) {
	s := JSONSink(JSONOpts{}).(*jsonSink)
	if s.w != os.Stdout {
		t.Fatalf("nil Writer should default to os.Stdout, got %v", s.w)
	}
}

func TestJSONSinkRespectsLevelsThroughFanout(t *testing.T) {
	var buf bytes.Buffer
	h := newFanoutHandler([]Sink{JSONSink(JSONOpts{Writer: &buf})}, nil)
	logger := slog.New(h)

	logger.Debug("debug.dropped", "detail", "x")
	logger.Info("info.kept", "app", "myapp")
	logger.Error("error.kept")

	lines := splitLines(t, buf.String())
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (DEBUG below the default INFO level is dropped)", len(lines))
	}
	var levels []string
	for i, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
		levels = append(levels, m["level"].(string))
	}
	if levels[0] != "INFO" || levels[1] != "ERROR" {
		t.Fatalf("levels = %v, want [INFO ERROR]", levels)
	}
}

func TestJSONSinkAttrsSurviveThroughFanout(t *testing.T) {
	var buf bytes.Buffer
	h := newFanoutHandler([]Sink{JSONSink(JSONOpts{Writer: &buf})}, nil)
	slog.New(h).Info("http.access",
		"method", "GET", "path", "/users/1", "status", 200, "dur_ms", 12.5)

	lines := splitLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"method": "GET", "path": "/users/1", "status": float64(200), "dur_ms": 12.5,
	} {
		if got := m[key]; got != want {
			t.Errorf("attr %q = %v, want %v", key, got, want)
		}
	}
}

func TestJSONSinkWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	s := JSONSink(JSONOpts{Writer: &buf})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	err := s.Write(fanoutEntry(t, "app.stop"))
	if !errors.Is(err, ErrSinkClosed) {
		t.Fatalf("Write after Close = %v, want ErrSinkClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("Close not idempotent")
	}
	if buf.Len() != 0 {
		t.Fatalf("closed sink wrote %d bytes", buf.Len())
	}
}

// closeTrackingWriter records whether anyone calls Close on it.
type closeTrackingWriter struct {
	bytes.Buffer
	closed bool
}

func (w *closeTrackingWriter) Close() error { w.closed = true; return nil }

func TestJSONSinkDoesNotCloseOwnedWriter(t *testing.T) {
	w := &closeTrackingWriter{}
	s := JSONSink(JSONOpts{Writer: w})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if w.closed {
		t.Fatal("sink closed a writer it does not own (os.Stdout must never be closed)")
	}
}

func TestJSONSinkWriteErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	s := JSONSink(JSONOpts{Writer: errWriter{err: boom}})
	if err := s.Write(fanoutEntry(t, "app.start")); !errors.Is(err, boom) {
		t.Fatalf("Write = %v, want %v", err, boom)
	}
}

func TestJSONSinkConcurrentWritesDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	s := JSONSink(JSONOpts{Writer: &buf})
	const goroutines = 8
	const perGoroutine = 25

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range perGoroutine {
				var e bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&e, nil))
				logger.Info("concurrent.entry", "g", g, "i", i)
				if err := s.Write(bytes.TrimRight(e.Bytes(), "\n")); err != nil {
					t.Errorf("Write: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()

	lines := splitLines(t, buf.String())
	if len(lines) != goroutines*perGoroutine {
		t.Fatalf("got %d lines, want %d", len(lines), goroutines*perGoroutine)
	}
	seen := map[string]bool{}
	for i, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line %d torn or invalid (interleaved write): %v: %q", i, err, ln)
		}
		key := fmt.Sprintf("%v|%v|%v", m["msg"], m["g"], m["i"])
		if seen[key] {
			t.Fatalf("duplicate entry %q at line %d", key, i)
		}
		seen[key] = true
	}
}

// Compile-time interface check: the sink satisfies Sink.
var _ Sink = (*jsonSink)(nil)
var _ io.Closer = (*jsonSink)(nil)
