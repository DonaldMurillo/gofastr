package framework

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func (a *App) resolveWellKnownBase(r *http.Request) string {
	if base := strings.TrimRight(a.Config.BaseURL, "/"); base != "" {
		return base
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if a.trustedProxies.ContainsAddr(r.RemoteAddr) {
		if u := r.Header.Get("X-Forwarded-Proto"); u == "http" || u == "https" {
			scheme = u
		}
	}
	return strings.TrimRight(scheme+"://"+r.Host, "/")
}

func (a *App) wellKnownVaries() bool {
	return strings.TrimRight(a.Config.BaseURL, "/") == ""
}

func (a *App) varyWellKnown(w http.ResponseWriter) {
	if !a.wellKnownVaries() {
		return
	}
	w.Header().Add("Vary", "Host")
	w.Header().Add("Vary", "X-Forwarded-Proto")
}

func (a *App) writeWellKnownJSON(w http.ResponseWriter, r *http.Request, doc any) {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		slog.Warn("well-known: encode document", "error", err)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	a.varyWellKnown(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
	w.Write([]byte("\n"))
}

func (a *App) handleAPICatalog(w http.ResponseWriter, r *http.Request) {
	base := a.resolveWellKnownBase(r)
	prefix := a.apiPrefix() // "" or "/api"
	a.writeWellKnownJSON(w, r, map[string]any{
		"linkset": []map[string]any{{
			"anchor": base + "/",
			"service-desc": []map[string]any{{
				"href": base + "/openapi.json",
				"type": "application/vnd.oai.openapi+json;version=3.0",
			}},
			"service-doc": []map[string]any{{
				"href": base + "/api/docs/",
				"type": "text/html",
			}},
			"service": []map[string]any{{
				"href": base + prefix + "/",
				"type": "application/json",
			}},
		}},
	})
}

func (a *App) handleMCPServerCard(w http.ResponseWriter, r *http.Request) {
	a.varyWellKnown(w)
	b, err := json.MarshalIndent(a.buildMCPServerCard(r), "", "  ")
	if err != nil {
		slog.Warn("well-known: encode MCP server card", "error", err)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/mcp-server-card+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
	w.Write([]byte("\n"))
}

func (a *App) handleMCPCatalog(w http.ResponseWriter, r *http.Request) {
	base := a.resolveWellKnownBase(r)
	a.writeWellKnownJSON(w, r, map[string]any{
		"specVersion": "draft",
		"entries": []map[string]any{{
			"identifier":  "urn:air:" + a.mcpCardName(),
			"displayName": a.mcpDisplayName(),
			"mediaType":   "application/mcp-server-card+json",
			"url":         base + "/mcp/server-card",
		}},
	})
}

func (a *App) buildMCPServerCard(r *http.Request) map[string]any {
	base := a.resolveWellKnownBase(r)
	_, version := a.MCP.ServerInfo()
	return map[string]any{
		"$schema":     "https://static.modelcontextprotocol.io/schemas/v1/server-card.schema.json",
		"name":        a.mcpCardName(),
		"version":     version,
		"description": a.mcpCardDescription(),
		"remotes": []map[string]any{{
			"type": "streamable-http",
			"url":  base + "/mcp",
		}},
	}
}

func (a *App) mcpCardName() string {
	app := strings.ToLower(a.Config.Name)
	if app == "" {
		app = "app"
	}
	var b strings.Builder
	for _, c := range app {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-':
			b.WriteRune(c)
		case c == ' ' || c == '_' || c == '.':
			b.WriteRune('-')
		}
	}
	s := b.String()
	if s == "" {
		s = "app"
	}
	return "io.gofastr/" + s
}

func (a *App) mcpDisplayName() string {
	if a.Config.Name != "" {
		return a.Config.Name
	}
	return "GoFastr MCP"
}

func (a *App) mcpCardDescription() string {
	if a.Config.Name != "" {
		return a.Config.Name + " MCP server"
	}
	return "GoFastr MCP server"
}

// AgentSkillEntry is one skill in /.well-known/agent-skills/index.json.
type AgentSkillEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "skill-md" (default) or "archive"
	Description string `json:"description"`
	URL         string `json:"url"`
	Digest      string `json:"digest"` // "sha256:<hex>" of the artifact at URL
}

// WithAgentSkills serves /.well-known/agent-skills/index.json listing the
// host's published Agent Skills; an empty list is a valid index.
func WithAgentSkills(skills []AgentSkillEntry) AppOption {
	return func(a *App) { a.agentSkills = skills }
}

func (a *App) handleAgentSkillsIndex(w http.ResponseWriter, r *http.Request) {
	skills := make([]AgentSkillEntry, len(a.agentSkills))
	copy(skills, a.agentSkills)
	for i := range skills {
		if skills[i].Type == "" {
			skills[i].Type = "skill-md"
		}
	}
	a.writeWellKnownJSON(w, r, map[string]any{
		"$schema": "https://schemas.agentskills.io/discovery/0.2.0/schema.json",
		"skills":  skills,
	})
}

// OAuthAuthorizationServerConfig configures
// /.well-known/oauth-authorization-server (RFC 8414).
type OAuthAuthorizationServerConfig struct {
	Issuer                            string // REQUIRED: issuer identifier URL
	AuthorizationEndpoint             string
	TokenEndpoint                     string
	IntrospectionEndpoint             string
	UserinfoEndpoint                  string
	JwksURI                           string
	ScopesSupported                   []string
	ResponseTypesSupported            []string
	GrantTypesSupported               []string
	TokenEndpointAuthMethodsSupported []string
}

// WithOAuthAuthorizationServer serves /.well-known/oauth-authorization-server
// (RFC 8414), for an app that is an OAuth2 or OpenID issuer.
func WithOAuthAuthorizationServer(cfg OAuthAuthorizationServerConfig) AppOption {
	return func(a *App) { a.oauthAuthServer = &cfg }
}

func (a *App) handleOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	cfg := a.oauthAuthServer
	if cfg == nil {
		http.NotFound(w, nil)
		return
	}
	doc := map[string]any{"issuer": cfg.Issuer}
	set := func(k string, v string) {
		if v != "" {
			doc[k] = v
		}
	}
	set("authorization_endpoint", cfg.AuthorizationEndpoint)
	set("token_endpoint", cfg.TokenEndpoint)
	set("introspection_endpoint", cfg.IntrospectionEndpoint)
	set("userinfo_endpoint", cfg.UserinfoEndpoint)
	set("jwks_uri", cfg.JwksURI)
	if len(cfg.ScopesSupported) > 0 {
		doc["scopes_supported"] = cfg.ScopesSupported
	}
	if len(cfg.ResponseTypesSupported) > 0 {
		doc["response_types_supported"] = cfg.ResponseTypesSupported
	}
	if len(cfg.GrantTypesSupported) > 0 {
		doc["grant_types_supported"] = cfg.GrantTypesSupported
	}
	if len(cfg.TokenEndpointAuthMethodsSupported) > 0 {
		doc["token_endpoint_auth_methods_supported"] = cfg.TokenEndpointAuthMethodsSupported
	}
	if ab := a.agentAuthBlock(r); ab != nil {
		doc["agent_auth"] = ab
	}
	a.writeWellKnownJSON(w, r, doc)
}
