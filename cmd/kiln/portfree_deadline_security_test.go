package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :533) — "a raw http.Get has no timeout: an
// app that accepts the connection but never responds would block the probe
// forever". This binary already carries the twin shape: waitReady
// (agent.go:145) probes through a 500ms client.
// Pins: portFree's occupancy probe is bounded — a listener that
// accepts the connection but never responds must not hang pickPort
// forever.
// Surfaces: cmd/kiln/agent.go::portFree :128-140 — bare http.Get (default
// client, zero timeout) at :130; caller pickPort :119-126 runs before
// every kiln serve boot.
// Finding: portFree is the pre-bind port probe for every kiln session. A
// wedged listener on the desired port (dying process holding the socket,
// black-holing middlebox) stalls the probe indefinitely — runAgent never
// reaches waitReady, the process sits silently, and the operator gets no
// error, ever. Probe-verified below: a peer that never writes headers
// holds portFree past a 3s watchdog.
// Fix direction: probe through a Timeout-bearing client (the waitReady
// 500ms client two functions down is the in-file convention) or a plain
// net.Dial with deadline — connection success/refused is the whole
// signal, so a wedged responder must read as "busy" quickly, not never.

func TestPortFreeBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // accepts, never writes headers
	}))
	defer srv.Close()
	defer close(release)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("setup broken: parse httptest URL %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("setup broken: httptest port %q: %v", u.Port(), err)
	}

	done := make(chan struct{}, 1)
	go func() {
		portFree(port)
		done <- struct{}{}
	}()

	select {
	case <-done:
		// bounded: the probe returned on its own
	case <-time.After(3 * time.Second):
		t.Errorf("SECURITY: [kiln-portfree-deadline] portFree(%d) was still blocked 3s into a peer that accepts the connection and never responds — the occupancy probe (agent.go:130) is a bare http.Get on the default client, so pickPort and every kiln boot behind it hang forever instead of treating a wedged port as busy; the same file's waitReady (:145) already probes through a 500ms client", port)
	}
}
