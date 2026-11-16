package framework

import (
	"context"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/webbotauth"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// WebBotAuthConfig configures Web Bot Auth: the published signing keys and,
// when Verify is set, experimental verification of inbound signatures.
type WebBotAuthConfig struct {
	// Keys is the JWK Set "keys" array, the site's public signing keys.
	Keys []map[string]any

	// Verify turns on inbound verification; nil verifies nothing and adds no
	// middleware.
	Verify *WebBotAuthVerifyConfig
}

// WebBotAuthVerifyConfig selects the inbound verification posture.
type WebBotAuthVerifyConfig struct {
	// Require answers unverified traffic 403; false, the default, only
	// annotates verified requests and logs failures.
	Require bool

	// MaxValidity caps a signature's expires minus created; zero means
	// webbotauth.DefaultMaxValidity, and a negative value panics.
	MaxValidity time.Duration
}

// WithWebBotAuth serves /.well-known/http-message-signatures-directory with
// the site's signing JWKS, and (when cfg.Verify is set) verifies inbound
// Web Bot Auth signatures.
func WithWebBotAuth(cfg WebBotAuthConfig) AppOption {
	return func(a *App) { a.webBotAuth = &cfg }
}

func (a *App) handleWebBotAuthDirectory(w http.ResponseWriter, r *http.Request) {
	if a.webBotAuth == nil {
		http.NotFound(w, nil)
		return
	}
	keys := a.webBotAuth.Keys
	if keys == nil {
		keys = []map[string]any{}
	}
	a.writeWellKnownJSON(w, r, map[string]any{"keys": keys})
}

// VerifiedAgent returns the agent a verified Web Bot Auth signature on the
// request identified, or nil for unverified traffic.
func VerifiedAgent(ctx context.Context) *webbotauth.Agent {
	return webbotauth.AgentFromContext(ctx)
}

// AgentRateLimitKey returns a ratelimit.MiddlewareByKey key function: a
// verified agent's identity, else the client IP (X-Forwarded-For only when
// trustXFF). Install it after WithWebBotAuth, behind an IP-keyed limiter.
func AgentRateLimitKey(trustXFF bool) func(*http.Request) string {
	return webbotauth.RateLimitKey(func(r *http.Request) string {
		return ratelimit.IPKey(ratelimit.ClientIP(r, trustXFF))
	})
}

// UCPConfig configures /.well-known/ucp (ucp.dev).
type UCPConfig struct {
	ProtocolVersion string
	Services        []map[string]any
	Capabilities    []map[string]any
	Endpoints       []map[string]any
	SpecURLs        []string
}

// WithUCP serves /.well-known/ucp with the site's UCP discovery metadata.
func WithUCP(cfg UCPConfig) AppOption {
	return func(a *App) { a.ucp = &cfg }
}

func (a *App) handleUCP(w http.ResponseWriter, r *http.Request) {
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
	a.writeWellKnownJSON(w, r, doc)
}

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
		apiBase = a.resolveWellKnownBase(r)
	}
	transports := cfg.Transports
	if transports == nil {
		transports = []string{}
	}
	services := cfg.CapabilitiesServices
	if services == nil {
		services = []map[string]any{}
	}
	a.writeWellKnownJSON(w, r, map[string]any{
		"protocol":     map[string]string{"name": "acp", "version": cfg.ProtocolVersion},
		"api_base_url": apiBase,
		"transports":   transports,
		"capabilities": map[string]any{"services": services},
	})
}
