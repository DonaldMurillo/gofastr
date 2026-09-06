package auth

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4 (contract decided:
// revoking a provider link revokes the credentials stored for that
// provider).
// Family: F21 Account lifecycle and alternate paths
// Property: unlink is the user-facing "this app no longer acts as me at
// {provider}" action, and a surviving refresh token (a password-equivalent
// for the provider API) contradicts it.
// Surfaces: accounts.go::unlinkHandler → revokeProviderTokens (both the
// atomic and the legacy path now drop the token row after a successful
// unlink), oauth2.go::OAuth2Plugin.TokenStore (the seam the accounts
// plugin reaches the store through, via PluginAs), oauth_token_store.go::
// SQLOAuthTokenStore.Delete.
// Fix: the OAuth2 plugin's TokenStore (OAuth2Config.TokenStore) is the
// store whose rows unlink drops; RefreshOAuthToken / ValidOAuthToken can
// no longer serve an unlinked provider's credentials. A wiring with no
// OAuth2 plugin, or no TokenStore configured, never persisted provider
// tokens, so there is nothing to drop.
// Note on setup: the red probe built the token store standalone because
// no seam existed at all; the promoted test wires it the way production
// does (OAuth2Config.TokenStore) — the assertion is unchanged.

// TestUnlinkDropsStoredProviderTokens links a provider for a
// password-holding user, persists that provider's tokens, unlinks, and
// asserts the stored credential row is gone.
func TestUnlinkDropsStoredProviderTokens(t *testing.T) {
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
	tokens, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		EncryptionKey: []byte("test-oauth-token-store-key-0123456789"),
	})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}

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
	if err := tokens.Save(ctx, OAuthTokenRecord{
		UserID:       u.GetID(),
		Provider:     "stub",
		AccessToken:  "at-secret", // not-a-secret: test fixture, never a live credential
		RefreshToken: "rt-secret", // not-a-secret: test fixture, never a live credential
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save token: %v", err)
	}

	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     users,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		// The production wiring this contract rides on: the token store
		// the unlink route must drop rows from.
		TokenStore:  tokens,
		StateSecret: "unlink-test-state-01",
	}))
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

	res := jar.do(r, http.MethodDelete, "/auth/unlink/stub", nil, "203.0.113.9:5555")
	if res.Code != http.StatusOK {
		t.Fatalf("setup: unlink got %d %s", res.Code, res.Body.String())
	}

	if _, err := tokens.Get(ctx, u.GetID(), "stub"); err == nil {
		t.Fatalf("SECURITY: [unlink-tokens] the stored OAuth token row for the unlinked provider survived DELETE /auth/unlink/stub — the app keeps a password-equivalent refresh token for a provider the user revoked")
	}
}
