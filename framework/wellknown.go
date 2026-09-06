package framework

// wellknown.go: the agent-readiness well-known endpoints that the
// isitagentready scanner scores. Each is auto-served when its precondition
// holds, so a host that wires the basics scores without per-route work:
//
//   - /.well-known/api-catalog                 (RFC 9727 linkset+json): when the app has an API (entities)
//   - /.well-known/mcp/server-card.json:         when WithMCP exposes /mcp
//     (plus the /mcp/server-card alias and /.well-known/mcp/catalog.json,
//     both also gated on WithMCP)
//   - /.well-known/agent-skills/index.json:      opt-in (host declares skills)
//   - /.well-known/oauth-authorization-server  (RFC 8414): opt-in (host is an OAuth issuer)
//
// The scanner only requires a 200 at each path; we emit real, spec-shaped
// bodies so the artifacts are useful, not just present.

import (
	"encoding/json"
	"net/http"
	"strings"
)

// resolveWellKnownBase returns the canonical origin for absolute URLs in
// well-known docs. These docs live at the framework layer, which has no
// sitemap base to fall back on, so the request is the source of truth.
//
// X-Forwarded-Host is NOT honored. It is a plain request header, so any
// client sets it, and the value ends up in the `Link: rel="service"`
// header naming the MCP endpoint. Reflecting an arbitrary origin there,
// with no Vary, is a cache-poisoning primitive that redirects a later
// visitor's agent at an attacker's server. That is the same reasoning
// battery/print already documents for its BaseURL ("deliberately NOT
// derived from the request Host header, which is client-controlled").
//
// r.Host is used instead: still client-supplied, but it is the authority
// the request was actually addressed to, and varyWellKnown below keeps
// caches from serving one caller's value to another.
//
// X-Forwarded-Proto IS honored, a TLS-terminating proxy is the normal
// deployment — but only as an exact "http"/"https". The raw value is
// request-controlled and this origin is reflected into the well-known
// document's absolute URLs, so a forged "https://evil.example/p" would
// paint an attacker-named origin into cacheable output, and "https,http"
// (a two-hop proxy chain) is not a scheme at all. Vary narrows who
// receives a poisoned entry; it does not clean the value. Same enum as
// framework/uihost/agentready.go and framework/pluginhost/assets.go.
func resolveWellKnownBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if u := r.Header.Get("X-Forwarded-Proto"); u == "http" || u == "https" {
		scheme = u
	}
	return strings.TrimRight(scheme+"://"+r.Host, "/")
}

// varyWellKnown declares the request inputs the response body depends
// on, so a shared cache keys on them instead of serving the first
// caller's absolute URLs to everyone behind it.
func varyWellKnown(w http.ResponseWriter) {
	w.Header().Add("Vary", "Host")
	w.Header().Add("Vary", "X-Forwarded-Proto")
}

func writeWellKnownJSON(w http.ResponseWriter, doc any) {
	varyWellKnown(w)
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
	// Same Vary discipline as every sibling well-known document: the
	// card body embeds remotes[].url built from r.Host and
	// X-Forwarded-Proto, so a shared cache must key on those inputs.
	varyWellKnown(w)
	w.Header().Set("Content-Type", "application/mcp-server-card+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(a.buildMCPServerCard(r))
}

// handleMCPCatalog serves /.well-known/mcp/catalog.json, the spec-
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
	// Default onto a per-request copy, never into a.agentSkills: the
	// slice WithAgentSkills installed is wiring-time configuration
	// shared by every request, and writing it here is an unsynchronized
	// mutation of process-global state (a data race under -race and a
	// leak of one request's normalization into every other's).
	skills := make([]AgentSkillEntry, len(a.agentSkills))
	copy(skills, a.agentSkills)
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
	// WorkOS agentic-registration profile: merge agent_auth when WithAuthMD
	// configured an AgentAuth block.
	if ab := a.agentAuthBlock(r); ab != nil {
		doc["agent_auth"] = ab
	}
	writeWellKnownJSON(w, doc)
}
