package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// getManifest fetches /.well-known/mcp.json from a started app.
func getManifest(app *App) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	req.Host = "manifest.test"
	app.router.ServeHTTP(rec, req)
	return rec
}

// TestMCPManifest_ServedWhenMCPMounted pins the is-agentic mcp-server
// contract: with WithMCP the manifest is 200 application/json naming the
// /mcp endpoint and its streamable-http transport, in BOTH field
// conventions (flat endpoint/transport and nested mcpServers).
func TestMCPManifest_ServedWhenMCPMounted(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "manifest-app"}), WithMCP())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	a, cleanup := startApp(t, app)
	defer cleanup()

	rec := getManifest(a)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Fatalf("Content-Type %q, want json", ct)
	}

	var doc struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Endpoint    string `json:"endpoint"`
		Transport   string `json:"transport"`
		MCPServers  map[string]struct {
			URL       string `json:"url"`
			Transport string `json:"transport"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if !strings.HasSuffix(doc.Endpoint, "/mcp") {
		t.Errorf("endpoint %q must name the /mcp endpoint", doc.Endpoint)
	}
	if doc.Transport != "streamable-http" {
		t.Errorf("transport %q, want streamable-http", doc.Transport)
	}
	if doc.Name == "" {
		t.Error("name missing")
	}
	if doc.Description == "" {
		t.Error("description missing")
	}
	srv, ok := doc.MCPServers[doc.Name]
	if !ok {
		t.Fatalf("mcpServers has no entry for %q: %+v", doc.Name, doc.MCPServers)
	}
	if !strings.HasSuffix(srv.URL, "/mcp") || srv.Transport != "streamable-http" {
		t.Errorf("mcpServers[%q] = url %q transport %q, want url ending in /mcp with streamable-http", doc.Name, srv.URL, srv.Transport)
	}
}

// TestMCPManifest_AbsentWithoutMCP is the negative control for the mount's
// gating: no WithMCP, no /mcp endpoint, so no manifest route either — a
// scanner must not be told an MCP server exists when none is mounted.
func TestMCPManifest_AbsentWithoutMCP(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "no-mcp"}))
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	a, cleanup := startApp(t, app)
	defer cleanup()

	rec := getManifest(a)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 (route must only exist under WithMCP)", rec.Code)
	}
}
