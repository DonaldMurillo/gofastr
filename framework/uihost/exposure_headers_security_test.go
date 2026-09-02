package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestUIHost_PageLLMIndexDisabledByDefault(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/llm-pages.md", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-llm] /llm-pages.md returned %d and exposed %q. Attack: route inventory is public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_PageLLMScreenDocDisabledByDefault(t *testing.T) {
	ds := newTestUIHostWithMultipleRoutes()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/about/llm.md", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-llm] per-screen llm.md returned %d and exposed %q. Attack: page-specific docs are public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_CreateSessionGETRejected(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/session", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("SECURITY: [uihost-session] GET /__gofastr/session returned %d. Attack: session minting is exposed to GET/CSRF/caching semantics.", rec.Code)
	}
}

func TestUIHost_NotFoundCarriesFrameDenyHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing X-Frame-Options DENY: %#v", rec.Header())
	}
}

func TestUIHost_NotFoundCarriesContentSecurityPolicy(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing Content-Security-Policy header: %#v", rec.Header())
	}
}

func TestUIHost_NotFoundCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_PageResponsesCarryNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-headers] page response missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_PageResponsesCarryReferrerPolicy(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("SECURITY: [uihost-headers] page response missing Referrer-Policy header: %#v", rec.Header())
	}
}

func TestUIHost_RuntimeJSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/runtime.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] runtime.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_ColorSchemeJSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/color-scheme.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] color-scheme.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_AppCSSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/app.css", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] app.css missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_ActionsJSCarriesNoSniffHeader(t *testing.T) {
	application := app.NewApp("actions-nosniff")
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] actions.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_WidgetCatalogRequiresAuth(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-widgets] widget catalog returned %d and exposed %q. Attack: infrastructure/widget inventory is public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_RuntimeModuleCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/runtime/widgets.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] split runtime module missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

// newAgentLinkHost builds a host whose agent-discovery surfaces (Link
// headers + agent card) are on, without pinning a BaseURL, so
// resolveBaseURL derives the origin per request.
func newAgentLinkHost(opts ...Option) *UIHost {
	application := app.NewApp("AgentLinkSec")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	base := []Option{WithAgentReady(AgentReadyConfig{
		AgentCard: &AgentCardConfig{Name: "X", MCPEndpoint: "/mcp"},
	})}
	return New(application, append(base, opts...)...)
}

// TestDiscoveryURLsIgnoreForwardedProto pins the trust boundary the docs
// state and pluginhost already enforces elsewhere in this repo:
// X-Forwarded-Proto is honored "for the scheme" (agent-ready.md), and
// framework/pluginhost/assets.go splices ONLY an exact "http"/"https"
// into output "because the raw value is request-controlled". Here,
// resolveBaseURL concatenates the raw header value into
// `scheme + "://" + r.Host` with no enum check, and the result is
// REFLECTED (not just used for a self-affecting boolean): into the Link
// response header on cacheable pages and the agent card's service URL.
// A caller sending a non-scheme value turns both into attacker-named
// origins — the cache-poisoning primitive the same function's comment
// refuses X-Forwarded-Host for.
//
// Not asserting against the documented proxy shape: an exact "https"
// from a trusted proxy must keep working (control 1 covers the derived
// origin staying the request's own).
//
// Surfaces: the page Link header (agent-card + service links) and the
// agent card's supportedInterfaces[].url on both the canonical
// /.well-known/agent-card.json and the legacy /.well-known/agent.json.
func TestDiscoveryURLsIgnoreForwardedProto(t *testing.T) {
	poisons := []string{
		"https://evil.example/p", // full attacker origin: base becomes evil.example/p://real
		"javascript:x",           // arbitrary scheme reflected into Link/card URLs
	}
	for _, poison := range poisons {
		t.Run(poison, func(t *testing.T) {
			host := newAgentLinkHost()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = "real.example"
			req.Header.Set("X-Forwarded-Proto", poison)
			rec := httptest.NewRecorder()
			host.ServeHTTP(rec, req)
			link := rec.Header().Get("Link")
			if strings.Contains(link, "evil.example") || strings.Contains(link, "javascript:") {
				t.Errorf("SECURITY: [uihost-agentlink] X-Forwarded-Proto %q was reflected into Link=%q. "+
					"Attack: cache-poisoned discovery URLs / agent-card pointing at attacker origins "+
					"(resolveBaseURL refuses X-Forwarded-Host for exactly this reflection but copies "+
					"X-Forwarded-Proto verbatim).", poison, link)
			}
			for _, cardPath := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
				req := httptest.NewRequest(http.MethodGet, cardPath, nil)
				req.Host = "real.example"
				req.Header.Set("X-Forwarded-Proto", poison)
				rec := httptest.NewRecorder()
				host.ServeHTTP(rec, req)
				if body := rec.Body.String(); strings.Contains(body, "evil.example") || strings.Contains(body, "javascript:") {
					t.Errorf("SECURITY: [uihost-agentcard] %s reflected X-Forwarded-Proto %q into the card: %s",
						cardPath, poison, body)
				}
			}
		})
	}

	// Control 1: without the header the derived origin is the request's own.
	host := newAgentLinkHost()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "real.example"
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, req)
	if link := rec.Header().Get("Link"); !strings.Contains(link, "http://real.example/") {
		t.Fatalf("control: clean request should advertise its own origin, got Link=%q", link)
	}

	// Control 2: a pinned BaseURL overrides request-derived values entirely.
	pinned := newAgentLinkHost(WithAgentReady(AgentReadyConfig{BaseURL: "https://pinned.example"}))
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Host = "real.example"
	req2.Header.Set("X-Forwarded-Proto", "https://evil.example/p")
	rec2 := httptest.NewRecorder()
	pinned.ServeHTTP(rec2, req2)
	if link := rec2.Header().Get("Link"); strings.Contains(link, "evil.example") || strings.Contains(link, "real.example") {
		t.Fatalf("control: pinned BaseURL must win over request headers, got Link=%q", link)
	}
}

// TestLinkAlternatePathControlBytes pins the C0 sanitation property on
// the one Link-header value built from the request PATH: markdownAlternate
// concatenates the percent-DECODED r.URL.Path into the rel="alternate"
// URL. A path segment of %0d%0a / %00 arrives decoded, so raw CR, LF and
// NUL land in the header value. net/http collapses CR/LF at write time,
// but NUL is not among the bytes it rewrites, and the raw value is what
// every other consumer of w.Header() (test recorders, middleware that
// copies headers, proxies that re-emit them) sees.
//
// Surface: the Link header on a rendered dynamic-route page with
// WithPublicLLMMD (the only emitter that concatenates the request path).
func TestLinkAlternatePathControlBytes(t *testing.T) {
	application := app.NewApp("LinkPathSec")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/notes/{slug}", &plainComp{}).WithTitle("Notes"), nil)
	host := New(application,
		WithPublicLLMMD(),
		WithAgentReady(AgentReadyConfig{AgentCard: &AgentCardConfig{Name: "X", MCPEndpoint: "/mcp"}}))

	for _, tc := range []struct{ name, raw, bad string }{
		{"crlf", "/notes/a%0d%0aX-Injected:%20yes", "\r"},
		{"nul", "/notes/a%00b", "\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.raw, nil)
			req.Host = "real.example"
			rec := httptest.NewRecorder()
			host.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("setup: page must render for the Link header to be written, got %d", rec.Code)
			}
			link := rec.Header().Get("Link")
			if i := strings.Index(link, tc.bad); i >= 0 {
				t.Fatalf("SECURITY: [uihost-link] decoded request path carried raw %q into the Link header "+
					"at offset %d: %q. Attack: control bytes from %s in the URL reach an outbound header "+
					"value unsanitized (markdownAlternate concatenates r.URL.Path with no C0 strip).",
					tc.bad, i, link, tc.raw)
			}
			// No other C0 byte either (tab is legal in field values).
			for i := range len(link) {
				if link[i] < 0x20 && link[i] != '\t' {
					t.Fatalf("SECURITY: [uihost-link] control byte 0x%02x in Link=%q", link[i], link)
				}
			}
		})
	}

	// Control: a clean path yields a clean header.
	req := httptest.NewRequest(http.MethodGet, "/notes/clean", nil)
	req.Host = "real.example"
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, req)
	if link := rec.Header().Get("Link"); !strings.Contains(link, "/notes/clean/llm.md") {
		t.Fatalf("control: clean path should still advertise its markdown alternate, got Link=%q", link)
	}
}
