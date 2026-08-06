package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOAuthProtectedResource_OptionalFields pins that the RFC 9728 metadata
// document at /.well-known/oauth-protected-resource reflects every optional
// field a host configures — jwks_uri, resource_name, resource_documentation,
// resource_policy_uri, resource_tos_uri, plus the authorization_servers and
// scopes_supported lists. Each is emitted only when set, so a regression that
// dropped one would silently publish an incomplete discovery doc that a client
// following the spec could not recover from.
func TestOAuthProtectedResource_OptionalFields(t *testing.T) {
	cfg := OAuthProtectedResourceConfig{
		Resource:               "https://api.example.test",
		AuthorizationServers:   []string{"https://auth.example.test"},
		ScopesSupported:        []string{"read", "write"},
		BearerMethodsSupported: []string{"header", "body"},
		JWKSURI:                "https://api.example.test/.well-known/jwks.json",
		ResourceName:           "Example API",
		ResourceDocumentation:  "https://api.example.test/docs",
		ResourcePolicyURI:      "https://api.example.test/privacy",
		ResourceTOSURI:         "https://api.example.test/tos",
	}
	app, cleanup := startApp(t, NewApp(WithOAuthProtectedResource(cfg)))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	stringFields := map[string]string{
		"resource":               cfg.Resource,
		"jwks_uri":               cfg.JWKSURI,
		"resource_name":          cfg.ResourceName,
		"resource_documentation": cfg.ResourceDocumentation,
		"resource_policy_uri":    cfg.ResourcePolicyURI,
		"resource_tos_uri":       cfg.ResourceTOSURI,
	}
	for key, want := range stringFields {
		if got, _ := doc[key].(string); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	// Slice fields come back from JSON as []any; compare element-wise.
	checkSlice := func(key string, want []string) {
		t.Helper()
		got, _ := doc[key].([]any)
		if len(got) != len(want) {
			t.Errorf("%s = %v, want %v", key, got, want)
			return
		}
		for i, w := range want {
			if g, _ := got[i].(string); g != w {
				t.Errorf("%s[%d] = %q, want %q", key, i, g, w)
			}
		}
	}
	checkSlice("authorization_servers", cfg.AuthorizationServers)
	checkSlice("scopes_supported", cfg.ScopesSupported)
	checkSlice("bearer_methods_supported", cfg.BearerMethodsSupported)
}

// TestOAuthProtectedResource_BearerDefaultsToHeader pins the documented
// default: a host that leaves BearerMethodsSupported empty still publishes
// ["header"] (RFC 6750's primary method), not an empty or absent field.
func TestOAuthProtectedResource_BearerDefaultsToHeader(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{
		Resource: "https://api.example.test",
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	got, _ := doc["bearer_methods_supported"].([]any)
	if len(got) != 1 || got[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header] (the RFC 6750 default)", got)
	}
	// Optional fields absent when unset — they must not appear as empty strings.
	for _, key := range []string{"jwks_uri", "resource_name", "resource_documentation", "resource_policy_uri", "resource_tos_uri", "authorization_servers", "scopes_supported"} {
		if _, present := doc[key]; present {
			t.Errorf("unset optional field %q should be absent, got %v", key, doc[key])
		}
	}
}
