package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Found by the 2026-11-01 review, round 2: an OAuth sign-in of a 2FA
// account minted a pending session and redirected to "/", which refuses
// it, so the user had no way to /2fa/challenge. Passkey and magic link
// already route to a TwoFactorURL; OAuth now does too.

func TestOAuthInitRequiresTwoFactorURLWith2FA(t *testing.T) {
	for _, twoFAFirst := range []bool{true, false} {
		mgr := New(AuthConfig{JWTSecret: "x", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
		oauth := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
		if twoFAFirst {
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
			mgr.Use(oauth)
		} else {
			mgr.Use(oauth)
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
		}
		if err := mgr.Init(nil); err == nil || !strings.Contains(err.Error(), "TwoFactorURL") {
			t.Errorf("2FA first=%v: Init = %v, want a TwoFactorURL refusal", twoFAFirst, err)
		}
	}
}

func TestOAuthPendingSignInGoesToTwoFactorURL(t *testing.T) {
	ctx := context.Background()
	store := newMemoryUserStore()
	u, err := store.CreateUser(ctx, "two@example.com", "h", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkOAuth(ctx, u.GetID(), "mock", "ext-1"); err != nil {
		t.Fatal(err)
	}
	prov := &mockProvider{name: "mock", tokenResp: &OAuth2Token{AccessToken: "a"},
		userResp: &OAuth2UserInfo{ID: "ext-1", Email: "two@example.com", EmailVerified: true}}
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr := New(AuthConfig{JWTSecret: "x", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, AllowInMemoryStores: true})
	mgr.Use(twofa)
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:    map[string]OAuth2Provider{"mock": prov},
		StateSecret:  "test-state-secret-01",
		TwoFactorURL: "/sign-in/code",
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	if err := twofa.store.SetTwoFA(ctx, u.GetID(), &TwoFAState{Secret: "S", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := mountOAuth2Routes(mgr)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	loc := w1.Header().Get("Location")
	state := loc[strings.LastIndex(loc, "state=")+len("state="):]
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, oauthCallbackReq("/auth/oauth/mock/callback?code=c&state="+state, state))
	if w2.Code != http.StatusFound {
		t.Fatalf("callback = %d %s", w2.Code, w2.Body.String())
	}
	if got := w2.Header().Get("Location"); got != "/sign-in/code" {
		t.Errorf("pending OAuth sign-in redirected to %q, want the TwoFactorURL", got)
	}
}
