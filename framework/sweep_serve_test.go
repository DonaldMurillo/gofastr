package framework

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

// A renewed pair that does not load (here a certificate file cut short
// mid-copy) keeps the last good pair serving; a later good pair loads.
func TestSweepServeTLSKeepsLastGoodPair(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	testCert(t, 1, certFile, keyFile)
	r := &certReloader{certFile: certFile, keyFile: keyFile, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := r.reload(); err != nil {
		t.Fatal(err)
	}
	serial := func() int64 {
		t.Helper()
		c, err := r.get(&tls.ClientHelloInfo{})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if c == nil {
			t.Fatal("get returned no certificate")
		}
		leaf, err := x509.ParseCertificate(c.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		return leaf.SerialNumber.Int64()
	}
	touch := func(path string, body []byte, at time.Time) {
		t.Helper()
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	touch(certFile, []byte("-----BEGIN CERTIFICATE-----\ncut short"), time.Now().Add(2*time.Second))
	if got := serial(); got != 1 {
		t.Fatalf("serial after a broken renewal = %d, want the last good 1", got)
	}

	newCert, newKey := filepath.Join(dir, "new-cert.pem"), filepath.Join(dir, "new-key.pem")
	testCert(t, 2, newCert, newKey)
	later := time.Now().Add(4 * time.Second)
	for from, to := range map[string]string{newCert: certFile, newKey: keyFile} {
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		touch(to, b, later)
	}
	if got := serial(); got != 2 {
		t.Fatalf("serial after a good renewal = %d, want 2", got)
	}
}

// The certificate-file source sets its own TLS 1.2 floor rather than
// leaning on the standard library's server default.
func TestSweepServeTLSFilesSourceSetsFloor(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	testCert(t, 1, certFile, keyFile)
	app := NewApp(WithTLS(TLSConfig{CertFile: certFile, KeyFile: keyFile}))
	cfg, err := app.serverTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %#x, want TLS 1.2 (%#x)", cfg.MinVersion, tls.VersionTLS12)
	}
}

// WithSecret after WithSecretRotation drops the rotation's previous
// secrets: the last explicit option decides the whole rotation window.
func TestSweepServeWithSecretDropsEarlierRotation(t *testing.T) {
	t.Setenv("GOFASTR_SECRET", "")
	t.Setenv("GOFASTR_SECRET_PREVIOUS", "")
	app := NewApp(
		WithSecretRotation("current-secret-0123456789abcdefghij", "previous-secret-0123456789abcdefghi"),
		WithSecret("replaced-secret-0123456789abcdefghi"),
	)
	if n := len(app.previousSecrets); n != 0 {
		t.Fatalf("previousSecrets has %d entries after WithSecret, want 0", n)
	}
	if string(app.secret) != "replaced-secret-0123456789abcdefghi" {
		t.Fatalf("secret = %q, want the WithSecret value", app.secret)
	}
}

// A static export with no run id (no `gofastr export` asked) writes no
// receipt, so none ships with the site.
func TestSweepServeExportReceiptNeedsARun(t *testing.T) {
	dir := t.TempDir()
	if err := writeExportReceipt(dir, "", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ExportReceiptFile)); !os.IsNotExist(err) {
		t.Fatalf("receipt stat = %v, want not-exist", err)
	}
	if err := writeExportReceipt(dir, "run-1", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ExportReceiptFile)); err != nil {
		t.Fatalf("receipt with a run id: %v", err)
	}
}

// TestHarness initializes plugins itself, so a registered plugin's wiring
// is in place before the first harness request.
func TestSweepServeHarnessInitializesPlugins(t *testing.T) {
	t.Setenv("GOFASTR_NO_SEMANTIC_COVERAGE", "1")
	app := NewApp(WithoutDefaultMiddleware())
	p := &readinessPlugin{}
	app.RegisterPlugin(p)
	TestHarness(t, app)
	if !p.called {
		t.Fatal("TestHarness left the plugin uninitialized")
	}
}

// A nested route records against its last entity segment, not the first.
func TestSweepServeEntityForPatternPrefersTheLastSegment(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "entity-nested"}))
	for _, name := range []string{"authors", "books"} {
		app.Entity(name, EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	}
	if got := app.entityForPattern("/api/authors/{id}/books"); got != "books" {
		t.Fatalf("entityForPattern = %q, want books", got)
	}
}

// RecordSemanticCoverage writes the manifest when the recording test
// ends, with no Flush from the test itself.
func TestSweepServeSemanticCoverageFlushesAtTestEnd(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	t.Setenv(semanticCoverageOptOut, "")
	t.Run("recording", func(t *testing.T) {
		app := NewApp(WithConfig(AppConfig{Name: "semcov-cleanup"}))
		app.Router().Get("/gadgets/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		RecordSemanticCoverage(t, app)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/gadgets/7", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !m.CoveredRoute("GET", "/gadgets/{id}") {
		t.Fatalf("manifest after the test ended lacks the route: %v", m.Routes)
	}
}

// A stream whose end does not finish within the drain's budget has its
// hijacked connection closed anyway.
func TestSweepServeStreamEndForcesStuckClose(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	release := make(chan struct{})
	defer close(release)
	set := &openStreams{}
	l := &longLived{cancel: func() { <-release }, set: set}
	if _, ok := l.join(); !ok {
		t.Fatal("join refused before the drain")
	}
	if !l.attach(server) {
		t.Fatal("attach refused before the drain")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	set.endAll(ctx)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read on the peer = %v, want io.EOF from the forced close", err)
	}
}

// A failed dialect probe is not remembered: the next InTx asks again.
func TestSweepServeTxDialectProbeFailureIsNotCached(t *testing.T) {
	closed := openSQLiteMem(t)
	_ = closed.Close()
	app := NewApp()
	app.DB = closed
	if _, err := app.txDialect(); err == nil {
		t.Fatal("probe of a closed DB succeeded")
	}
	app.DB = openSQLiteMem(t)
	d, err := app.txDialect()
	if err != nil {
		t.Fatalf("probe after a failure: %v", err)
	}
	if d != migrate.DialectSQLite {
		t.Fatalf("dialect = %q, want %q", d, migrate.DialectSQLite)
	}
}

// sweepSwapSetup is an interactive setup whose swap the test calls.
type sweepSwapSetup struct{ swap chan func() }

func (*sweepSwapSetup) Incomplete(context.Context) (bool, error)     { return true, nil }
func (*sweepSwapSetup) CanRunHeadless(context.Context) (bool, error) { return false, nil }
func (*sweepSwapSetup) RunSteps(context.Context) error               { return nil }
func (s *sweepSwapSetup) Handler(swap func(), _, _ http.HandlerFunc) http.Handler {
	s.swap <- swap
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}
func (*sweepSwapSetup) SetupURL(string, string) string { return "" }

// The setup swap serves the real router before the deferred OnStart hooks
// run: a request while they run is answered by the app, not the wizard.
func TestSweepServeSetupSwapServesAppDuringDeferredHooks(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	runner := &sweepSwapSetup{swap: make(chan func(), 1)}
	app := NewApp(WithoutDefaultMiddleware(), WithSetup(runner))
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	app.Router().Get("/real", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	entered, release := make(chan struct{}), make(chan struct{})
	app.OnStart(func(context.Context) error {
		close(entered)
		<-release
		return nil
	})
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start never became ready")
	}
	swapped := make(chan struct{})
	go func() { (<-runner.swap)(); close(swapped) }()
	<-entered
	resp, err := http.Get("http://" + addr + "/real")
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	close(release)
	<-swapped
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /real during the deferred hooks = %d, want 200", resp.StatusCode)
	}
}

// Shutdown closes a WebSocket with status 1001, going away.
func TestSweepServeShutdownClosesWebSocketWith1001(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	opened := make(chan *stream.WebSocketConn, 1)
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := stream.Upgrade(w, r, stream.WSConfig{CheckOrigin: func(*http.Request) bool { return true }, CloseTimeout: 50 * time.Millisecond})
		if err == nil {
			opened <- c
		}
	}))
	conn, br := wsHandshake(t, ln.Addr().String(), "/ws")
	l := &longLived{cancel: func() {}, set: &openStreams{}}
	l.setWS(<-opened)
	l.end()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0]&0x0f != 0x8 {
		t.Fatalf("no close frame: % x %v", hdr, err)
	}
	if code := uint16(hdr[2])<<8 | uint16(hdr[3]); code != 1001 {
		t.Fatalf("close code %d, want 1001", code)
	}
}

// A boot phase failing with context.Canceled while no Shutdown is under
// way is a failure, not a graceful stop.
func TestSweepServeCanceledWithoutStopFailsRun(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.OnStart(func(context.Context) error { return context.Canceled })
	err := app.Run(context.Background(), ServicesHost())
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "start hooks") {
		t.Fatalf("Run = %v, want the start hook's context.Canceled", err)
	}
}

// Start on an address already taken fails with the bind error.
func TestSweepServeStartReportsBindFailure(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	done := make(chan error, 1)
	go func() { done <- app.Start(taken.Addr().String()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "listen and serve") {
			t.Fatalf("Start = %v, want a listen and serve error", err)
		}
	case <-time.After(10 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		t.Fatal("Start served on an address already in use")
	}
}

// A WrapServer call made once the server's wrappers are being applied
// panics instead of being dropped.
func TestSweepServeWrapServerDuringWrapPanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	var late any
	app.WrapServer(func(h http.Handler) http.Handler {
		func() {
			defer func() { late = recover() }()
			app.WrapServer(func(h http.Handler) http.Handler { return h })
		}()
		return h
	})
	addr, stop := startOnRandomPort(t, app)
	_ = addr
	stop()
	if late == nil {
		t.Fatal("WrapServer while the wrappers were applied did not panic")
	}
}

// A handler that ignores its context past Shutdown's deadline has its
// connection closed, and Shutdown reports the incomplete drain.
func TestSweepServeShutdownClosesHandlerPastDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	app.Router().Get("/stuck", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		http.NewResponseController(w).Flush()
		<-release
	}))
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start never became ready")
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /stuck HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	if line, err := br.ReadString('\n'); err != nil || !strings.Contains(line, "200") {
		t.Fatalf("status line %q, %v", line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := app.Shutdown(ctx); err == nil || !strings.Contains(err.Error(), "draining http server") {
		t.Fatalf("Shutdown = %v, want a draining http server error", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.Copy(io.Discard, br); err != nil {
		t.Fatalf("connection still open after Shutdown: %v", err)
	}
	<-done
}

// An OnReady hook does not start once a Shutdown has begun.
func TestSweepServeNoReadyHookAfterShutdown(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	stopped := make(chan error, 1)
	app.OnReady(func(string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopped <- app.Shutdown(ctx)
		}()
		for !app.stopRequestedNow() {
			runtime.Gosched()
		}
	})
	var second bool
	app.OnReady(func(string) { second = true })
	if err := app.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if second {
		t.Fatal("an OnReady hook ran after Shutdown began")
	}
}
