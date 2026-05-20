package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// memSink is a concurrency-safe in-memory sink for tests.
type memSink struct {
	mu      sync.Mutex
	entries [][]byte
	closed  bool
}

func (m *memSink) Write(entry []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, append([]byte(nil), entry...))
	return nil
}
func (m *memSink) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
func (m *memSink) lines() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.entries))
	for i, e := range m.entries {
		out[i] = append([]byte(nil), e...)
	}
	return out
}

func TestFanoutWritesToEverySink(t *testing.T) {
	a, b := &memSink{}, &memSink{}
	h := newFanoutHandler([]Sink{a, b}, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)
	logger.Info("hello", "k", 1)
	if len(a.lines()) != 1 || len(b.lines()) != 1 {
		t.Fatalf("expected each sink to receive 1 entry, got a=%d b=%d", len(a.lines()), len(b.lines()))
	}
	var rec map[string]any
	if err := json.Unmarshal(a.lines()[0], &rec); err != nil {
		t.Fatalf("entry not valid JSON: %v", err)
	}
	if rec["msg"] != "hello" || rec["k"].(float64) != 1 {
		t.Fatalf("entry shape wrong: %v", rec)
	}
}

func TestFanoutRespectsLevel(t *testing.T) {
	a := &memSink{}
	h := newFanoutHandler([]Sink{a}, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(h)
	logger.Info("nope")
	logger.Warn("yes")
	if len(a.lines()) != 1 {
		t.Fatalf("level filter failed: got %d entries", len(a.lines()))
	}
}

func TestFanoutWithAttrsAndGroups(t *testing.T) {
	a := &memSink{}
	h := newFanoutHandler([]Sink{a}, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h).With("service", "api").WithGroup("req")
	logger.Info("hit", "path", "/x")

	var rec map[string]any
	if err := json.Unmarshal(a.lines()[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec["service"] != "api" {
		t.Fatalf("service attr missing: %v", rec)
	}
	req, ok := rec["req"].(map[string]any)
	if !ok || req["path"] != "/x" {
		t.Fatalf("group not applied: %v", rec)
	}
}

func TestAccessMiddlewareLogsRequest(t *testing.T) {
	sink := &memSink{}
	h := newFanoutHandler([]Sink{sink}, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	mw := accessMiddleware(logger, false)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/things", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := sink.lines()
	if len(lines) != 1 {
		t.Fatalf("want 1 entry, got %d", len(lines))
	}
	var got map[string]any
	if err := json.Unmarshal(lines[0], &got); err != nil {
		t.Fatal(err)
	}
	if got["msg"] != "http.access" {
		t.Fatalf("msg = %v", got["msg"])
	}
	if got["status"].(float64) != 201 {
		t.Fatalf("status = %v", got["status"])
	}
	if got["bytes"].(float64) != 2 {
		t.Fatalf("bytes = %v", got["bytes"])
	}
	if got["method"] != "GET" || got["path"] != "/things" {
		t.Fatalf("method/path: %v", got)
	}
}

func TestRecoveryMiddlewareLogsPanic(t *testing.T) {
	sink := &memSink{}
	h := newFanoutHandler([]Sink{sink}, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	mw := recoveryMiddleware(logger)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	lines := sink.lines()
	if len(lines) != 1 {
		t.Fatalf("want 1 entry, got %d", len(lines))
	}
	if !bytes.Contains(lines[0], []byte("http.panic")) {
		t.Fatalf("missing http.panic msg: %s", lines[0])
	}
	if !bytes.Contains(lines[0], []byte("boom")) {
		t.Fatalf("missing panic value: %s", lines[0])
	}
}

func TestFanoutEnabledChecksLevel(t *testing.T) {
	a := &memSink{}
	h := newFanoutHandler([]Sink{a}, &slog.HandlerOptions{Level: slog.LevelError})
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Info should be disabled at LevelError")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Error should be enabled")
	}
}

// TestFanoutStderrFallbackOnSinkError verifies that a failing sink does
// not silently drop entries — operators see at least one path.
func TestFanoutStderrFallbackOnSinkError(t *testing.T) {
	sink := failingSink{err: errors.New("disk on fire")}
	h := newFanoutHandler([]Sink{sink}, nil)
	logger := slog.New(h)

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	logger.Info("hello", "k", "v")

	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	got := buf.String()
	if !strings.Contains(got, "disk on fire") || !strings.Contains(got, "preview") {
		t.Fatalf("stderr did not capture sink error fallback; got=%q", got)
	}
}

type failingSink struct{ err error }

func (f failingSink) Write(_ []byte) error { return f.err }
func (f failingSink) Close() error         { return nil }
