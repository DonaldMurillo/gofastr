package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// A sibling subdomain can set a cookie for the parent domain. With the
// browser-binding cookie named plain gofastr_oauth_state, an attacker on
// evil.example.com started a flow for their own account, planted
// gofastr_oauth_state=<their state>; Domain=example.com on the callback
// path, and sent the victim to the captured callback: the binding passed
// and the victim held the attacker's session (2026-10-27 A26). Over Secure
// the cookie is __Host- prefixed, which a browser accepts only host-only,
// Secure and Path=/, so no sibling can plant it, and the callback reads
// that name alone.
func TestOAuthStateCookieHostPrefixed(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{ID: "attacker-1", Email: "attacker@evil.example"},
	})
	r := mountOAuth2Routes(mgr)

	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("redirect = %d, want 302", start.Code)
	}
	loc, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	state := loc.Query().Get("state")
	var bound *http.Cookie
	for _, c := range start.Result().Cookies() {
		if c.Value == state {
			bound = c
		}
	}
	if bound == nil {
		t.Fatal("setup: no cookie carries the state")
	}
	if bound.Name != "__Host-gofastr_oauth_state" || !bound.Secure || bound.Path != "/" || bound.Domain != "" {
		t.Errorf("SECURITY: state cookie = %q (Secure %v, Path %q, Domain %q); want __Host-gofastr_oauth_state, Secure, Path=/, host-only",
			bound.Name, bound.Secure, bound.Path, bound.Domain)
	}

	// The victim's browser carries only what the sibling planted.
	cb := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+url.QueryEscape(state)+"&code=abc", nil)
	cb.AddCookie(&http.Cookie{Name: "gofastr_oauth_state", Value: state})
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, cb)
	if cw.Code == http.StatusFound || cw.Code == http.StatusOK {
		t.Errorf("SECURITY: a planted unprefixed state cookie passed the browser binding (status %d)", cw.Code)
	}
}

// Over plain HTTP in DevMode a __Host- cookie would be refused by the
// browser (it requires Secure), so the dev flow keeps the plain name.
func TestOAuthStateCookieDevPlain(t *testing.T) {
	mgr := New(AuthConfig{
		DevMode:             true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           newMemoryUserStore(),
		AllowInMemoryStores: true,
	})
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{"mock": &mockProvider{
			name:      "mock",
			tokenResp: &OAuth2Token{AccessToken: "tok"},
			userResp:  &OAuth2UserInfo{ID: "u-1", Email: "alice@example.com"},
		}},
		StateSecret: "test-secret-key-state",
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := mountOAuth2Routes(mgr)
	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	cb := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+url.QueryEscape(state)+"&code=abc", nil)
	named := false
	for _, c := range start.Result().Cookies() {
		named = named || c.Name == "gofastr_oauth_state"
		cb.AddCookie(c)
	}
	if !named {
		t.Errorf("dev state cookie is not gofastr_oauth_state: %v", start.Result().Cookies())
	}
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, cb)
	if cw.Code != http.StatusFound && cw.Code != http.StatusOK {
		t.Errorf("dev callback = %d, want success (body=%s)", cw.Code, cw.Body.String())
	}
}
