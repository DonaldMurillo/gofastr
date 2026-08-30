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
		JWTSecret:           "test-secret",
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		AllowInMemoryStores: true, // unit tests run on the memory session store
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
	req := oauthCallbackReq("/auth/oauth/stub/callback?state="+state+"&code=fakecode", state)
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
// succeeds, this is the recovery path the docs promise.
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
// user A cannot be completed under user B's session, even a valid, signed
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
// cookie is rejected (401), the flow is authenticated by construction.
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

// TestOAuthLinkRefusesBoundIdentity pins the link-theft guard on the
// AUTHENTICATED /link callback: a logged-in user completing a link flow
// for a provider identity that is already bound to a DIFFERENT local user
// gets 409, the binding is not reassigned, and the refusal is audited
// (oauth.refused / provider_already_linked). The DB layer's immutability
// is pinned in entity_oauth_links_test.go; this is the HTTP branch.
func TestOAuthLinkRefusesBoundIdentity(t *testing.T) {
	store := newLinkingUserStore()
	rec := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		AllowInMemoryStores: true,
		AuditSink:           rec,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{"stub": &stubOAuthProvider{
			name:     "stub",
			userInfo: &OAuth2UserInfo{ID: "prov-taken", Email: "mallory@example.com", EmailVerified: true},
		}},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// The provider identity is already durably bound to the victim.
	victim := store.preExistingUser("victim@example.com")
	if err := store.LinkOAuth(context.Background(), victim.GetID(), "stub", "prov-taken"); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	linksBefore := store.linkCalls

	// Mallory has her own valid session and completes a link flow whose
	// OAuth round-trip returns the victim's provider identity.
	mallory := store.preExistingUser("mallory@example.com")
	sess, err := mgr.SessionStore().Create(context.Background(), mallory.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}
	w := linkCallbackReq(t, plugin, r, mallory.GetID(), sess.Token)

	if w.Code != http.StatusConflict {
		t.Fatalf("SECURITY: [oauth-link-theft] completing /link for an identity bound to another user returned "+
			"%d (body=%s), want 409", w.Code, w.Body.String())
	}
	if store.linkCalls != linksBefore {
		t.Fatalf("SECURITY: [oauth-link-theft] the callback re-bound the identity; LinkOAuth calls went %d → %d",
			linksBefore, store.linkCalls)
	}
	owner, err := store.FindByOAuth(context.Background(), "stub", "prov-taken")
	if err != nil || owner.GetID() != victim.GetID() {
		t.Fatalf("SECURITY: [oauth-link-theft] victim's binding disturbed: owner=%v err=%v", owner, err)
	}
	ev := rec.findByKind("oauth.refused")
	if ev == nil {
		t.Fatalf("SECURITY: [oauth-link-theft] refusal not audited; kinds seen: %v", rec.kinds())
	}
	if ev.Meta["reason"] != "provider_already_linked" {
		t.Fatalf("audit reason = %q, want provider_already_linked", ev.Meta["reason"])
	}
}
