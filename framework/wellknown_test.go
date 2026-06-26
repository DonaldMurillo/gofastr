package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── API catalog (RFC 9727 linkset) ─────────────────────────────────

func TestAPICatalog_RouteMountedWithAPI(t *testing.T) {
	app, cleanup := startApp(t, openapiApp(t))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/api-catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (api-catalog should mount when the app has entities)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q, want application/json", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	linkset, ok := doc["linkset"].([]any)
	if !ok || len(linkset) == 0 {
		t.Fatalf("missing linkset: %v", doc)
	}
	first := linkset[0].(map[string]any)
	if _, ok := first["service-desc"]; !ok {
		t.Errorf("linkset entry missing service-desc: %v", first)
	}
}

func TestAPICatalog_NotMountedWithoutAPI(t *testing.T) {
	app, cleanup := startApp(t, NewApp()) // no entities
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/api-catalog", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 without entities, got %d", rec.Code)
	}
}

// ── MCP server card ────────────────────────────────────────────────

func TestMCPServerCard_RouteMountedWithMCP(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP()))
	defer cleanup()

	// Scanner-probed path AND spec-reserved path both serve the SEP-2127 card.
	for _, path := range []string{"/.well-known/mcp/server-card.json", "/mcp/server-card"} {
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/mcp-server-card+json") {
			t.Errorf("%s: Content-Type %q, want application/mcp-server-card+json", path, ct)
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: invalid JSON: %v", path, err)
		}
		if doc["$schema"] == nil || doc["name"] == nil || doc["version"] == nil || doc["description"] == nil {
			t.Errorf("%s: card missing required SEP-2127 fields: %v", path, doc)
		}
		// name is reverse-DNS (io.gofastr/<app>); remotes advertise /mcp.
		if name, _ := doc["name"].(string); !strings.Contains(name, "/") {
			t.Errorf("%s: name %q not reverse-DNS", path, name)
		}
		remotes, _ := doc["remotes"].([]any)
		if len(remotes) == 0 {
			t.Errorf("%s: missing remotes", path)
		}
	}
	// Catalog points at the spec-reserved card URL.
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp/catalog.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status %d", rec.Code)
	}
	var cat map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatalf("catalog invalid JSON: %v", err)
	}
	if cat["specVersion"] == nil {
		t.Errorf("catalog missing specVersion: %v", cat)
	}
}

func TestMCPServerCard_NotMountedWithoutMCP(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp/server-card.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 without WithMCP, got %d", rec.Code)
	}
}

// ── Agent skills index ─────────────────────────────────────────────

func TestAgentSkillsIndex_EmptyByDefault(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/agent-skills/index.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (empty index still satisfies discovery)", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["$schema"] == nil {
		t.Error("missing $schema")
	}
	skills, ok := doc["skills"].([]any)
	if !ok {
		t.Fatalf("skills is %T, want []", doc["skills"])
	}
	if len(skills) != 0 {
		t.Errorf("expected empty skills by default, got %v", skills)
	}
}

func TestAgentSkillsIndex_WithSkills(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithAgentSkills([]AgentSkillEntry{{
		Name: "code-review", Description: "Review code.", URL: "/.well-known/agent-skills/code-review/SKILL.md", Digest: "sha256:abc",
	}})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/agent-skills/index.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	skills, _ := doc["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %v", skills)
	}
	entry := skills[0].(map[string]any)
	if entry["type"] != "skill-md" {
		t.Errorf("type defaulted to %v, want skill-md", entry["type"])
	}
}

// ── OAuth Authorization Server (RFC 8414) ──────────────────────────

func TestOAuthAuthorizationServer(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{
		Issuer:                "https://auth.example",
		AuthorizationEndpoint: "https://auth.example/authorize",
		TokenEndpoint:         "https://auth.example/token",
		ScopesSupported:       []string{"openid", "read"},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["issuer"] != "https://auth.example" {
		t.Errorf("issuer = %v", doc["issuer"])
	}
	if doc["token_endpoint"] != "https://auth.example/token" {
		t.Errorf("token_endpoint = %v", doc["token_endpoint"])
	}
}

func TestOAuthAuthorizationServer_NotMountedByDefault(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 by default, got %d", rec.Code)
	}
}
