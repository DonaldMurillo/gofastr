package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMCPServerCard_NameDerivedFromConfig pins that the MCP server-card and
// catalog endpoints derive their identifiers from AppConfig.Name the way the
// spec requires: the reverse-DNS card name sanitizes the app name into the
// ^[a-zA-Z0-9._-]+$ pattern (spaces/underscores/dots collapse to dashes),
// while the catalog displayName carries the raw human name. A regression that
// emitted the raw name into the reverse-DNS field would break spec-validating
// clients.
func TestMCPServerCard_NameDerivedFromConfig(t *testing.T) {
	const appName = "My Cool App_v2.io"
	app, cleanup := startApp(t, NewApp(WithConfig(AppConfig{Name: appName}), WithMCP()))
	defer cleanup()

	// Server card: name must be the sanitized reverse-DNS identifier.
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("server-card status %d", rec.Code)
	}
	var card map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("server-card JSON: %v", err)
	}
	if got := card["name"]; got != "io.gofastr/my-cool-app-v2-io" {
		t.Errorf("card name = %q, want io.gofastr/my-cool-app-v2-io (sanitized)", got)
	}

	// Catalog: displayName carries the raw app name, the identifier the
	// sanitized one.
	rec = httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp/catalog.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status %d", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("catalog JSON: %v", err)
	}
	entries, _ := doc["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("catalog has no entries: %v", doc)
	}
	first := entries[0].(map[string]any)
	if got := first["displayName"]; got != appName {
		t.Errorf("catalog displayName = %q, want %q", got, appName)
	}
	if got := first["identifier"]; got != "urn:air:io.gofastr/my-cool-app-v2-io" {
		t.Errorf("catalog identifier = %q, want urn:air:io.gofastr/my-cool-app-v2-io", got)
	}
}

// TestMCPServerCard_NameFallsBackForAllInvalidChars pins the other arm of
// mcpCardName: an app name with NO spec-valid characters (everything gets
// stripped) must fall back to "app" rather than emitting an empty identifier
// — io.gofastr/<empty> would be a malformed, spec-rejecting card.
func TestMCPServerCard_NameFallsBackForAllInvalidChars(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithConfig(AppConfig{Name: "🎉✨"}), WithMCP()))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("server-card status %d", rec.Code)
	}
	var card map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("server-card JSON: %v", err)
	}
	if got := card["name"]; got != "io.gofastr/app" {
		t.Errorf("card name = %q, want io.gofastr/app (all-invalid name must fall back)", got)
	}
}
