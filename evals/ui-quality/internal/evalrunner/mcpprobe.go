package evalrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// probeCandidateMCP asks the booted candidate's /mcp for tools/list and
// summarizes the agent-facing surface: total tool count, whether the
// introspection set survived the builder (app_routes as the sentinel),
// and whether the dev-gated log tools leaked into this prod-style boot.
// A candidate without /mcp (endpoint removed, non-JSON reply) reports
// zero tools — that's the signal, not an error.
func probeCandidateMCP(ctx context.Context, baseURL string) (tools int, introspection, logToolsProd bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		return 0, false, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
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
