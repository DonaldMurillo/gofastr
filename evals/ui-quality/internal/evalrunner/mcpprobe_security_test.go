package evalrunner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pins: bounds before decode — a response body from the untrusted
// candidate server is read through the 1 MiB byte cap before json.Decode,
// and an over-cap reply reads as no signal like a missing /mcp.
// Surfaces: evals/ui-quality/internal/evalrunner/mcpprobe.go
// probeCandidateMCP — io.LimitReader(mcpProbeBodyCap+1) in front of the
// decoder, with the +1 over-cap tail check; caller runner.go.
// Finding (2026-09-06 probe): a candidate whose /mcp answered tools/list
// with an arbitrarily large JSON array was buffered in full until the 15s
// deadline — eval-runner OOM / disk-fill by the untrusted candidate.
func TestMcpprobeUnboundedBody(t *testing.T) {
	// Oversized but cheap tools/list reply: the app_routes sentinel plus
	// deterministic filler, > 1 MiB (the repo cap convention) and far
	// under 50 MiB.
	var body strings.Builder
	body.WriteString(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"app_routes"}`)
	for i := range 24000 {
		fmt.Fprintf(&body, ",{\"name\":\"padding_tool_%06d_%s\"}", i, strings.Repeat("x", 40))
	}
	body.WriteString("]}}")
	if body.Len() <= 1<<20 {
		t.Fatalf("setup broken: probe body is %d bytes, expected > 1 MiB", body.Len())
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body.String()))
	}))
	defer srv.Close()

	tools, introspection, logTools := probeCandidateMCP(context.Background(), srv.URL)
	if tools != 0 || introspection || logTools {
		t.Errorf("SECURITY: [eval-mcpprobe-unbounded] probe decoded an oversized (>1 MiB) /mcp reply in full: "+
			"tools=%d introspection=%t logTools=%t; a body beyond the byte cap from the untrusted candidate "+
			"must read as no signal (zero tools, no introspection), like a missing /mcp",
			tools, introspection, logTools)
	}
}

// Pins: probing the untrusted candidate's /mcp is bounded per attempt — a
// candidate that accepts the connection but never responds fails the probe
// on its own (mcpProbeClient's timeout), not at the request-context wall.
// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}).
// Finding (2026-09-06/07 probe): on http.DefaultClient (zero Timeout) only
// the 15s request context ever fired, so every wedged candidate stalled the
// eval runner the full budget per attempt.
func TestMcpprobeDeadlineBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // accepts, never writes headers
	}))
	defer srv.Close()
	defer close(release)

	done := make(chan struct{}, 1)
	go func() {
		_, _, _ = probeCandidateMCP(context.Background(), srv.URL)
		done <- struct{}{}
	}()

	select {
	case <-done:
		// bounded: the probe failed fast on its own
	case <-time.After(3 * time.Second):
		t.Errorf("SECURITY: [eval-mcpprobe-deadline] probeCandidateMCP still blocked 3s into a candidate that accepts the connection and never responds — a zero-timeout client leaves only the request-context wall to fire, so every wedged candidate costs the eval runner the full context budget per attempt; probe through a Timeout-bearing client (TestBootProbeClientTimesOut is the repo convention) and keep the ctx as backstop")
	}
}
