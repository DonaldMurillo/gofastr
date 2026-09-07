//go:build red

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :533) — "an app that accepts the connection
// but never responds would block the probe forever and the deadline below
// would never fire".
// Property: waitReady's 5s deadline is operative mid-attempt — a booted
// app that accepts the connection but never responds must fail readiness
// by the deadline, not hang the bench past it.
// Surfaces: cmd/bench-resources/main.go::waitReady :217-230 — bare
// http.Get at :220 (default client, zero timeout) inside the deadline
// loop; the load loop's http.Get at :182 carries the same shape. The
// deadline is dead code mid-attempt: time.Now().Before(end) is checked
// only between attempts (:218-219), so one wedged attempt outlives the
// entire 5s budget.
// Finding: a wedged app binary (accepts, never responds — the exact shape
// TestBootProbeClientTimesOut guards for the boot gate) stalls waitReady
// forever: the bench row never reports "did not become ready in 5s", the
// runner just hangs. Probe-verified below: a never-responding peer holds
// waitReady(url, 5s) past an 8s watchdog.
// Fix direction: probe through a Timeout-bearing client sized under the
// loop budget (the bootProbeClient shape), so one hung attempt costs the
// deadline instead of infinity; the load loop's http.Get gets the same
// client.

func TestWaitReadyRedBounded(t *testing.T) {
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
		t.Errorf("SECURITY: [bench-waitready-deadline] waitReady(url, 5s) was still blocked 8s in — main.go:220 probes with a bare http.Get and the 5s deadline is only checked between attempts (:218-219), so one wedged app hangs the bench forever instead of reporting \"did not become ready in 5s\"; the load loop (:182) repeats the same bare http.Get. Probe through a Timeout-bearing client (TestBootProbeClientTimesOut is the repo convention)")
	}
}
