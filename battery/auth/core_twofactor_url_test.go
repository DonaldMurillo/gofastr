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

// Decision 2026-11-01: an HTML form login of a 2FA account redirected to
// its `next` page (or "/"), which refuses the pending session it had just
// minted, so the user had no way to /2fa/challenge. CorePlugin now needs
// WithTwoFactorURL next to a 2FA plugin, as passkey, magic link and OAuth do.

func TestCoreInitRequiresTwoFactorURLWith2FA(t *testing.T) {
	for _, twoFAFirst := range []bool{true, false} {
		mgr := New(AuthConfig{JWTSecret: "x", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
		if twoFAFirst {
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
			mgr.Use(NewCorePlugin())
		} else {
			mgr.Use(NewCorePlugin())
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
		}
		if err := mgr.Init(nil); err == nil || !strings.Contains(err.Error(), "WithTwoFactorURL") {
			t.Errorf("2FA first=%v: Init = %v, want a WithTwoFactorURL refusal", twoFAFirst, err)
		}
	}
	// Without a 2FA plugin nothing is required.
	mgr := New(AuthConfig{JWTSecret: "x", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Errorf("core alone: Init = %v", err)
	}
}

func TestCoreFormLoginOf2FAAccountGoesToTwoFactorURL(t *testing.T) {
	ctx := context.Background()
	store := newMemoryUserStore()
	hash, _ := HashPassword("correct-horse-1")
	two, _ := store.CreateUser(ctx, "two@example.com", hash, []string{"user"})
	if _, err := store.CreateUser(ctx, "one@example.com", hash, []string{"user"}); err != nil {
		t.Fatal(err)
	}
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr := New(AuthConfig{JWTSecret: "x", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/sign-in/code"))
	mgr.Use(twofa)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	if err := twofa.store.SetTwoFA(ctx, two.GetID(), &TwoFAState{Secret: "S", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := mountRoutes(mgr)
	login := func(email string) string {
		form := url.Values{"email": {email}, "password": {"correct-horse-1"}, "next": {"/dashboard"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("%s: form login = %d %s", email, w.Code, w.Body.String())
		}
		return w.Header().Get("Location")
	}
	if got := login("two@example.com"); got != "/sign-in/code" {
		t.Errorf("2FA account's form login redirected to %q, want the TwoFactorURL", got)
	}
	if got := login("one@example.com"); got != "/dashboard" {
		t.Errorf("an account without 2FA redirected to %q, want its next page", got)
	}
}
