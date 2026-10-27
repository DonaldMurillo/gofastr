package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// failFirstDelete fails its first Delete, the transient error that
// left an unlink half done.
type failFirstDelete struct {
	OAuthTokenStore
	failed atomic.Bool
}

func (s *failFirstDelete) Delete(ctx context.Context, userID, provider string) error {
	if s.failed.CompareAndSwap(false, true) {
		return errors.New("transient delete failure")
	}
	return s.OAuthTokenStore.Delete(ctx, userID, provider)
}

// An unlink whose token delete fails has already removed the link, so
// the retry finds nothing linked. It must still drop the stored
// refresh token, or no path ever removes it.
func TestUnlinkRetryDropsStrandedToken(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	users := NewEntityUserStore(db, "users")
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	inner, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		EncryptionKey: []byte("test-oauth-token-store-key-0123456789"),
	})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}
	tokens := &failFirstDelete{OAuthTokenStore: inner}

	hash, err := HashPassword("supersecret1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.CreateUser(ctx, "alice@example.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.LinkOAuth(ctx, u.GetID(), "stub", "ext-1"); err != nil {
		t.Fatalf("LinkOAuth: %v", err)
	}
	if err := inner.Save(ctx, OAuthTokenRecord{
		UserID: u.GetID(), Provider: "stub",
		AccessToken:  "at-secret", // not-a-secret: test fixture, never a live credential
		RefreshToken: "rt-secret", // not-a-secret: test fixture, never a live credential
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save token: %v", err)
	}

	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour,
		SessionCookie: "session_id", UserStore: users, DevMode: true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewOAuth2Plugin(OAuth2Config{TokenStore: tokens, StateSecret: "unlink-test-state-01"}))
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	jar := &cookieJar{}
	login := jar.do(r, http.MethodPost, "/auth/login",
		map[string]string{"email": "alice@example.com", "password": "supersecret1"}, "203.0.113.9:5555")
	if login.Code != http.StatusOK && login.Code != http.StatusSeeOther {
		t.Fatalf("setup: login got %d %s", login.Code, login.Body.String())
	}

	if res := jar.do(r, http.MethodDelete, "/auth/unlink/stub", nil, "203.0.113.9:5555"); res.Code != http.StatusInternalServerError {
		t.Fatalf("first unlink = %d %s, want 500 from the failed token delete", res.Code, res.Body.String())
	}
	_ = jar.do(r, http.MethodDelete, "/auth/unlink/stub", nil, "203.0.113.9:5555")
	if _, err := inner.Get(ctx, u.GetID(), "stub"); err == nil {
		t.Fatal("the retried unlink left the provider's refresh token stored")
	}
}
