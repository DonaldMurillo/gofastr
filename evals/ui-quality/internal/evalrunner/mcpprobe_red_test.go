//go:build red

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

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: bounds before decode — a response body from an untrusted peer
// (the agent-built candidate server being probed) must be read through a
// byte cap before json.Decode (repo convention: io.LimitReader, 1 MiB).
// Surfaces: evals/ui-quality/internal/evalrunner/mcpprobe.go
// probeCandidateMCP — :28 http.DefaultClient.Do (15s ctx), :43
// json.NewDecoder(resp.Body).Decode with no LimitReader /
// MaxBytesReader / Content-Length check; caller runner.go:639.
// Finding: a candidate whose /mcp answers tools/list with an arbitrarily
// large JSON array (or a paced never-ending stream) is buffered in full
// until the 15s deadline — eval-runner OOM / disk-fill by the untrusted
// candidate.
// Fix direction: wrap resp.Body in io.LimitReader (1 MiB) before the
// decoder and treat an over-cap read as no signal, like a missing /mcp.

func TestMcpprobeRedUnboundedBody(t *testing.T) {
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

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3) — the time arm of this file's finding;
// the byte-cap arm above is the landed half.
// Family: every outbound probe carries an operative per-attempt deadline.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :533).
// Property: probing the untrusted candidate's /mcp is bounded per attempt
// — a candidate that accepts the connection but never responds must fail
// the probe fast, not ride the whole context budget.
// Surfaces: evals/ui-quality/internal/evalrunner/mcpprobe.go:28 —
// http.DefaultClient.Do (zero Timeout); the 15s request context (:20) is
// the only bound, so the deadline wall, not the client, is what finally
// fires on a wedged candidate.
// Finding: probe-verified below — a peer that never writes headers holds
// probeCandidateMCP past a 3s watchdog (it returns only at the 15s ctx
// wall), stalling the eval runner the full budget per attempt.
// Fix direction: replace http.DefaultClient with a Timeout-bearing client
// (the bootProbeClient shape) so each attempt fails fast on its own; the
// request context stays as the outer backstop.
func TestMcpprobeRedDeadlineBounded(t *testing.T) {
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
		t.Errorf("SECURITY: [eval-mcpprobe-deadline] probeCandidateMCP still blocked 3s into a candidate that accepts the connection and never responds — mcpprobe.go:28 runs on http.DefaultClient (zero Timeout) and only the 15s request context (:20) ever fires, so every wedged candidate costs the eval runner the full context budget per attempt; probe through a Timeout-bearing client (TestBootProbeClientTimesOut is the repo convention) and keep the ctx as backstop")
	}
}
