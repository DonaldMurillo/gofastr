package framework

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// OAuthProtectedResourceConfig configures the
// /.well-known/oauth-protected-resource endpoint (RFC 9728).
type OAuthProtectedResourceConfig struct {
	// Resource is the resource identifier, required: an absolute https URL
	// with no userinfo or fragment.
	Resource string

	// AuthorizationServers are the issuer URLs whose tokens this resource
	// accepts: https, with no userinfo, query or fragment.
	AuthorizationServers []string

	ScopesSupported []string

	// BearerMethodsSupported lists "header", "body" or "query"; default
	// ["header"].
	BearerMethodsSupported []string

	JWKSURI string

	ResourceName string

	ResourceDocumentation string

	ResourcePolicyURI string

	ResourceTOSURI string
}

// WithOAuthProtectedResource serves /.well-known/oauth-protected-resource
// (RFC 9728) from cfg. Start, Serve and Run reject an invalid resource or
// authorization-server URL.
func WithOAuthProtectedResource(cfg OAuthProtectedResourceConfig) AppOption {
	return func(a *App) {
		a.oauthResource = &cfg
	}
}

func (c *OAuthProtectedResourceConfig) validate() error {
	if !validOAuthMetadataURL(c.Resource, false) {
		return fmt.Errorf("OAuth protected resource Resource must be an absolute HTTPS URL without userinfo or a fragment")
	}
	for index, issuer := range c.AuthorizationServers {
		if !validOAuthMetadataURL(issuer, true) {
			return fmt.Errorf("OAuth protected resource AuthorizationServers[%d] must be an absolute HTTPS issuer URL without userinfo, a query, or a fragment", index)
		}
	}
	return nil
}

func validOAuthMetadataURL(raw string, issuer bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || asciicase.Lower(parsed.Scheme) != "https" || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(raw, "# \t\r\n") {
		return false
	}
	return !issuer || (parsed.RawQuery == "" && !parsed.ForceQuery)
}

const oauthResourceWellKnownPath = "/.well-known/oauth-protected-resource"

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

func (a *App) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
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

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		slog.Warn("oauth resource metadata: encode document", "error", err)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
	w.Write([]byte("\n"))
}
