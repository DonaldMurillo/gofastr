package framework

// agent_extras.go — the remaining isitagentready.com production-scanner
// checks that ARE framework-buildable as served routes (each opt-in; the
// host provides the real data, the framework serves the discovery doc):
//
//   - /.well-known/http-message-signatures-directory  (Web Bot Auth — the
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
	"net/http"
)

// ── Web Bot Auth (publish a JWKS for outbound request signing) ─────

// WebBotAuthConfig configures /.well-known/http-message-signatures-directory.
// The site publishes its signing keys (a JWK Set) so receivers can verify
// the requests it sends as a bot/agent. (This is the publishing side, not
// RFC 9421 inbound verification.)
type WebBotAuthConfig struct {
	// Keys is the JWK Set "keys" array — the site's public signing keys.
	Keys []map[string]any
}

// WithWebBotAuth serves /.well-known/http-message-signatures-directory with
// the site's signing JWKS.
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
