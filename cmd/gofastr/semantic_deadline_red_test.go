//go:build red

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :533). The byte-cap arm for these same two
// helpers landed as TestSemanticRemoteRedBodyCapped
// (semantic_remote_red_test.go); the time arm is the unpinned half.
// Property: the CLI's remote semantic helpers are bounded per attempt — a
// GOFASTR_URL peer that accepts the connection but never responds must
// fail the call, not hang it.
// Surfaces: cmd/gofastr/semantic.go::remoteQuery :299 (http.Post, default
// client) and ::remoteGet :318 (http.Get, default client) — zero timeout
// on either; embedStats/embedClear dispatch runs them straight from flag
// handling.
// Finding: a wedged remote peer (frozen dev box, black-holing proxy)
// stalls the gofastr CLI forever — no error, no deadline, the process has
// to be killed by hand. Probe-verified below: a peer that never writes
// headers holds both helpers past a 3s watchdog.
// Fix direction: one package-level client with an operative Timeout (the
// bootProbeClient shape) shared by both helpers; a timed-out peer then
// surfaces as the transport error the callers already print.

func TestSemanticRemoteRedDeadline(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		srv := newSemanticRemoteRedWedgedPeer(t)
		done := make(chan struct{}, 1)
		go func() {
			_, _ = remoteQuery(srv.URL, semantic.Query{Text: "x"})
			done <- struct{}{}
		}()
		select {
		case <-done:
			// bounded: the call failed fast on its own
		case <-time.After(3 * time.Second):
			t.Errorf("SECURITY: [semantic-remote-deadline] remoteQuery still blocked 3s into a peer that accepts the connection and never responds — semantic.go:299 http.Post runs on the default client with zero timeout, so a wedged GOFASTR_URL peer hangs the CLI forever; the byte-cap arm of this property already landed (semantic_remote_red_test.go), the deadline arm needs a Timeout-bearing client")
		}
	})

	t.Run("get", func(t *testing.T) {
		srv := newSemanticRemoteRedWedgedPeer(t)
		done := make(chan struct{}, 1)
		go func() {
			_, _ = remoteGet(srv.URL + "/semantic/stats")
			done <- struct{}{}
		}()
		select {
		case <-done:
			// bounded: the call failed fast on its own
		case <-time.After(3 * time.Second):
			t.Errorf("SECURITY: [semantic-remote-deadline] remoteGet still blocked 3s into a peer that accepts the connection and never responds — semantic.go:318 http.Get runs on the default client with zero timeout, so a wedged GOFASTR_URL peer hangs the CLI forever; the byte-cap arm of this property already landed (semantic_remote_red_test.go), the deadline arm needs a Timeout-bearing client")
		}
	})
}

// newSemanticRemoteRedWedgedPeer mirrors newSemanticRemoteRedPeer's shape
// (same fixture family, one file over) for the time arm: it accepts every
// request and never writes a byte until the test releases it. Cleanup
// releases the handler before closing the server so srv.Close cannot
// block on the wedged request.
func newSemanticRemoteRedWedgedPeer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv
}
