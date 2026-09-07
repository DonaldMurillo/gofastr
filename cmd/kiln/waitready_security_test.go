package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Pins: readiness authenticates the responder and fails when the serve
// child is gone — runAgent exports KILN_URL to the spawned agent (bash tool,
// --auto-approve), so "ready" must mean "the kiln we started answered", not
// "some server answered".
// Surfaces: cmd/kiln/agent.go::waitReady
// Finding: waitReady returns nil for ANY successful HTTP response — no kiln
// marker check on status, body, or headers — and runAgent never checks serve
// liveness after Start. A local impostor that wins the port race between
// pickPort and serve's bind (pickPort probes with a bare GET and falls back
// to kernel-assigned 0 when busy; TOCTOU remains between probe and bind)
// answers 200 on /kiln/world, passes readiness, and receives the agent's
// entire session: prompts, world dumps, tool calls.
// Fix direction: have waitReady verify a kiln-specific marker (header or
// body shape served only by kiln serve) and fail readiness on mismatch, so
// an impostor responder or a dead serve child stops runAgent before OMP
// launches with KILN_URL pointed at it.
func TestKilnWaitReadyRefusesImpostor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Answers 200 on every path — including the exact readiness URL
		// runAgent polls — while being nothing like kiln: no marker, no
		// world shape, just a body that says so.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-kiln"))
	}))
	defer srv.Close()

	// The exact URL shape runAgent polls (agent.go:
	// waitReady(kilnURL+"/kiln/world", 10*time.Second)).
	err := waitReady(srv.URL+"/kiln/world", 2*time.Second)
	if err == nil {
		t.Errorf("SECURITY: [kiln-waitready-impostor] waitReady accepted a responder that is not kiln (200 \"not-kiln\" on every path) — readiness is any-HTTP-response, so a local impostor winning the pickPort→bind race passes, runAgent exports KILN_URL to it, and the --auto-approve agent hands it every prompt, world dump, and tool call")
	}
}
