package auth

// Pins: the stored OAuth refresh token never regresses below a
// concurrently-persisted newer grant — RefreshOAuthToken's final write is a
// compare-and-swap (SaveIfRefreshUnchanged) on the refresh token refreshed
// FROM, so a grant persisted between the read and the write survives.
// Found by the 2026-09-06 adversarial pass (round 5).
// The store contract (SaveIfRefreshUnchanged) is what makes the promise
// hold: a plain read-compare-write is check-then-act across two statements.
// Surfaces: battery/auth/oauth_token_store.go::RefreshOAuthToken (re-read ~:335, unconditional
// Save ~:341); battery/auth/oauth_token_store.go::(SQLOAuthTokenStore).Save (~:200, an
// unconditional upsert, no compare-and-swap).
// Finding: the re-read guard is check-then-act. Interleaving that bricks the linkage:
// (1) Get→rt-A; (2) exchange happens (provider rotates rt-A); (3) the re-read snapshots rt-A
// (guard passes); (4) a concurrent login-callback Save lands rt-B AFTER that snapshot;
// (5) the final unconditional Save overwrites rt-B with (access', rt-A) — rt-A is already
// revoked at the provider under rotation, so the linkage bricks until the user re-authorizes.
// Fix direction: make the final write conditional (compare-and-swap on the refresh token
// refreshed FROM) so a row that moved on after the last read is never overwritten.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// lateGrantStore delegates to the inner store and reproduces, deterministically,
// the login-callback Save that lands inside the remaining clobber window: on the
// FIRST store call after the provider exchange completes, it snapshots the row
// (what RefreshOAuthToken's re-read observes), persists the newer grant, and
// returns the PRE-save snapshot. The guard therefore compares against the stale
// state, passes, and the final unconditional Save lands on top of the newer grant.
type lateGrantStore struct {
	inner        OAuthTokenStore
	exchangeDone atomic.Bool // set on the fake endpoint's goroutine
	injected     bool        // touched only on the RefreshOAuthToken goroutine
	injectErr    error
}

func (s *lateGrantStore) Get(ctx context.Context, userID, provider string) (OAuthTokenRecord, error) {
	snap, err := s.inner.Get(ctx, userID, provider)
	if err != nil {
		return snap, err
	}
	s.injectNewerGrant(ctx, userID, provider)
	return snap, nil
}

func (s *lateGrantStore) Save(ctx context.Context, rec OAuthTokenRecord) error {
	s.injectNewerGrant(ctx, rec.UserID, rec.Provider)
	return s.inner.Save(ctx, rec)
}

func (s *lateGrantStore) SaveIfRefreshUnchanged(ctx context.Context, rec OAuthTokenRecord, from string) (bool, error) {
	s.injectNewerGrant(ctx, rec.UserID, rec.Provider)
	return s.inner.SaveIfRefreshUnchanged(ctx, rec, from)
}

func (s *lateGrantStore) Delete(ctx context.Context, userID, provider string) error {
	return s.inner.Delete(ctx, userID, provider)
}

func (s *lateGrantStore) injectNewerGrant(ctx context.Context, userID, provider string) {
	if !s.exchangeDone.Load() || s.injected {
		return
	}
	s.injected = true
	s.injectErr = s.inner.Save(ctx, OAuthTokenRecord{
		UserID:       userID,
		Provider:     provider,
		AccessToken:  "newer-access", // not-a-secret: refresh-race fixture
		RefreshToken: "rt-newer",     // not-a-secret: refresh-race fixture
		Expiry:       time.Now().Add(time.Hour),
	})
}

func TestOAuthRedRefreshClobberWindow(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1) // :memory: is per-connection; one conn shares the DB
	store, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		EncryptionKey: []byte("test-oauth-token-store-key-0123456789"),
	})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}
	ctx := context.Background()

	// The stale grant the refresh path starts from.
	if err := store.Save(ctx, OAuthTokenRecord{
		UserID:       "u1",
		Provider:     "google",
		AccessToken:  "stale-access", // not-a-secret: refresh-race fixture
		RefreshToken: "rt-old",       // not-a-secret: refresh-race fixture
		Expiry:       time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	wrapped := &lateGrantStore{inner: store}

	// Provider-omission behavior (Google's typical refresh grant): a fresh
	// access token and NO refresh_token, so the record the refresh path
	// writes back still carries rt-old. The endpoint flags the exchange as
	// complete so the wrapper knows the remaining window is open.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.exchangeDone.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "refreshed-access", "expires_in": 3600})
	}))
	defer srv.Close()
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	prov.httpClient = &http.Client{Timeout: 60 * time.Second, Transport: tr}

	if _, err := RefreshOAuthToken(ctx, wrapped, prov, "u1"); err != nil {
		t.Fatalf("RefreshOAuthToken: %v", err)
	}
	if wrapped.injectErr != nil {
		t.Fatalf("setup broken: injected login-callback Save failed: %v", wrapped.injectErr)
	}
	if !wrapped.injected {
		t.Fatal("setup broken: no store call observed after the provider exchange; RefreshOAuthToken's call pattern changed")
	}

	got, err := store.Get(ctx, "u1", "google")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "rt-newer" || got.AccessToken != "newer-access" {
		t.Errorf("SECURITY: [oauth-refresh-clobber-window] the refresh path clobbered a concurrently-persisted newer grant: stored (access %q, refresh %q), want (\"newer-access\", \"rt-newer\"). The re-read guard is check-then-act: the login callback's Save landed after the re-read snapshot, and the final unconditional upsert overwrote the rotated grant with the already-revoked rt-old — under provider rotation the linkage bricks until the user re-authorizes, and nothing reports it", got.AccessToken, got.RefreshToken)
	}
}
