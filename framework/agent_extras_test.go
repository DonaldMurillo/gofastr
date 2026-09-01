package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/webbotauth"
)

func TestWebBotAuth_ServesJWKS(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys: []map[string]any{{"kty": "OKP", "kid": "bot-1", "use": "sig", "alg": "EdDSA"}},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/http-message-signatures-directory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "\"keys\"") || !strings.Contains(body, "bot-1") {
		t.Errorf("JWKS missing keys: %s", body)
	}
}

func TestWebBotAuth_NotConfigured404(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/http-message-signatures-directory", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestUCP_ServesDiscovery(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithUCP(UCPConfig{
		ProtocolVersion: "0.1",
		Services:        []map[string]any{{"name": "checkout"}},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/ucp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"\"protocolVersion\"", "0.1", "checkout"} {
		if !strings.Contains(body, want) {
			t.Errorf("ucp doc missing %q: %s", want, body)
		}
	}
}

func TestACP_ServesDiscovery(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithACP(ACPConfig{
		ProtocolVersion:      "0.1",
		APIBaseURL:           "https://shop.test/api",
		Transports:           []string{"https"},
		CapabilitiesServices: []map[string]any{{"name": "catalog"}},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acp.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"\"protocol\"", "\"acp\"", "https://shop.test/api", "\"services\"", "catalog"} {
		if !strings.Contains(body, want) {
			t.Errorf("acp doc missing %q: %s", want, body)
		}
	}
}

// AgentRateLimitKey: a verified agent is its own bucket, an unverified
// caller is its IP, and the verifier installed by WithWebBotAuth runs
// before a limiter the app adds afterwards, so the key sees the agent.
func TestAgentRateLimitKeySplitsAgentFromIP(t *testing.T) {
	key := AgentRateLimitKey(false)
	r := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
	r.RemoteAddr = "198.51.100.7:9"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := key(r); got != "198.51.100.7" {
		t.Fatalf("unverified key = %q, want the socket IP (XFF untrusted)", got)
	}
	verified := r.WithContext(webbotauth.WithAgent(r.Context(), &webbotauth.Agent{URL: "https://crawler.example/.well-known/http-message-signatures-directory"}))
	if got := key(verified); got != "agent:https://crawler.example/.well-known/http-message-signatures-directory" {
		t.Fatalf("verified key = %q, want the agent identity", got)
	}
	if got := AgentRateLimitKey(true)(r); got != "10.0.0.1" {
		t.Fatalf("trustXFF key = %q, want the forwarded IP", got)
	}
}
