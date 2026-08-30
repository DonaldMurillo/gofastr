package framework

// webbotauth_wiring_test.go: the WithWebBotAuth option's two
// directions and the proof that the publishing side is byte-identical
// whether or not the verification fields are set.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pinnedJWKSBody = "{\n  \"keys\": [\n    {\n      \"kid\": \"bot-1\",\n      \"kty\": \"OKP\",\n      \"use\": \"sig\"\n    }\n  ]\n}"

// TestWebBotAuth_PublishingBytesPinnedWhenVerifyUnset pins the exact
// bytes a Keys-only caller gets from /.well-known/http-message-
// signatures-directory. The verification fields must not change this:
// an existing publisher opts into verification and its directory
// document stays identical. (Mutation proof: change
// handleWebBotAuthDirectory — drop the nil-Keys normalization, add a
// field, alter the content type — and this test fails.)
func TestWebBotAuth_PublishingBytesPinnedWhenVerifyUnset(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys: []map[string]any{{"kty": "OKP", "kid": "bot-1", "use": "sig"}},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/http-message-signatures-directory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimRight(rec.Body.String(), "\n"); got != pinnedJWKSBody {
		t.Errorf("publishing bytes drifted:\n got: %s\nwant: %s", got, pinnedJWKSBody)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// TestWebBotAuth_PublishingBytesIdenticalWithVerifySet: enabling
// verification does not touch the published document.
func TestWebBotAuth_PublishingBytesIdenticalWithVerifySet(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys:   []map[string]any{{"kty": "OKP", "kid": "bot-1", "use": "sig"}},
		Verify: &WebBotAuthVerifyConfig{},
	})))
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/http-message-signatures-directory", nil))
	if got := strings.TrimRight(rec.Body.String(), "\n"); got != pinnedJWKSBody {
		t.Errorf("verify fields changed the published document:\n got: %s\nwant: %s", got, pinnedJWKSBody)
	}
}

// TestWebBotAuth_VerifyNilAddsNothing: without Verify, unsigned and
// even signature-bearing requests pass through untouched — no
// verification middleware exists on the chain.
func TestWebBotAuth_VerifyNilAddsNothing(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys: []map[string]any{{"kty": "OKP"}},
	})))
	defer cleanup()
	app.router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if VerifiedAgent(r.Context()) != nil {
			t.Error("VerifiedAgent non-nil without verification enabled")
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/probe", nil)
	r.Header.Set("Signature-Input", `sig1=("@authority");tag="web-bot-auth"`) // would fail every check
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("request with bogus signature blocked: %d", rec.Code)
	}
}

// TestWebBotAuth_ObserveModePassesThrough: with Verify set in observe
// mode, unsigned traffic is served; the verified path is exercised
// end-to-end in core/webbotauth (which can reach its TLS test
// directory).
func TestWebBotAuth_ObserveModePassesThrough(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys:   []map[string]any{{"kty": "OKP"}},
		Verify: &WebBotAuthVerifyConfig{},
	})))
	defer cleanup()
	app.router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if VerifiedAgent(r.Context()) != nil {
			t.Error("unsigned request annotated as verified")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("observe mode blocked unsigned traffic: %d", rec.Code)
	}
}

// TestWebBotAuth_RequireModeBlocksUnsigned: the app-level wiring of
// the require posture through the real router.
func TestWebBotAuth_RequireModeBlocksUnsigned(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithWebBotAuth(WebBotAuthConfig{
		Keys:   []map[string]any{{"kty": "OKP"}},
		Verify: &WebBotAuthVerifyConfig{Require: true},
	})))
	defer cleanup()
	app.router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("require mode served unsigned traffic")
	}))

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("require mode returned %d", rec.Code)
	}
	if rec.Header().Get("Accept-Signature") == "" {
		t.Error("403 carries no Accept-Signature")
	}
}

func TestVerifiedAgent_PlainContext(t *testing.T) {
	if VerifiedAgent(context.Background()) != nil {
		t.Error("VerifiedAgent on a plain context must be nil")
	}
}
