//go:build red

package evalrunner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
