package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Pins: waitReady's deadline is operative mid-attempt — a booted app that
// accepts the connection but never responds fails readiness by the deadline
// (probeClient's timeout bounds each attempt under the loop budget), instead
// of hanging the bench past it.
//
// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}) — "an app that accepts the connection but
// never responds would block the probe forever and the deadline below
// would never fire".
//
// Surfaces: cmd/bench-resources/main.go::waitReady — probeClient.Get inside
// the deadline loop; the load loop carries the same client. Before the fix
// the deadline was dead code mid-attempt: time.Now().Before(end) was
// checked only between attempts, so one wedged bare http.Get outlived the
// entire 5s budget.
//
// Finding (probe, 2026-09-06/07): a never-responding peer held
// waitReady(url, 5s) past an 8s watchdog — the bench row never reported
// "did not become ready in 5s", the runner just hung. Fix: probe through a
// Timeout-bearing client sized under the loop budget (the bootProbeClient
// shape), so one hung attempt costs the deadline instead of infinity.

func TestWaitReadyDeadlineBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // accepts, never responds
	}))
	defer srv.Close()
	defer close(release)

	done := make(chan bool, 1)
	go func() {
		done <- waitReady(srv.URL, 5*time.Second)
	}()

	select {
	case <-done:
		// bounded: readiness resolved (false) within the watchdog
	case <-time.After(8 * time.Second):
		t.Errorf("SECURITY: [bench-waitready-deadline] waitReady(url, 5s) was still blocked 8s in — a bare http.Get with no client timeout is checked only between attempts, so one wedged app hangs the bench forever instead of reporting \"did not become ready in 5s\"; probe through a Timeout-bearing client (TestBootProbeClientTimesOut is the repo convention)")
	}
}
