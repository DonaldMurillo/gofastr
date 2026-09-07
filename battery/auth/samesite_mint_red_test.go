//go:build red

package auth

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: every site that mints the session cookie emits SameSite=Strict.
// The login and logout mints are pinned by name
// (TestCorePlugin_LoginCookieUsesStrictSameSite /
// TestCorePlugin_LogoutCookieUsesStrictSameSite, core.go:300/:390); these
// are the two divergent surfaces. Both have minted Lax since their original
// commit with no decision comment — the only Lax-justifying comment in the
// file (oauth2.go:241-244) belongs to the STATE cookie gofastr_oauth_state,
// which genuinely needs Lax to ride the provider's top-level redirect back.
// Surfaces: battery/auth/magiclink.go:670-678 (verifyHandler session mint,
// http.SameSiteLaxMode), battery/auth/oauth2.go:560-568 (callbackHandler
// session mint, http.SameSiteLaxMode).
// Finding: sessions minted by magic-link verify and the OAuth callback ride
// any top-level cross-site GET navigation, re-opening on those sessions the
// CSRF exposure the Strict login mint closed — a divergence the battery
// already prices in (twofa.go:370-373, twofa_security_test.go:115-119).
// Fix direction: both mints set http.SameSiteStrictMode, matching the
// password-login mint. Neither flow needs the session cookie to return
// cross-site: the user lands on the callback same-site (the provider
// redirects top-level, but the SESSION cookie is minted by that same-site
// response and is only needed afterwards).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestMagicLinkRedCookieStrict: a same-origin form POST redeeming a valid
// magic link mints the session cookie; that mint must carry
// SameSite=Strict like the password-login mint it parallels.
func TestMagicLinkRedCookieStrict(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatalf("setup broken: createPurposeToken: %v", err)
	}

	req := magicConfirmReq(token) // same-origin form POST, no cross-site markers
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := mintedSession(rec); got == "" {
		t.Fatalf("setup broken: same-origin verify minted no session (status %d, body %q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Errorf("SECURITY: [session-cookie-samesite-mint] magic-link verify minted the session cookie as %q, "+
			"want SameSite=Strict — password login's mint is Strict (TestCorePlugin_LoginCookieUsesStrictSameSite); "+
			"a Lax session rides any top-level cross-site GET and re-opens the CSRF exposure Strict closed",
			rec.Header().Get("Set-Cookie"))
	}
}

// TestOauthCallbackRedCookieStrict: the provider callback completing a
// legitimate same-browser flow mints the session cookie; that mint must
// carry SameSite=Strict too. The state cookie may stay Lax (it must ride
// the provider's redirect); the session cookie has no such return trip.
func TestOauthCallbackRedCookieStrict(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{ID: "u-1", Email: "alice@example.com"},
	})
	r := mountOAuth2Routes(mgr)

	// Start the flow in "this browser": the redirect plants the state
	// cookie the callback requires (browser binding).
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, redirectReq)
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("setup broken: parse redirect: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("setup broken: no state in the provider redirect")
	}

	cb := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+url.QueryEscape(state)+"&code=abc", nil)
	for _, c := range w.Result().Cookies() {
		cb.AddCookie(c)
	}
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, cb)

	if cw.Code != http.StatusFound {
		t.Fatalf("setup broken: callback status %d: %s", cw.Code, cw.Body.String())
	}
	// The response clears the state cookie AND mints the session; only the
	// session mint is under test (the state cookie keeps its Lax — it must
	// ride the provider's redirect, oauth2.go:241-244).
	if got := mintedSession(cw); got == "" {
		t.Fatalf("setup broken: callback minted no session cookie (Set-Cookie %q)", cw.Header().Values("Set-Cookie"))
	}
	var sessionHeader string
	for _, h := range cw.Header().Values("Set-Cookie") {
		if strings.HasPrefix(h, "session_id=") {
			sessionHeader = h
		}
	}
	if !strings.Contains(sessionHeader, "SameSite=Strict") {
		t.Errorf("SECURITY: [session-cookie-samesite-mint] OAuth callback minted the session cookie as %q, "+
			"want SameSite=Strict — password login's mint is Strict (TestCorePlugin_LoginCookieUsesStrictSameSite); "+
			"a Lax session rides any top-level cross-site GET and re-opens the CSRF exposure Strict closed",
			sessionHeader)
	}
}
