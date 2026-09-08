package evalrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// mcpProbeClient bounds each probe attempt: the candidate server is
// agent-built and untrusted, so a peer that accepts the connection but
// never responds must fail the probe on its own (2s, well under the 15s
// request context, which stays the outer backstop — the bootProbeClient
// shape, cmd/gofastr/examples_blueprint_test.go).
var mcpProbeClient = &http.Client{Timeout: 2 * time.Second}

// mcpProbeBodyCap bounds the bytes read from a candidate's /mcp reply: a
// tools/list answer is a few KB at most, so anything beyond the cap is a
// hostile or broken candidate, and reading it in full is an OOM vector.
// The repo cap convention is 1 MiB.
const mcpProbeBodyCap = 1 << 20

// probeCandidateMCP asks the booted candidate's /mcp for tools/list and
// summarizes the agent-facing surface: total tool count, whether the
// introspection set survived the builder (app_routes as the sentinel),
// and whether the dev-gated log tools leaked into this prod-style boot.
// A candidate without /mcp (endpoint removed, non-JSON reply, oversized
// reply) reports zero tools. That's the signal, not an error.
func probeCandidateMCP(ctx context.Context, baseURL string) (tools int, introspection, logToolsProd bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		return 0, false, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := mcpProbeClient.Do(req)
	if err != nil {
		return 0, false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false, false
	}
	var decoded struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	// Cap before decode: an over-cap read decodes as garbage/no signal.
	// The +1 makes a body of exactly the cap distinguishable from one that
	// ran over it (a hit at cap+1 is over; at cap it is not).
	limited := io.LimitReader(resp.Body, mcpProbeBodyCap+1)
	if err := json.NewDecoder(limited).Decode(&decoded); err != nil {
		return 0, false, false
	}
	// A body that decoded but ran past the cap is over-cap: treat it as no
	// signal, like a missing /mcp, rather than a truncated read of a
	// hostile reply.
	if over, err := io.Copy(io.Discard, resp.Body); over > 0 || err != nil {
		return 0, false, false
	}
	for _, tool := range decoded.Result.Tools {
		if tool.Name == "app_routes" {
			introspection = true
		}
		if strings.HasPrefix(tool.Name, "log_") {
			logToolsProd = true
		}
	}
	return len(decoded.Result.Tools), introspection, logToolsProd
}

// builderUsedMCP reports whether the builder's transcript mentions /mcp
// traffic. Soft signal: a hit means the builder at least reached for the
// MCP surface (curl'ing tools, reading the endpoint); silence means the
// debug loop went undiscovered. Paths that don't exist are skipped.
func builderUsedMCP(logPaths ...string) bool {
	for _, path := range logPaths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if bytes.Contains(body, []byte("/mcp")) {
			return true
		}
	}
	return false
}
