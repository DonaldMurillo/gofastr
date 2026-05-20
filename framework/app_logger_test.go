package framework

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// syncWriter is a goroutine-safe bytes.Buffer adapter used by the
// logger contract tests to capture slog output without races.
type syncWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// TestAppLoggerDefaultsToNonNil pins that Logger() never returns nil so
// callers can use it without a nil guard.
func TestAppLoggerDefaultsToNonNil(t *testing.T) {
	app := NewApp()
	if app.Logger() == nil {
		t.Fatal("App.Logger() returned nil before SetLogger was called")
	}
}

// TestLoggerIsAppLocal pins that App.Logger does not read
// slog.Default(). The whole architectural point of an App-local logger
// is to escape the process-global; falling back to slog.Default would
// mean an unrelated slog.SetDefault call elsewhere (another library, a
// test) could redirect this App's framework logs.
//
// This test temporarily swaps slog.Default for a buffer-backed logger;
// the App must NOT route its default access logging through it.
func TestLoggerIsAppLocal(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)

	var (
		mu        sync.Mutex
		globalBuf bytes.Buffer
	)
	slog.SetDefault(slog.New(slog.NewJSONHandler(syncWriter{&mu, &globalBuf}, &slog.HandlerOptions{Level: slog.LevelInfo})))

	app := NewApp()
	app.Router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	got := globalBuf.String()
	mu.Unlock()

	if strings.Contains(got, `"path":"/probe"`) {
		t.Fatalf("framework default Logging leaked through slog.Default; captured:\n%s", got)
	}
}

// TestSetLoggerAffectsMiddleware pins the App-local logger contract:
// middleware constructed with LoggingFn(app.Logger) picks up the
// latest SetLogger value per request — no chain rewiring needed.
//
// The framework's DefaultMiddleware no longer includes LoggingFn (so
// battery/log can own access logging without duplication), so this
// test wires the middleware explicitly to exercise the contract.
func TestSetLoggerAffectsMiddleware(t *testing.T) {
	app := NewApp()

	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	app.SetLogger(slog.New(slog.NewJSONHandler(syncWriter{&mu, &buf}, &slog.HandlerOptions{Level: slog.LevelInfo})))
	app.Use(router.Middleware(middleware.LoggingFn(app.Logger)))

	app.Router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	out := buf.String()
	mu.Unlock()

	line := strings.TrimSpace(out)
	if line == "" {
		t.Fatal("no log entry captured — middleware did not use App.Logger()")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log entry is not valid JSON: %v\n%s", err, line)
	}
	if entry["msg"] != "request" {
		t.Errorf("msg = %v, want %q", entry["msg"], "request")
	}
	if entry["method"] != "GET" {
		t.Errorf("method = %v, want GET", entry["method"])
	}
	if entry["path"] != "/probe" {
		t.Errorf("path = %v, want /probe", entry["path"])
	}
	if entry["status"] == nil {
		t.Error("status missing from structured log entry")
	}
}

// TestSetLoggerNilPanics pins that nil isn't a silent no-op; callers
// intending to silence output must pass a discard logger explicitly.
func TestSetLoggerNilPanics(t *testing.T) {
	app := NewApp()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetLogger(nil) did not panic")
		}
	}()
	app.SetLogger(nil)
}

// TestWithLoggerNilPanics is the option-path counterpart — keeps the
// "logger is always non-nil" contract from drifting if WithLogger is
// later refactored to lazy-eval the option.
func TestWithLoggerNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewApp(WithLogger(nil)) did not panic")
		}
	}()
	_ = NewApp(WithLogger(nil))
}

// TestDefaultRecoveryUsesAppLogger pins that panics caught by the
// framework's default Recovery middleware route through App.Logger
// (so battery/log's swap reaches them), not slog.Default which would
// leak panic logs to stderr regardless of SetLogger.
func TestDefaultRecoveryUsesAppLogger(t *testing.T) {
	app := NewApp()
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	app.SetLogger(slog.New(slog.NewJSONHandler(syncWriter{&mu, &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))

	app.Router.Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("kaboom")
	}))
	srv := httptest.NewServer(app.Router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	mu.Lock()
	got := buf.String()
	mu.Unlock()
	if !strings.Contains(got, "panic recovered") || !strings.Contains(got, "kaboom") {
		t.Fatalf("default Recovery did not route panic through App.Logger; captured:\n%s", got)
	}
}
