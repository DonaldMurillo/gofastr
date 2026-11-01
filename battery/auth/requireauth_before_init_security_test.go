package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Found by the 2026-11-01 battery/auth bug bash. The JWTAuth appeared only
// in Init, so the usual wiring, RequireAuth(mgr.JWT()) before App.Start,
// handed the middleware nil: every valid bearer request panicked and the
// "no auth is wired" banner never cleared.
func TestRequireAuthWiredBeforeInit(t *testing.T) {
	store := newMemoryUserStore()
	hash, _ := HashPassword("correct horse battery")
	u, err := store.CreateUser(context.Background(), "a@example.com", hash, []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(AuthConfig{DevMode: true, JWTSecret: "s3cret-0123456789abcdef", UserStore: store})
	fwApp := framework.NewApp(framework.WithoutDefaultMiddleware())
	mw := RequireAuth(mgr.JWT())
	if err := mgr.Init(fwApp); err != nil {
		t.Fatal(err)
	}
	if !fwApp.AuthenticatorNoted() {
		t.Error("RequireAuth(mgr.JWT()) wired before Init never quieted the no-auth banner")
	}
	tok, err := mgr.JWT().GenerateToken(u)
	if err != nil {
		t.Fatal(err)
	}
	var ran string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cu := GetCurrentUser(r.Context()); cu != nil {
			ran = cu.GetID()
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("valid JWT through RequireAuth wired before Init panicked: %v", p)
			}
		}()
		h.ServeHTTP(rec, req)
	}()
	if rec.Code != http.StatusOK || ran != u.GetID() {
		t.Errorf("valid JWT: status %d ran=%q, want 200 as %s", rec.Code, ran, u.GetID())
	}
}

func TestRequireAuthNilPanicsAtWiring(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("RequireAuth(nil) did not panic at wiring time")
		}
	}()
	RequireAuth(nil)
}

func TestNegativeJWTExpiryRefused(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, JWTSecret: "s3cret-0123456789abcdef", JWTExpiry: -time.Hour, UserStore: newMemoryUserStore()})
	if err := mgr.Init(nil); err == nil {
		t.Error("Init accepted a negative JWTExpiry")
	}
}
