package framework

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Each of these discovery/manifest handlers guards on "the host never
// configured me" and 404s. The guard was untested on every one of them, and
// it is the branch that runs in the common case, since the surfaces are all
// opt-in. A handler that fell through on nil config would nil-deref inside the
// route rather than 404, taking the request down with it.
func TestUnconfiguredDiscoveryHandlersReturn404(t *testing.T) {
	app := NewApp()
	cases := map[string]http.HandlerFunc{
		"web-bot-auth directory":     app.handleWebBotAuthDirectory,
		"ucp":                        app.handleUCP,
		"acp":                        app.handleACP,
		"auth.md":                    app.handleAuthMD,
		"oauth-authorization-server": app.handleOAuthAuthorizationServer,
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest("GET", "/", nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s on an unconfigured app = %d, want 404", name, rec.Code)
			}
		})
	}
}

// A nil gate would silently allow every caller, which is worse than no gate
// because it reads as protection. The option panics at wiring time instead.
func TestWithMCPGateRejectsNilGate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithMCPGate(nil) did not panic — a nil precondition allows everyone")
		}
	}()
	WithMCPGate(nil)
}

// MCPRequireUser must accept a context that carries a resolved principal, or
// gating a tool with it would lock out the callers it is meant to admit.
func TestMCPRequireUserAcceptsResolvedUser(t *testing.T) {
	ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: "u1"})
	if err := MCPRequireUser()(ctx); err != nil {
		t.Fatalf("MCPRequireUser refused an authenticated caller: %v", err)
	}
}

// resolveWellKnownBase decides the scheme every advertised absolute URL
// carries, including the Link: rel=service MCP endpoint. Its own doc comment
// weighs the forged-header risk, so both branches deserve to be pinned rather
// than inferred.
func TestResolveWellKnownBaseScheme(t *testing.T) {
	plain := httptest.NewRequest("GET", "http://app.example/x", nil)
	if got := resolveWellKnownBase(plain); got != "http://app.example" {
		t.Errorf("plain request base = %q", got)
	}

	tls := httptest.NewRequest("GET", "https://app.example/x", nil)
	tls.TLS = &tlsConnState
	if got := resolveWellKnownBase(tls); got != "https://app.example" {
		t.Errorf("TLS request base = %q, want https", got)
	}

	// A terminating proxy speaks for the scheme; the host is NOT taken from a
	// forwarded header (that was closed in v0.45.0).
	fwd := httptest.NewRequest("GET", "http://app.example/x", nil)
	fwd.Header.Set("X-Forwarded-Proto", "https")
	fwd.Header.Set("X-Forwarded-Host", "evil.example")
	if got := resolveWellKnownBase(fwd); got != "https://app.example" {
		t.Errorf("forwarded-proto base = %q, want https on the real host", got)
	}
}

// The RFC 8414 document emits each optional field only when configured. An
// unconditional emit would advertise empty capability lists, which a strict
// client reads as "supports nothing".
func TestOAuthAuthorizationServerEmitsConfiguredFields(t *testing.T) {
	app := NewApp(WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{
		Issuer:                            "https://id.example",
		AuthorizationEndpoint:             "https://id.example/authorize",
		TokenEndpoint:                     "https://id.example/token",
		IntrospectionEndpoint:             "https://id.example/introspect",
		UserinfoEndpoint:                  "https://id.example/userinfo",
		JwksURI:                           "https://id.example/jwks",
		ScopesSupported:                   []string{"openid"},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	}))

	rec := httptest.NewRecorder()
	app.handleOAuthAuthorizationServer(rec, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{
		"issuer", "authorization_endpoint", "token_endpoint",
		"introspection_endpoint", "userinfo_endpoint", "jwks_uri",
		"scopes_supported", "response_types_supported",
		"grant_types_supported", "token_endpoint_auth_methods_supported",
	} {
		if _, ok := doc[k]; !ok {
			t.Errorf("configured field %q missing from the document", k)
		}
	}
}

// An issuer-only config must NOT advertise empty lists.
func TestOAuthAuthorizationServerOmitsUnsetFields(t *testing.T) {
	app := NewApp(WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{Issuer: "https://id.example"}))
	rec := httptest.NewRecorder()
	app.handleOAuthAuthorizationServer(rec, httptest.NewRequest("GET", "/", nil))
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc) != 1 {
		t.Errorf("issuer-only config emitted %d fields: %v", len(doc), doc)
	}
}

var tlsConnState = tls.ConnectionState{}
