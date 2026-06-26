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

// handleMCPServerCard mirrors the MCP initialize handshake as a GET-able
// server card: serverInfo, the /mcp endpoint, the transport, capabilities,
// and the tool names. Served when WithMCP mounts /mcp.
func (a *App) handleMCPServerCard(w http.ResponseWriter, r *http.Request) {
	base := resolveWellKnownBase(r)
	name, version := a.MCP.ServerInfo()
	toolNames := make([]string, 0, len(a.MCP.ListTools()))
	for _, t := range a.MCP.ListTools() {
		toolNames = append(toolNames, t.Name)
	}
	writeWellKnownJSON(w, map[string]any{
		"serverInfo":   map[string]string{"name": name, "version": version},
		"endpoint":     base + "/mcp",
		"transport":    "streamable-http",
		"capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}},
		"tools":        toolNames,
	})
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
