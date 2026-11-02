package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A path value arrives percent-decoded, and router.Param cuts a NUL but
// keeps invalid UTF-8: DELETE /auth/unlink/a%FFb handed the store a 0xff
// byte. Postgres refuses it in a text bind with 22021, so the unlink and
// the token revoke answered 500 there and a plain miss on SQLite. Both
// handlers now answer the miss (404) without reaching the store.

type unlinkRecorder struct {
	*EntityUserStore
	providers []string
}

func (s *unlinkRecorder) UnlinkOAuthGuarded(ctx context.Context, userID, provider string) (UnlinkOutcome, error) {
	s.providers = append(s.providers, provider)
	return s.EntityUserStore.UnlinkOAuthGuarded(ctx, userID, provider)
}

func TestUnlinkRefusesUnstorableProvider(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	users := &unlinkRecorder{EntityUserStore: NewEntityUserStore(db, "users")}
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("supersecret1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "alice@example.com", hash, []string{"user"}); err != nil {
		t.Fatal(err)
	}
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour, SessionCookie: "session_id",
		UserStore: users, DevMode: true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	jar := &cookieJar{}
	if login := jar.do(r, http.MethodPost, "/auth/login",
		map[string]string{"email": "alice@example.com", "password": "supersecret1"}, "203.0.113.9:5555"); login.Code != http.StatusOK && login.Code != http.StatusSeeOther {
		t.Fatalf("setup: login got %d %s", login.Code, login.Body.String())
	}

	res := jar.do(r, http.MethodDelete, "/auth/unlink/a%FFb", nil, "203.0.113.9:5555")
	if res.Code != http.StatusNotFound {
		t.Errorf("DELETE /auth/unlink/a%%FFb = %d %s, want 404", res.Code, res.Body.String())
	}
	if len(users.providers) != 0 {
		t.Errorf("the store was asked to unlink %q; a provider that is not storable text must not reach it", users.providers)
	}
}

type revokeRecorder struct {
	APITokenStore
	ids []string
}

func (s *revokeRecorder) Revoke(ctx context.Context, id, ownerKind, ownerID string) error {
	s.ids = append(s.ids, id)
	return s.APITokenStore.Revoke(ctx, id, ownerKind, ownerID)
}

func TestRevokeTokenRefusesUnstorableID(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	store := &revokeRecorder{APITokenStore: ts}
	mgr := New(AuthConfig{JWTSecret: "plugin-test", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(store)
	plugin.Init(mgr)
	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	for _, id := range []string{"a\xffb", "\xc3"} {
		req := httptest.NewRequest(http.MethodDelete, "/auth/tokens/x", nil)
		req.SetPathValue("id", id)
		req = req.WithContext(handler.SetUser(req.Context(), alice))
		rec := httptest.NewRecorder()
		plugin.revokeTokenHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("DELETE /auth/tokens/%q = %d %s, want 404", id, rec.Code, rec.Body.String())
		}
	}
	if len(store.ids) != 0 {
		t.Errorf("the store was asked to revoke %q; an id that is not storable text must not reach it", store.ids)
	}
}
