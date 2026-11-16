package framework

import (
	"net/http"
)

// AuthMDConfig configures /auth.md and the agent_auth discovery block.
type AuthMDConfig struct {
	// Markdown is the /auth.md body, the manifest agents read to authenticate.
	Markdown string

	// AgentAuth, when set, is added as an agent_auth block to the
	// WithOAuthAuthorizationServer document.
	AgentAuth *AgentAuthBlock
}

// AgentAuthBlock is the agent_auth object in the OAuth authorization-server
// metadata (the WorkOS agentic-registration profile).
type AgentAuthBlock struct {
	// Skill is the /auth.md URL; empty means <origin>/auth.md.
	Skill            string
	IdentityEndpoint string
	ClaimEndpoint    string
	EventsEndpoint   string
	// IdentityTypesSupported defaults to anonymous, identity_assertion and
	// service_auth.
	IdentityTypesSupported []string
}

// WithAuthMD serves /auth.md and, when AgentAuth is set, adds an agent_auth
// block to the WithOAuthAuthorizationServer document.
func WithAuthMD(cfg AuthMDConfig) AppOption {
	return func(a *App) { a.authMD = &cfg }
}

func (a *App) handleAuthMD(w http.ResponseWriter, r *http.Request) {
	if a.authMD == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(a.authMD.Markdown))
}

func (a *App) agentAuthBlock(r *http.Request) map[string]any {
	if a.authMD == nil || a.authMD.AgentAuth == nil {
		return nil
	}
	b := a.authMD.AgentAuth
	skill := b.Skill
	if skill == "" {
		skill = a.resolveWellKnownBase(r) + "/auth.md"
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
