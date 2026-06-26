package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
