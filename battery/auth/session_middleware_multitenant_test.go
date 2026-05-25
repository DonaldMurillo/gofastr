package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// TestSessionMiddleware_ClearsUserOnAnonymousFallthrough pins the
// multi-tenant identity leak fix. When two SessionMiddlewares are
// nested (e.g. mgrA at app-level, mgrB on a per-tenant route group),
// a request that has a valid mgrA cookie BUT NO mgrB cookie must NOT
// carry mgrA's user identity into mgrB's handler. Without the fix,
// the inner middleware's anonymous branch leaves whatever user the
// outer set in ctx — so mgrB's OwnerField CRUD scopes mgrA's user id
// against mgrB's data, leaking rows cross-tenant.
//
// The fix: every anonymous fall-through branch in SessionMiddleware
// must clear `handler.SetUser(ctx, nil)` so the extractor reads
// "no user."
func TestSessionMiddleware_ClearsUserOnAnonymousFallthrough(t *testing.T) {
	mgr := newMultiTenantTestMgr(t)

	// Pre-populate ctx with a user from the "outer" middleware.
	outerUser := &BasicUser{ID: "outer-user", Email: "outer@example.com"}
	var seenInHandler User
	handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = GetCurrentUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Apply SessionMiddleware (the "inner" tenant middleware), then
	// invoke it on a request that already has outer-user in ctx but
	// NO cookie for the inner mgr.
	mw := SessionMiddleware(mgr)
	req := httptest.NewRequest(http.MethodGet, "/inner-route", nil)
	// Simulate outer middleware setting the user.
	req = req.WithContext(handler.SetUser(req.Context(), outerUser))
	rec := httptest.NewRecorder()
	mw(handlerFn).ServeHTTP(rec, req)

	if seenInHandler != nil {
		t.Errorf("IDENTITY LEAK: inner handler saw outer middleware's user %+v (must be nil after anonymous fall-through)", seenInHandler)
	}
}

// TestSessionMiddleware_ClearsUserOnStoreError covers the same fix for
// the store-error branch (store outage shouldn't leave a stale user
// in ctx either).
func TestSessionMiddleware_ClearsUserOnStoreError(t *testing.T) {
	outerUser := &BasicUser{ID: "outer-user", Email: "outer@example.com"}
	var seenInHandler User
	handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = GetCurrentUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mgr := newTestManagerWithStores(t,
		&fakeSessionStore{get: func(_ context.Context, _ string) (*Session, error) {
			return nil, ErrSessionNotFound
		}},
		&fakeUserStore{findByID: func(_ context.Context, _ string) (User, error) { return nil, ErrUserNotFound }},
	)
	mw := SessionMiddleware(mgr)
	req := httptest.NewRequest(http.MethodGet, "/inner", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: "anything"})
	req = req.WithContext(handler.SetUser(req.Context(), outerUser))
	rec := httptest.NewRecorder()
	mw(handlerFn).ServeHTTP(rec, req)

	if seenInHandler != nil {
		t.Errorf("IDENTITY LEAK on store-error: handler saw %+v (must be nil)", seenInHandler)
	}
}

// newMultiTenantTestMgr builds a SessionMiddleware-ready manager with a
// real entity store (so the path under test exercises the real code,
// not a fake). The cookie name is unique per call.
func newMultiTenantTestMgr(t *testing.T) *AuthManager {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "auth-mt-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, ddl := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE, password_hash TEXT, roles TEXT, password_set BOOLEAN)`,
		`CREATE TABLE sessions (token TEXT PRIMARY KEY, user_id TEXT, created_at TEXT, expires_at TEXT, two_factor_verified BOOLEAN, pending_two_factor BOOLEAN)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}

	mgr := New(AuthConfig{
		JWTSecret:     "tenant-test",
		UserStore:     NewEntityUserStore(db, "users"),
		SessionStore:  NewEntitySessionStore(db, "sessions"),
		DevMode:       true,
		SessionTTL:    time.Hour,
		SessionCookie: "inner_tenant_session",
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("mgr.Init: %v", err)
	}
	return mgr
}
