package framework

// wellknown.go — the agent-readiness well-known endpoints that the
// isitagentready scanner scores. Each is auto-served when its precondition
// holds, so a host that wires the basics scores without per-route work:
//
//   - /.well-known/api-catalog                 (RFC 9727 linkset+json) — when the app has an API (entities)
//   - /.well-known/mcp/server-card.json        — when WithMCP exposes /mcp
//   - /.well-known/agent-skills/index.json     — opt-in (host declares skills)
//   - /.well-known/oauth-authorization-server  (RFC 8414) — opt-in (host is an OAuth issuer)
//
// The scanner only requires a 200 at each path; we emit real, spec-shaped
// bodies so the artifacts are useful, not just present.

import (
	"encoding/json"
	"net/http"
	"strings"
)

// resolveWellKnownBase returns the canonical origin for absolute URLs in
// well-known docs: the forwarded scheme + Host (X-Forwarded-Proto/Host
// honored). These docs live at the framework layer, which has no sitemap
// base to fall back on, so the request is the source of truth.
func resolveWellKnownBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if u := r.Header.Get("X-Forwarded-Proto"); u != "" {
		scheme = u
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return strings.TrimRight(scheme+"://"+host, "/")
}

func writeWellKnownJSON(w http.ResponseWriter, doc any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(doc)
}

// ── API catalog (RFC 9727 linkset+json) ────────────────────────────

// handleAPICatalog emits a linkset advertising the OpenAPI spec
// (service-desc) + Swagger docs (service-doc) + status. Served when the
// app has entities (i.e. /openapi.json is mounted).
func (a *App) handleAPICatalog(w http.ResponseWriter, r *http.Request) {
	base := resolveWellKnownBase(r)
	prefix := a.apiPrefix() // "" or "/api"
	writeWellKnownJSON(w, map[string]any{
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

// ── MCP server card ────────────────────────────────────────────────

// handleMCPServerCard serves the MCP Server Card (experimental extension
// SEP-2127) in the spec shape ($schema/name/version/description/remotes),
// at both the spec-reserved and scanner-probed paths. See the body comment.
func (a *App) handleMCPServerCard(w http.ResponseWriter, r *http.Request) {
	// MCP Server Card (experimental extension SEP-2127 /
	// modelcontextprotocol/experimental-ext-server-card): $schema, name
	// (reverse-DNS), version, description, remotes[]. Media type
	// application/mcp-server-card+json. Served at both GET /mcp/server-card
	// (spec-reserved) and /.well-known/mcp/server-card.json (the path
	// isitagentready probes, which the live spec discourages) so both the
	// spec and the scanner are satisfied.
	w.Header().Set("Content-Type", "application/mcp-server-card+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(a.buildMCPServerCard(r))
}

// handleMCPCatalog serves /.well-known/mcp/catalog.json — the spec-
// recommended well-known that points at the server card.
func (a *App) handleMCPCatalog(w http.ResponseWriter, r *http.Request) {
	base := resolveWellKnownBase(r)
	writeWellKnownJSON(w, map[string]any{
		"specVersion": "draft",
		"entries": []map[string]any{{
			"identifier":  "urn:air:" + a.mcpCardName(),
			"displayName": a.mcpDisplayName(),
			"mediaType":   "application/mcp-server-card+json",
			"url":         base + "/mcp/server-card",
		}},
	})
}

// buildMCPServerCard assembles the spec-shaped server card.
func (a *App) buildMCPServerCard(r *http.Request) map[string]any {
	base := resolveWellKnownBase(r)
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

// mcpCardName returns a reverse-DNS identifier for the server card
// (spec pattern ^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$), derived from Config.Name.
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

// ── Agent skills index (cloudflare/agent-skills-discovery-rfc) ─────

// AgentSkillEntry is one skill in the /.well-known/agent-skills/index.json.
// Mirrors the v0.2.0 discovery schema.
type AgentSkillEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "skill-md" (default) or "archive"
	Description string `json:"description"`
	URL         string `json:"url"`
	Digest      string `json:"digest"` // "sha256:<hex>" of the artifact at URL
}

// WithAgentSkills serves /.well-known/agent-skills/index.json enumerating
// the host's published Agent Skills (per the agent-skills-discovery-rfc).
// The host provides the entries (name/type/url/digest of each SKILL.md or
// archive it publishes); an empty list still satisfies the discovery check.
func WithAgentSkills(skills []AgentSkillEntry) AppOption {
	return func(a *App) { a.agentSkills = skills }
}

func (a *App) handleAgentSkillsIndex(w http.ResponseWriter, r *http.Request) {
	skills := a.agentSkills
	if skills == nil {
		skills = []AgentSkillEntry{}
	}
	for i := range skills {
		if skills[i].Type == "" {
			skills[i].Type = "skill-md"
		}
	}
	writeWellKnownJSON(w, map[string]any{
		"$schema": "https://schemas.agentskills.io/discovery/0.2.0/schema.json",
		"skills":  skills,
	})
}

// ── OAuth Authorization Server (RFC 8414) ──────────────────────────

// OAuthAuthorizationServerConfig configures
// /.well-known/oauth-authorization-server (RFC 8414). Relevant when the
// host acts as an OAuth2/OpenID issuer (battery/auth is a client by
// default, so this is opt-in).
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
// (RFC 8414). Use it when the app is an OAuth2/OpenID issuer so clients can
// discover endpoints + supported capabilities.
func WithOAuthAuthorizationServer(cfg OAuthAuthorizationServerConfig) AppOption {
	return func(a *App) { a.oauthAuthServer = &cfg }
}

func (a *App) handleOAuthAuthorizationServer(w http.ResponseWriter, _ *http.Request) {
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
	writeWellKnownJSON(w, doc)
}
