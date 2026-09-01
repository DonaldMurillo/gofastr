package framework

// agent_extras.go: the remaining isitagentready.com production-scanner
// checks that ARE framework-buildable as served routes (each opt-in; the
// host provides the real data, the framework serves the discovery doc):
//
//   - /.well-known/http-message-signatures-directory  (Web Bot Auth, the
//     site PUBLISHES a JWKS so it can sign its outbound bot/agent requests)
//   - /.well-known/ucp                                 (Universal Commerce Protocol)
//   - /.well-known/acp.json                            (Agentic Commerce Protocol)
//
// NOT buildable as served routes (documented in agent-ready.md): dnsAid
// (DNS SVCB/HTTPS + DNSSEC), x402 (payment middleware returning HTTP 402),
// mpp (payment execution + an x-payment-info OpenAPI extension that needs a
// payment backend), webMcp (client-side browser API), ap2 (server-only,
// no public probe).

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/webbotauth"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// ── Web Bot Auth (publish a JWKS for outbound request signing) ─────

// WebBotAuthConfig configures Web Bot Auth in both directions.
//
// Publishing (Keys, unchanged since the option existed): the site
// serves its signing keys as a JWK Set at
// /.well-known/http-message-signatures-directory so receivers can
// verify the requests it sends as a bot/agent.
//
// Verification (Verify, experimental): inbound RFC 9421 signature
// verification under the profile of
// draft-meunier-webbotauth-httpsig-protocol-02 (18 August 2026). The
// draft is moving and this half of the option tracks it; the publishing
// half is stable. Leaving Verify nil keeps the publishing behaviour
// byte-identical and adds no middleware.
type WebBotAuthConfig struct {
	// Keys is the JWK Set "keys" array, the site's public signing keys.
	Keys []map[string]any

	// Verify turns on inbound verification of Web Bot Auth signatures.
	// Nil (the default) verifies nothing: requests pass through
	// untouched and only /.well-known/http-message-signatures-directory
	// is served. See WebBotAuthVerifyConfig for the modes.
	Verify *WebBotAuthVerifyConfig
}

// WebBotAuthVerifyConfig selects the inbound verification posture.
type WebBotAuthVerifyConfig struct {
	// Require blocks traffic that is not verified (403 with an
	// Accept-Signature response). The default, false, is observe mode:
	// verified requests carry the agent identity in their context
	// (see VerifiedAgent) and verification failures are logged, but
	// nothing is ever blocked. Observe is the default on purpose: a
	// bug in draft-tracking verification middleware must not be able
	// to take an app's traffic down.
	Require bool
}

// WithWebBotAuth serves /.well-known/http-message-signatures-directory with
// the site's signing JWKS, and (when cfg.Verify is set) verifies inbound
// Web Bot Auth signatures.
func WithWebBotAuth(cfg WebBotAuthConfig) AppOption {
	return func(a *App) { a.webBotAuth = &cfg }
}

func (a *App) handleWebBotAuthDirectory(w http.ResponseWriter, _ *http.Request) {
	if a.webBotAuth == nil {
		http.NotFound(w, nil)
		return
	}
	keys := a.webBotAuth.Keys
	if keys == nil {
		keys = []map[string]any{}
	}
	writeWellKnownJSON(w, map[string]any{"keys": keys})
}

// VerifiedAgent returns the identity established by a verified Web Bot
// Auth signature on the current request: the agent's resolved key
// directory URL (the protocol's identifier) and the key thumbprint
// that verified it. It returns nil for unverified traffic, which is
// the common case: browsers send no signature. Use it for annotation,
// differentiated rate limits, and policy, and treat nil as "learned
// nothing about the sender", never as evidence of hostility.
func VerifiedAgent(ctx context.Context) *webbotauth.Agent {
	return webbotauth.AgentFromContext(ctx)
}

// AgentRateLimitKey returns a key function for ratelimit.MiddlewareByKey
// that gives every verified Web Bot Auth agent its own budget and keys
// everything else by client IP, honouring X-Forwarded-For only when
// trustXFF is set (the same rule as ratelimit.ClientIP). Add the limiter
// with app.Router().Use after WithWebBotAuth so the verifier runs first:
//
//	limiter := ratelimit.NewLimiter(ratelimit.Config{MaxAttempts: 600, Window: time.Minute})
//	app.Router().Use(limiter.MiddlewareByKey(framework.AgentRateLimitKey(false)))
//
// See webbotauth.RateLimitKey for why identity, not IP, is the honest
// budget for agent traffic.
func AgentRateLimitKey(trustXFF bool) func(*http.Request) string {
	return webbotauth.RateLimitKey(func(r *http.Request) string {
		return ratelimit.ClientIP(r, trustXFF)
	})
}

// ── Universal Commerce Protocol (/.well-known/ucp) ─────────────────

// UCPConfig configures /.well-known/ucp (ucp.dev).
type UCPConfig struct {
	ProtocolVersion string
	Services        []map[string]any
	Capabilities    []map[string]any
	Endpoints       []map[string]any
	// SpecURLs are advertised spec/schema URLs (the scanner expects them reachable).
	SpecURLs []string
}

// WithUCP serves /.well-known/ucp with the site's UCP discovery metadata.
func WithUCP(cfg UCPConfig) AppOption {
	return func(a *App) { a.ucp = &cfg }
}

func (a *App) handleUCP(w http.ResponseWriter, _ *http.Request) {
	if a.ucp == nil {
		http.NotFound(w, nil)
		return
	}
	cfg := a.ucp
	doc := map[string]any{"protocolVersion": cfg.ProtocolVersion}
	if cfg.Services != nil {
		doc["services"] = cfg.Services
	} else {
		doc["services"] = []map[string]any{}
	}
	if cfg.Capabilities != nil {
		doc["capabilities"] = cfg.Capabilities
	}
	if cfg.Endpoints != nil {
		doc["endpoints"] = cfg.Endpoints
	}
	if len(cfg.SpecURLs) > 0 {
		doc["specs"] = cfg.SpecURLs
	}
	writeWellKnownJSON(w, doc)
}

// ── Agentic Commerce Protocol (/.well-known/acp.json) ──────────────

// ACPConfig configures /.well-known/acp.json (agenticcommerce.dev).
type ACPConfig struct {
	ProtocolVersion      string           // protocol.version (protocol.name is fixed "acp")
	APIBaseURL           string           // api_base_url
	Transports           []string         // supported transports
	CapabilitiesServices []map[string]any // capabilities.services
}

// WithACP serves /.well-known/acp.json with the site's ACP discovery metadata.
func WithACP(cfg ACPConfig) AppOption {
	return func(a *App) { a.acp = &cfg }
}

func (a *App) handleACP(w http.ResponseWriter, r *http.Request) {
	if a.acp == nil {
		http.NotFound(w, nil)
		return
	}
	cfg := a.acp
	apiBase := cfg.APIBaseURL
	if apiBase == "" {
		apiBase = resolveWellKnownBase(r)
	}
	transports := cfg.Transports
	if transports == nil {
		transports = []string{}
	}
	services := cfg.CapabilitiesServices
	if services == nil {
		services = []map[string]any{}
	}
	writeWellKnownJSON(w, map[string]any{
		"protocol":     map[string]string{"name": "acp", "version": cfg.ProtocolVersion},
		"api_base_url": apiBase,
		"transports":   transports,
		"capabilities": map[string]any{"services": services},
	})
}
