package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthAuditFollowsSessionMint(t *testing.T) {
	for _, failClosed := range []bool{true, false} {
		store := newMemoryUserStore()
		user, err := store.CreateUser(context.Background(), "oauth@example.com", "h", []string{"user"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.LinkOAuth(context.Background(), user.GetID(), "mock", "ext-audit"); err != nil {
			t.Fatal(err)
		}
		provider := &mockProvider{name: "mock", tokenResp: &OAuth2Token{AccessToken: "a"},
			userResp: &OAuth2UserInfo{ID: "ext-audit", Email: "oauth@example.com", EmailVerified: true}}
		sink := &recordingSink{}
		mgr := New(AuthConfig{JWTSecret: "x", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, AllowInMemoryStores: true, AuditSink: sink})
		twofa := NewTwoFAPlugin(TwoFAConfig{})
		mgr.Use(twofa)
		mgr.Use(NewOAuth2Plugin(OAuth2Config{Providers: map[string]OAuth2Provider{"mock": provider}, StateSecret: "test-state-secret-01", TwoFactorURL: "/sign-in/code"}))
		if err := mgr.Init(nil); err != nil {
			t.Fatal(err)
		}
		if failClosed {
			twofa.store = brokenTwoFAStore{twofa.store}
		}
		router := mountOAuth2Routes(mgr)
		redirect := httptest.NewRecorder()
		router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
		location, err := url.Parse(redirect.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		state := location.Query().Get("state")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, oauthCallbackReq("/auth/oauth/mock/callback?code=c&state="+url.QueryEscape(state), state))
		successes, failures := 0, 0
		for _, event := range sink.snapshot() {
			if event.Kind == "oauth.login" || event.Kind == "oauth.linked" {
				successes++
			}
			if event.Kind == "login.failed" && event.Meta["reason"] == "twofa_failclosed" && event.Meta["via"] == "oauth" {
				failures++
			}
		}
		if failClosed {
			if response.Code != 500 || successes != 0 || failures != 1 || !strings.Contains(response.Body.String(), reasonTwoFactorUnavailable.Code()) {
				t.Fatalf("failed mint: status %d, successes %d, failures %d, body %s", response.Code, successes, failures, response.Body)
			}
		} else if response.Code != 302 || successes != 1 || failures != 0 {
			t.Fatalf("successful mint: status %d, successes %d, failures %d", response.Code, successes, failures)
		}
	}
}
