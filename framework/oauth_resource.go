package framework

// oauth_resource.go: OAuth 2.0 Protected Resource Metadata (RFC 9728).
//
// A GoFastr app whose API accepts bearer access tokens (e.g. battery/auth's
// JWT bearer auth) is an OAuth "protected resource". RFC 9728 lets it
// publish a metadata document at /.well-known/oauth-protected-resource so a
// client or authorization server can discover how to talk to it: which
// authorization servers mint accepted tokens, which scopes are supported,
// how to present a bearer token, where the signing JWKS lives, etc.
//
// Opt-in via WithOAuthProtectedResource. The framework only serves the
// document; emitting the companion WWW-Authenticate: resource_metadata
// header on 401s (RFC 9728 §5) is left to the host's auth middleware so it
// can be scoped to the exact token-protected routes.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// OAuthProtectedResourceConfig configures the
// /.well-known/oauth-protected-resource endpoint (RFC 9728).
type OAuthProtectedResourceConfig struct {
	// Resource is the protected resource's identifier: an absolute https
	// URL with no fragment (a query component is discouraged but allowed).
	// REQUIRED by RFC 9728.
	Resource string

	// AuthorizationServers lists OAuth authorization-server issuer
	// identifiers (per RFC 8414) whose tokens this resource accepts.
	AuthorizationServers []string

	// ScopesSupported lists the scope values accepted in access requests
	// to this resource.
	ScopesSupported []string

	// BearerMethodsSupported lists how a bearer token may be presented:
	// "header", "body", and/or "query" (RFC 6750). Defaults to ["header"].
	BearerMethodsSupported []string

	// JWKSURI is the https URL of the resource's JWK Set (public signing
	// keys the resource uses to sign responses, if any).
	JWKSURI string

	// ResourceName is a human-readable name for display.
	ResourceName string

	// ResourceDocumentation is a https URL with developer info.
	ResourceDocumentation string

	// ResourcePolicyURI is a https URL describing data-use policy.
	ResourcePolicyURI string

	// ResourceTOSURI is a https URL describing terms of service.
	ResourceTOSURI string
}

// WithOAuthProtectedResource serves /.well-known/oauth-protected-resource
// (RFC 9728) from cfg. Use it when the app exposes OAuth-token-protected
// resources so clients can discover how to obtain and present tokens.
func WithOAuthProtectedResource(cfg OAuthProtectedResourceConfig) AppOption {
	return func(a *App) {
		a.oauthResource = &cfg
	}
}

// oauthResourceWellKnownPath is the well-known prefix of RFC 9728.
const oauthResourceWellKnownPath = "/.well-known/oauth-protected-resource"

// metadataPaths returns the paths the document is served at: the bare
// well-known prefix, and when the resource identifier has a path, the
// prefix followed by it (RFC 9728 §3.1), which is the URL a compliant
// client builds for https://api.example.com/mcp and the only one whose
// document §3.3 lets it accept. A path with anything outside the
// unreserved characters and "/" is not given a route of its own: it
// could not be a literal route pattern.
func (c *OAuthProtectedResourceConfig) metadataPaths() []string {
	paths := []string{oauthResourceWellKnownPath}
	u, err := url.Parse(c.Resource)
	if err != nil {
		return paths
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" || p != u.EscapedPath() && p+"/" != u.EscapedPath() {
		return paths
	}
	for _, r := range p {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~/", r)) {
			return paths
		}
	}
	if strings.Contains(p, "//") || strings.Contains(p, "/./") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/.") || strings.HasSuffix(p, "/..") {
		return paths
	}
	return append(paths, oauthResourceWellKnownPath+p)
}

// handleOAuthProtectedResource emits the RFC 9728 metadata document.
func (a *App) handleOAuthProtectedResource(w http.ResponseWriter, _ *http.Request) {
	cfg := a.oauthResource
	if cfg == nil {
		http.NotFound(w, nil)
		return
	}
	bearer := cfg.BearerMethodsSupported
	if len(bearer) == 0 {
		bearer = []string{"header"}
	}
	doc := map[string]any{
		"resource": cfg.Resource,
	}
	if len(cfg.AuthorizationServers) > 0 {
		doc["authorization_servers"] = cfg.AuthorizationServers
	}
	if len(cfg.ScopesSupported) > 0 {
		doc["scopes_supported"] = cfg.ScopesSupported
	}
	doc["bearer_methods_supported"] = bearer
	if cfg.JWKSURI != "" {
		doc["jwks_uri"] = cfg.JWKSURI
	}
	if cfg.ResourceName != "" {
		doc["resource_name"] = cfg.ResourceName
	}
	if cfg.ResourceDocumentation != "" {
		doc["resource_documentation"] = cfg.ResourceDocumentation
	}
	if cfg.ResourcePolicyURI != "" {
		doc["resource_policy_uri"] = cfg.ResourcePolicyURI
	}
	if cfg.ResourceTOSURI != "" {
		doc["resource_tos_uri"] = cfg.ResourceTOSURI
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(doc)
}
