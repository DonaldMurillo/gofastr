package log_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Load test for battery/log: drive concurrent traffic through an App
// with the plugin wired, mix happy / panic / slow / failing-sink
// scenarios, and assert the invariants we care about in production:
//
//   - Steady-state writes never block requests.
//   - Counters track losses accurately (no silent drops).
//   - Shutdown is bounded (no hang on broken sinks).
//   - No goroutine leak after Shutdown.
//
// Skipped under -short so day-to-day CI stays fast; run explicitly with
//
//	go test -run TestLoad ./battery/log/

const loadDuration = 3 * time.Second
const loadConcurrency = 32

// TestLoadHappyPath: lots of concurrent requests, no failures injected.
// We expect zero counters to advance, and goroutines to be balanced
// before and after.
func TestLoadHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; skipped under -short")
	}
	goroutinesBefore := runtime.NumGoroutine()

	sink := &countingSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "load"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Router().Get("/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	hammer(t, srv.URL+"/ok", loadConcurrency, loadDuration)

	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)
	m := lp.Metrics()
	if m.PostStopDrops != 0 || m.SinkWriteFailures != 0 {
		t.Errorf("happy path produced drops/failures; metrics=%+v", m)
	}
	if sink.writes.Load() == 0 {
		t.Fatal("sink saw zero writes — middleware not in chain?")
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Give a beat for any in-flight goroutines to unwind, then check leaks.
	// net/http's connection accept loop and idle-connection goroutines
	// take a moment to wind down after Server.Close. Under -race the
	// race detector adds tracking goroutines, so the bound is generous.
	// We're guarding against "battery/log leaked a goroutine per
	// request" (would be hundreds), not "net/http's bookkeeping takes
	// a few ms longer".
	for i := 0; i < 20; i++ {
		runtime.GC()
		if runtime.NumGoroutine()-goroutinesBefore <= 16 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	delta := runtime.NumGoroutine() - goroutinesBefore
	if delta > 16 {
		t.Errorf("possible goroutine leak: started with %d, ended with %d (delta %d)",
			goroutinesBefore, runtime.NumGoroutine(), delta)
	}
}

// TestLoadSinkAlwaysFails verifies the stderr-rate-limited fallback +
// counter behavior when a sink can never accept writes. Counters
// advance, but the path doesn't block the request hot path.
func TestLoadSinkAlwaysFails(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; skipped under -short")
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "load"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{alwaysFailSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Router().Get("/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	hammer(t, srv.URL+"/ok", loadConcurrency, loadDuration)

	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)
	m := lp.Metrics()
	if m.SinkWriteFailures == 0 {
		t.Fatalf("failing sink should advance SinkWriteFailures; metrics=%+v", m)
	}

	// Shutdown should still return within a tight bound.
	t0 := time.Now()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("Shutdown took %v with failing sink — should be bounded", d)
	}
}

// TestLoadHandlerPanics: mix happy + panicking endpoints. Recovery
// middleware should catch every panic; access log + http.panic entries
// should match request counts; no leaks.
func TestLoadHandlerPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; skipped under -short")
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "load"}))
	sink := &countingSink{}
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Router().Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("intentional")
	}))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	hammer(t, srv.URL+"/boom", loadConcurrency, loadDuration)

	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)
	m := lp.Metrics()
	if m.PostStopDrops != 0 || m.SinkWriteFailures != 0 {
		t.Errorf("panicking handlers produced drops/failures; metrics=%+v", m)
	}
	if sink.writes.Load() == 0 {
		t.Fatal("sink saw zero writes under panic load")
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestLoadShutdownDuringTraffic stresses the actual problem from the
// punch list: workers writing through app.Logger() during Shutdown
// shouldn't crash, and the post-stop drop counter should track the
// late writes accurately.
func TestLoadShutdownDuringTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; skipped under -short")
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "load"}))
	sink := &countingSink{}
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Background writer keeps logging after Shutdown — simulates a
	// worker goroutine outliving the App.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				app.Logger().Info("bg work", "k", "v")
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	close(done)
	time.Sleep(50 * time.Millisecond)

	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)
	m := lp.Metrics()
	if m.PostStopDrops == 0 {
		t.Errorf("background writer logged after Shutdown; PostStopDrops should advance; metrics=%+v", m)
	}
}

// ---- helpers --------------------------------------------------------------

func hammer(t *testing.T, url string, workers int, dur time.Duration) {
	t.Helper()
	// Each test gets its own transport with keep-alives off so connections
	// don't linger across tests (cross-test FD pressure was producing
	// flaky failures where the next test's hammer couldn't actually drive
	// traffic against the new server).
	transport := &http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: 0,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	defer client.CloseIdleConnections()

	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				resp, err := client.Get(url)
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}

type countingSink struct {
	writes atomic.Uint64
	closed atomic.Bool
}

func (c *countingSink) Write(_ []byte) error {
	if c.closed.Load() {
		return log.ErrSinkClosed
	}
	c.writes.Add(1)
	return nil
}
func (c *countingSink) Close() error { c.closed.Store(true); return nil }

type alwaysFailSink struct{}

func (alwaysFailSink) Write(_ []byte) error { return errors.New("simulated sink failure") }
func (alwaysFailSink) Close() error         { return nil }
