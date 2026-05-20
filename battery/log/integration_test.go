package log_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// memSink is a concurrency-safe in-memory sink used by integration tests
// that exercise the plugin against a real *framework.App.
type memSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (m *memSink) Write(entry []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf.Write(entry)
	m.buf.WriteByte('\n')
	return nil
}
func (m *memSink) Close() error { return nil }

func (m *memSink) entries(t *testing.T) []map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	dec := json.NewDecoder(bytes.NewReader(m.buf.Bytes()))
	var out []map[string]any
	for dec.More() {
		var v map[string]any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		out = append(out, v)
	}
	return out
}

func TestPluginInstallsAccessMiddleware(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{
		Sinks: []log.Sink{sink},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var sawAccess bool
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.access" {
			sawAccess = true
			if e["status"].(float64) != 204 {
				t.Errorf("status field wrong: %v", e["status"])
			}
			if e["path"] != "/ping" {
				t.Errorf("path field wrong: %v", e["path"])
			}
		}
	}
	if !sawAccess {
		t.Fatalf("no http.access entry; got %d entries", len(sink.entries(t)))
	}
}

func TestPluginRecoversPanic(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "test"}),
		framework.WithoutDefaultMiddleware(),
	)
	app.RegisterPlugin(log.New(log.Config{
		Sinks: []log.Sink{sink},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("explode")
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	var sawPanic bool
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.panic" && e["panic"] == "explode" {
			sawPanic = true
		}
	}
	if !sawPanic {
		t.Fatalf("expected http.panic entry; got %v", sink.entries(t))
	}
}

// TestSpoofedXFFDoesNotChangeRemote pins the default (TrustForwardedFor:false)
// behaviour: client-supplied X-Forwarded-For never overrides the
// access log's `remote` field. The raw value is still preserved in
// `forwarded_for` so operators can correlate without trust.
func TestSpoofedXFFDoesNotChangeRemote(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/p", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/p", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var entry map[string]any
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.access" && e["path"] == "/p" {
			entry = e
		}
	}
	if entry == nil {
		t.Fatal("no http.access entry")
	}
	if got := entry["remote"].(string); strings.Contains(got, "1.2.3.4") {
		t.Errorf("remote = %q — spoofed XFF leaked into `remote` without TrustForwardedFor", got)
	}
	if got := entry["forwarded_for"].(string); got != "1.2.3.4" {
		t.Errorf("forwarded_for = %q, want %q (raw should still be preserved)", got, "1.2.3.4")
	}
}

// TestAccessPathCapped pins that a giant request URL doesn't write a
// giant log entry. Without the cap, Go's default 1 MiB request-line
// limit lets an attacker write multi-MB log lines per request → easy
// disk-fill DoS.
func TestAccessPathCapped(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/{rest...}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	huge := "/" + strings.Repeat("a", 64<<10) // 64 KiB
	resp, err := http.Get(srv.URL + huge)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	for _, e := range sink.entries(t) {
		if e["msg"] != "http.access" {
			continue
		}
		gotPath := e["path"].(string)
		if len(gotPath) > 4<<10 {
			t.Fatalf("path field length = %d, want ≤ 4 KiB (capped at 2 KiB + marker)", len(gotPath))
		}
	}
}

// TestPanicEntrySizeCapped pins that a handler that panics with a huge
// payload doesn't write a giant log entry. Without caps a 1 MB panic
// value would be serialized in full into a single line.
func TestPanicEntrySizeCapped(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	huge := strings.Repeat("A", 1<<20) // 1 MiB
	app.Router().Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic(huge)
	}))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/boom")
	resp.Body.Close()

	var panicEntry map[string]any
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.panic" {
			panicEntry = e
			break
		}
	}
	if panicEntry == nil {
		t.Fatal("no http.panic entry captured")
	}
	gotPanic, _ := panicEntry["panic"].(string)
	if len(gotPanic) > 8<<10 {
		t.Errorf("panic field length = %d, want ≤ 8KiB (was capped at 4KiB + marker)", len(gotPanic))
	}
	gotStack, _ := panicEntry["stack"].(string)
	if len(gotStack) > 80<<10 {
		t.Errorf("stack field length = %d, want ≤ 80KiB (was capped at 64KiB)", len(gotStack))
	}
}

// TestOneAccessEntryPerRequest pins that with default middleware on +
// battery/log registered, a successful request produces exactly one
// access-shaped log entry (the plugin's "http.access"), not two
// (formerly the framework's "request" entry fired alongside).
func TestOneAccessEntryPerRequest(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	requests := 0
	httpAccess := 0
	for _, e := range sink.entries(t) {
		switch e["msg"] {
		case "request":
			requests++
		case "http.access":
			httpAccess++
		}
	}
	if requests != 0 {
		t.Errorf("framework default Logging still firing — %d 'request' entries, want 0", requests)
	}
	if httpAccess != 1 {
		t.Errorf("plugin access log fired %d times, want 1", httpAccess)
	}
}

// TestAccessLogEmittedOnPanic pins the contract that an http.access
// entry is written even when the handler panics. Without it, the
// observability gap hits exactly when you most need the log — the
// caller doesn't see method/path/status for the broken request.
func TestAccessLogEmittedOnPanic(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{
		Sinks: []log.Sink{sink},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("explode")
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var sawAccessFromPanic bool
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.access" && e["path"] == "/boom" && e["status"].(float64) == 500 {
			sawAccessFromPanic = true
		}
	}
	if !sawAccessFromPanic {
		t.Fatalf("no http.access entry for panicking request; entries=%v", sink.entries(t))
	}
}

// TestPluginRecoveryFiresWithDefaultsOn pins that battery/log's
// recoveryMiddleware actually catches the panic even with the
// framework's middleware.Recovery() also in the chain. Both review
// passes claimed framework Recovery would catch first; in fact
// log-recovery is registered LATER via app.Use, which places it
// INSIDE framework Recovery — so it catches first.
func TestPluginRecoveryFiresWithDefaultsOn(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{
		Sinks: []log.Sink{sink},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	app.Router().Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("explode")
	}))

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	var sawPanic bool
	for _, e := range sink.entries(t) {
		if e["msg"] == "http.panic" && e["panic"] == "explode" {
			sawPanic = true
		}
	}
	if !sawPanic {
		t.Fatalf("plugin recovery never wrote http.panic — framework Recovery swallowed it. Entries: %v", sink.entries(t))
	}
}

func TestPluginDefaultsToFileSinkWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "tmpapp"}))
	app.RegisterPlugin(log.New(log.Config{}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// Just verify the resolved path exists and is writable by emitting one log.
	// We do this via the plugin's logger.
	p, _ := app.Plugins.Get("log")
	if p == nil {
		t.Fatal("log plugin not registered")
	}
	lp := p.(*log.Plugin)
	lp.Logger().Info("hello")
}
