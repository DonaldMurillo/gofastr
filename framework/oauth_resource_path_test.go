package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOAuthProtectedResourceServesThePathInsertedURL: RFC 9728 §3.1 puts
// the metadata of a resource whose identifier has a path at the
// well-known prefix followed by that path, so https://api.example.com/mcp
// is described at /.well-known/oauth-protected-resource/mcp, and §3.3
// has the client refuse a document whose resource does not match the
// identifier it built the URL from. The app served only the bare prefix,
// so a compliant client of a path-scoped resource (an MCP server at
// /mcp) found nothing.
func TestOAuthProtectedResourceServesThePathInsertedURL(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{
		Resource:             "https://api.example.com/mcp",
		AuthorizationServers: []string{"https://auth.example.com"},
	})))
	defer cleanup()
	for _, p := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", p, rec.Code)
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc["resource"] != "https://api.example.com/mcp" {
			t.Fatalf("GET %s resource = %v", p, doc["resource"])
		}
	}
}

// TestOAuthResourceMetadataPathsRefuseNonLiteralPaths: only a path the
// router can hold as a literal pattern gets its own route.
func TestOAuthResourceMetadataPathsRefuseNonLiteralPaths(t *testing.T) {
	for res, want := range map[string]int{
		"https://api.example.com":         1,
		"https://api.example.com/":        1,
		"https://api.example.com/mcp/":    2,
		"https://api.example.com/v1/mcp":  2,
		"https://api.example.com/{id}":    1,
		"https://api.example.com/a/../b":  1,
		"https://api.example.com/a%2Fb":   1,
		"https://api.example.com/a b":     1,
		"https://api.example.com//double": 1,
	} {
		c := OAuthProtectedResourceConfig{Resource: res}
		if got := c.metadataPaths(); len(got) != want {
			t.Errorf("%s: paths %v, want %d", res, got, want)
		}
	}
}
