package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMD_ServesMarkdown(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithAuthMD(AuthMDConfig{
		Markdown: "# Agent auth\n\nRegister at /agent/register.",
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type %q, want text/markdown", ct)
	}
	if !strings.Contains(rec.Body.String(), "# Agent auth") {
		t.Errorf("body mismatch: %q", rec.Body.String())
	}
}

func TestAuthMD_NotConfigured404(t *testing.T) {
	app, cleanup := startApp(t, NewApp())
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth.md", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 without WithAuthMD, got %d", rec.Code)
	}
}

func TestAuthMD_AgentAuthMergedIntoAS(t *testing.T) {
	app, cleanup := startApp(t, NewApp(
		WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{Issuer: "https://ex.test"}),
		WithAuthMD(AuthMDConfig{
			Markdown: "# auth",
			AgentAuth: &AgentAuthBlock{
				IdentityEndpoint: "https://ex.test/agent/identity",
				ClaimEndpoint:    "https://ex.test/agent/identity/claim",
			},
		}),
	))
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	req.Host = "ex.test"
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	// agent_auth block present with the configured endpoints + defaults.
	for _, want := range []string{
		"\"agent_auth\"", "\"identity_endpoint\"", "https://ex.test/agent/identity",
		"\"identity_types_supported\"", "anonymous", "\"skill\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AS doc missing %q:\n%s", want, body)
		}
	}
}
