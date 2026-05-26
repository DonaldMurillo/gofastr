package dev_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

func TestEnabledMatrix(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		wantOn     bool
	}{
		{"default off", map[string]string{}, false},
		{"dev set, prod env => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "production"}, false},
		{"dev set, prod (uppercase) => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "PRODUCTION"}, false},
		{"dev set, prod abbrev => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "prod"}, false},
		{"dev set, live alias => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "live"}, false},
		{"dev set, staging alias => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "staging"}, false},
		{"dev set, non-prod => on", map[string]string{"GOFASTR_DEV": "1"}, true},
		{"dev=true => on", map[string]string{"GOFASTR_DEV": "true"}, true},
		{"dev=0 => off", map[string]string{"GOFASTR_DEV": "0"}, false},
		{"dev=false => off (must use ParseBool semantics)", map[string]string{"GOFASTR_DEV": "false"}, false},
		{"dev=no => off (must use ParseBool semantics)", map[string]string{"GOFASTR_DEV": "no"}, false},
		{"dev set, opt-out via _LIVERELOAD=0 => off", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_DEV_LIVERELOAD": "0"}, false},
		{"dev set, _LIVERELOAD=1 explicit => on", map[string]string{"GOFASTR_DEV": "1", "GOFASTR_DEV_LIVERELOAD": "1"}, true},
		{"no dev, _LIVERELOAD=1 alone => off", map[string]string{"GOFASTR_DEV_LIVERELOAD": "1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k := range envKeys {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := dev.LiveReloadEnabled(); got != tc.wantOn {
				t.Fatalf("LiveReloadEnabled=%v, want %v (env=%v)", got, tc.wantOn, tc.env)
			}
		})
	}
}

var envKeys = map[string]struct{}{
	"GOFASTR_DEV":            {},
	"GOFASTR_ENV":            {},
	"GOFASTR_DEV_LIVERELOAD": {},
}

func TestRegisterServesScript(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)

	req := httptest.NewRequest(http.MethodGet, dev.LiveReloadScriptURL, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type=%q, want javascript", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "EventSource") {
		t.Fatalf("script body missing EventSource:\n%s", body)
	}
	if !strings.Contains(body, "location.reload") {
		t.Fatalf("script body missing reload trigger:\n%s", body)
	}
}

func TestRegisterSSEReadyEvent(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, dev.LiveReloadStreamURL, nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(rr, req)
		close(done)
	}()
	<-done

	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%q, want text/event-stream", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: ready") {
		t.Fatalf("SSE missing ready event:\n%s", body)
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)
	// Second call must not panic / double-register.
	dev.RegisterLiveReload(r)

	req := httptest.NewRequest(http.MethodGet, dev.LiveReloadScriptURL, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 after double-register", rr.Code)
	}
	_, _ = io.Copy(io.Discard, rr.Body)
}

// Upgrade path: a host that already has its own /__livereload route
// must not crash when MaybeRegisterLiveReload fires. Pre-existing
// patterns win; auto-wiring detects them and skips.
func TestRegisterSkipsWhenRoutePreExists(t *testing.T) {
	r := router.New()
	// Simulate a host with their own hand-rolled livereload.
	r.Get(dev.LiveReloadScriptURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// Auto-register must not panic on duplicate.
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("RegisterLiveReload panicked on duplicate route: %v", rec)
		}
	}()
	dev.RegisterLiveReload(r)

	// The host's handler must still be the one serving the route.
	req := httptest.NewRequest(http.MethodGet, dev.LiveReloadScriptURL, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusTeapot {
		t.Fatalf("host's route was clobbered (status=%d, expected 418 from host handler)", rr.Code)
	}
}

// SSE handler must release the goroutine when the heartbeat Write
// fails (server going down, client RST). Drives a real heartbeat tick
// against a writer that errors so the bail path is actually exercised
// — context cancel is a separate code path and doesn't prove the
// Write-error branch works.
func TestSSEBailsOnHeartbeatWriteError(t *testing.T) {
	// Compress the heartbeat so the test runs in milliseconds, not 25s.
	restore := dev.SetHeartbeatIntervalForTest(t, 20*time.Millisecond)
	defer restore()

	r := router.New()
	dev.RegisterLiveReload(r)

	req := httptest.NewRequest(http.MethodGet, dev.LiveReloadStreamURL, nil)
	// Long-lived ctx — we want to prove the handler bails on the
	// Write error, NOT on context cancellation. If the handler only
	// returned via ctx-done, this test would deadlock.
	req = req.WithContext(context.Background())
	rw := &countingErrWriter{}

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(rw, req)
		close(done)
	}()

	// Let the ready-event Write succeed, then flip the writer to
	// error mode so the next heartbeat tick gets an error.
	time.Sleep(5 * time.Millisecond)
	rw.failFrom.Store(rw.writeCount.Load() + 1)

	select {
	case <-done:
		// Pass: handler returned on Write error.
	case <-time.After(2 * time.Second):
		t.Fatalf("handler did not return after Write error; writes seen=%d", rw.writeCount.Load())
	}
	// Sanity: at least one Write succeeded (ready event) and at
	// least one errored (heartbeat).
	if rw.writeCount.Load() < 2 {
		t.Fatalf("expected at least 2 Write calls, got %d", rw.writeCount.Load())
	}
	if rw.errCount.Load() == 0 {
		t.Fatal("no Write error was returned to the handler — test didn't exercise the bail path")
	}
}

// countingErrWriter records every Write call. After failFrom is set
// (to a write-count threshold), subsequent Writes return io.ErrClosedPipe
// — simulating a dead client connection.
type countingErrWriter struct {
	hdr        http.Header
	status     int
	writeCount atomic.Int32
	errCount   atomic.Int32
	failFrom   atomic.Int32 // 0 = always succeed; N = error from Nth call onward
}

func (e *countingErrWriter) Header() http.Header {
	if e.hdr == nil {
		e.hdr = http.Header{}
	}
	return e.hdr
}
func (e *countingErrWriter) WriteHeader(code int) { e.status = code }
func (e *countingErrWriter) Write(p []byte) (int, error) {
	n := e.writeCount.Add(1)
	threshold := e.failFrom.Load()
	if threshold > 0 && n >= threshold {
		e.errCount.Add(1)
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func (e *countingErrWriter) Flush() {}
