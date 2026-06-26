package framework

// authmd.go — /auth.md + the agent_auth block (WorkOS "agentic registration"
// profile: workos/auth.md). This is one of isitagentready.com's production
// scanner checks (authMd). It layers on the OAuth discovery docs:
//
//   - GET /auth.md                          — a Markdown procedural manifest
//                                            agents read to authenticate.
//   - an `agent_auth` block inside          — points agents at the skill
//     /.well-known/oauth-authorization-server   (/auth.md) + the identity/
//                                            claim/events endpoints.
//
// Opt-in via WithAuthMD. The Markdown body and the agent-auth endpoints are
// host-authored (the host implements the registration endpoints); the
// framework only serves the discovery surface.

import (
	"net/http"
)

// AuthMDConfig configures /auth.md and the agent_auth discovery block.
type AuthMDConfig struct {
	// Markdown is the /auth.md body — the procedural manifest agents read
	// (discover → register → claim → exchange → use → revoke). Host-authored.
	Markdown string

	// AgentAuth, when set, is merged as an `agent_auth` block into the
	// /.well-known/oauth-authorization-server document (requires
	// WithOAuthAuthorizationServer). Nil omits the block.
	AgentAuth *AgentAuthBlock
}

// AgentAuthBlock is the agent_auth object advertised in the OAuth
// authorization-server metadata (WorkOS agentic-registration profile).
type AgentAuthBlock struct {
	// Skill is the URL of the /auth.md manifest. Defaults to <base>/auth.md.
	Skill string
	// IdentityEndpoint registers/looks up an agent identity.
	IdentityEndpoint string
	// ClaimEndpoint claims a pending identity.
	ClaimEndpoint string
	// EventsEndpoint receives async notifications (e.g. revocation).
	EventsEndpoint string
	// IdentityTypesSupported defaults to ["anonymous","identity_assertion","service_auth"].
	IdentityTypesSupported []string
}

// WithAuthMD serves /auth.md and (when AgentAuth is set) merges an agent_auth
// block into the OAuth authorization-server metadata. Pair with
// WithOAuthAuthorizationServer for the agent_auth block to be emitted.
func WithAuthMD(cfg AuthMDConfig) AppOption {
	return func(a *App) { a.authMD = &cfg }
}

// handleAuthMD serves the /auth.md manifest as text/markdown.
func (a *App) handleAuthMD(w http.ResponseWriter, r *http.Request) {
	if a.authMD == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(a.authMD.Markdown))
}

// agentAuthBlock builds the agent_auth object for the OAuth AS metadata,
// resolving the Skill default against the request origin. Returns nil when
// no AgentAuth is configured.
func (a *App) agentAuthBlock(r *http.Request) map[string]any {
	if a.authMD == nil || a.authMD.AgentAuth == nil {
		return nil
	}
	b := a.authMD.AgentAuth
	skill := b.Skill
	if skill == "" {
		skill = resolveWellKnownBase(r) + "/auth.md"
	}
	types := b.IdentityTypesSupported
	if len(types) == 0 {
		types = []string{"anonymous", "identity_assertion", "service_auth"}
	}
	doc := map[string]any{
		"skill":                    skill,
		"identity_types_supported": types,
		"identity_assertion": map[string]any{
			"assertion_types_supported": []string{"urn:ietf:params:oauth:token-type:id-jag"},
		},
	}
	set := func(k, v string) {
		if v != "" {
			doc[k] = v
		}
	}
	set("identity_endpoint", b.IdentityEndpoint)
	set("claim_endpoint", b.ClaimEndpoint)
	set("events_endpoint", b.EventsEndpoint)
	return doc
}
