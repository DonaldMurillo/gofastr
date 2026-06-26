package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthProtectedResource_Handler(t *testing.T) {
	app := NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{
		Resource:             "https://api.example",
		AuthorizationServers: []string{"https://auth.example"},
		ScopesSupported:      []string{"read", "write"},
		ResourceName:         "Example API",
	}))

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	app.handleOAuthProtectedResource(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q, want application/json", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	if doc["resource"] != "https://api.example" {
		t.Errorf("resource = %v", doc["resource"])
	}
	// bearer_methods_supported defaults to ["header"] when unset.
	if m, _ := doc["bearer_methods_supported"].([]any); len(m) != 1 || m[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header]", doc["bearer_methods_supported"])
	}
	as, _ := doc["authorization_servers"].([]any)
	if len(as) != 1 || as[0] != "https://auth.example" {
		t.Errorf("authorization_servers = %v", doc["authorization_servers"])
	}
	if doc["resource_name"] != "Example API" {
		t.Errorf("resource_name = %v", doc["resource_name"])
	}
}

func TestOAuthProtectedResource_NotConfigured404(t *testing.T) {
	app := NewApp()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	app.handleOAuthProtectedResource(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// ── Gap: route is actually mounted in Start() when opted in ─────────

func TestOAuthProtectedResource_RouteMounted(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{
		Resource: "https://api.example",
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("route not mounted: status %d, body %s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["resource"] != "https://api.example" {
		t.Errorf("resource = %v", doc["resource"])
	}
}

func TestOAuthProtectedResource_NotMountedByDefault(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("without the option, route should be absent (404); got %d", rec.Code)
	}
}
