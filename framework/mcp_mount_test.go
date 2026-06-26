package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Gap: framework.WithMCP actually mounts /mcp in Start() ──────────

func TestWithMCP_MountsEndpoint(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP()))
	defer cleanup()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /mcp status %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %v", resp.Error)
	}
	if resp.Result.ProtocolVersion == "" {
		t.Error("initialize result missing protocolVersion")
	}
}

func TestWithMCP_NotMountedByDefault(t *testing.T) {
	// Without WithMCP, /mcp is not registered by the framework — a client
	// must not get a valid initialize response.
	app, cleanup := startApp(t, NewApp())
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "protocolVersion") {
		t.Errorf("/mcp should not be mounted without WithMCP, but initialize succeeded: %s", rec.Body.String())
	}
}

// TestWithMCP_SetsServerNameFromConfig confirms the app's Config.Name is
// advertised as serverInfo.name in the handshake.
func TestWithMCP_SetsServerNameFromConfig(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithConfig(AppConfig{Name: "myapp"}), WithMCP()))
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(rec, req)

	var resp struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Result.ServerInfo.Name != "myapp" {
		t.Errorf("serverInfo.name = %q, want myapp", resp.Result.ServerInfo.Name)
	}
}
