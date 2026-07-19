package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestOAuth2State_RoundTripsBoundUserID: the HMAC-signed state carries the
// authenticated user id for the link flow (empty for ordinary login), and it
// survives a validate round-trip untouched. A forged/altered userID would
// break the HMAC (covered by the tamper test in oauth2_test.go).
func TestOAuth2State_RoundTripsBoundUserID(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"mock": &stubOAuthProvider{name: "mock"}},
		StateSecret: "k",
	})

	loginState, err := p.generateState("mock", "")
	if err != nil {
		t.Fatalf("generateState(login): %v", err)
	}
	if uid, ok := p.validateAndConsumeState(loginState, "mock"); !ok || uid != "" {
		t.Fatalf("login state: uid=%q ok=%v, want \"\" true", uid, ok)
	}

	linkState, err := p.generateState("mock", "user-77")
	if err != nil {
		t.Fatalf("generateState(link): %v", err)
	}
	if uid, ok := p.validateAndConsumeState(linkState, "mock"); !ok || uid != "user-77" {
		t.Fatalf("link state: uid=%q ok=%v, want \"user-77\" true", uid, ok)
	}
}

// linkFixture wires a manager + oauth2 plugin + linking store the way the
// authenticated-link tests need, and returns the plugin, router, and store.
func linkFixture(t *testing.T, info *OAuth2UserInfo) (*OAuth2Plugin, *router.Router, *linkingUserStore, *AuthManager) {
	t.Helper()
	store := newLinkingUserStore()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"stub": &stubOAuthProvider{name: "stub", userInfo: info}},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return plugin, r, store, mgr
}

func linkCallbackReq(t *testing.T, plugin *OAuth2Plugin, r *router.Router, linkUserID, sessionToken string) *httptest.ResponseRecorder {
	t.Helper()
	state, err := plugin.generateState("stub", linkUserID)
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth/stub/callback?state="+state+"&code=fakecode", nil)
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionToken})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestOAuth_AuthenticatedLinkBindsProviderToProvenUser: the logged-in owner of
// a PASSWORD account (which the unauthenticated callback would 409) links a
// provider that returns the same email. Because the user proved ownership of
// both the account (session) and the provider (OAuth round-trip), the link
// succeeds — this is the recovery path the docs promise.
func TestOAuth_AuthenticatedLinkBindsProviderToProvenUser(t *testing.T) {
	plugin, r, store, mgr := linkFixture(t, &OAuth2UserInfo{
		ID: "prov-id-1", Email: "owner@example.com", Provider: "stub", EmailVerified: true,
	})
	owner := store.preExistingUser("owner@example.com") // has a password

	sess, err := mgr.SessionStore().Create(context.Background(), owner.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	w := linkCallbackReq(t, plugin, r, owner.GetID(), sess.Token)

	if w.Code != http.StatusFound {
		t.Fatalf("authenticated link should succeed (302), got %d (body=%s)", w.Code, w.Body.String())
	}
	if store.linkCalls != 1 {
		t.Fatalf("expected exactly 1 link call, got %d", store.linkCalls)
	}
}

// TestOAuth_AuthenticatedLinkRefusesSessionMismatch: a link-state bound to
// user A cannot be completed under user B's session — even a valid, signed
// link-state is rejected (403) when it doesn't match the current login, and
// nothing is linked.
func TestOAuth_AuthenticatedLinkRefusesSessionMismatch(t *testing.T) {
	plugin, r, store, mgr := linkFixture(t, &OAuth2UserInfo{
		ID: "prov-id-2", Email: "a@example.com", Provider: "stub", EmailVerified: true,
	})
	userA := store.preExistingUser("a@example.com")
	userB := store.preExistingUser("b@example.com")

	// Session belongs to B, but the link-state names A.
	sessB, err := mgr.SessionStore().Create(context.Background(), userB.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	w := linkCallbackReq(t, plugin, r, userA.GetID(), sessB.Token)

	if w.Code != http.StatusForbidden {
		t.Fatalf("link under a mismatched session must be 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	if store.linkCalls != 0 {
		t.Fatalf("must not link when the session does not match the link request; got %d", store.linkCalls)
	}
}

// TestOAuth_AuthenticatedLinkRequiresSession: a link-state without any session
// cookie is rejected (401) — the flow is authenticated by construction.
func TestOAuth_AuthenticatedLinkRequiresSession(t *testing.T) {
	plugin, r, store, _ := linkFixture(t, &OAuth2UserInfo{
		ID: "prov-id-3", Email: "c@example.com", Provider: "stub", EmailVerified: true,
	})
	owner := store.preExistingUser("c@example.com")

	w := linkCallbackReq(t, plugin, r, owner.GetID(), "") // no session cookie

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("link without a session must be 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if store.linkCalls != 0 {
		t.Fatalf("must not link without a session; got %d", store.linkCalls)
	}
}
