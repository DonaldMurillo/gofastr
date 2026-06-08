package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// compile-time interface check.
var _ OAuthTokenStore = (*SQLOAuthTokenStore)(nil)

func newSQLOAuthTokenStore(t *testing.T) *SQLOAuthTokenStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		EncryptionKey: []byte("test-oauth-token-store-key-0123456789"),
	})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}
	return s
}

func TestOAuthTokenStore_SaveGet(t *testing.T) {
	s := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	rec := OAuthTokenRecord{
		UserID:       "u1",
		Provider:     "google",
		AccessToken:  "at-1",
		RefreshToken: "rt-1",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := s.Save(ctx, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Get(ctx, "u1", "google")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "at-1" || got.RefreshToken != "rt-1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.Expiry.Equal(rec.Expiry) {
		t.Fatalf("expiry mismatch: got %v want %v", got.Expiry, rec.Expiry)
	}
}

func TestOAuthTokenStore_SaveUpserts(t *testing.T) {
	s := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	base := OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "old", RefreshToken: "rt", Expiry: time.Now().Add(time.Hour)}
	if err := s.Save(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.AccessToken = "new"
	if err := s.Save(ctx, base); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "u1", "google")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new" {
		t.Fatalf("upsert failed: got %q want new", got.AccessToken)
	}
}

func TestOAuthTokenStore_GetNotFound(t *testing.T) {
	s := newSQLOAuthTokenStore(t)
	if _, err := s.Get(context.Background(), "nobody", "google"); err != ErrOAuthTokenNotFound {
		t.Fatalf("Get err = %v, want ErrOAuthTokenNotFound", err)
	}
}

func TestOAuthTokenStore_StoredOpaque(t *testing.T) {
	// Tokens must not be stored as bare plaintext columns: a DB dump should
	// not leak the live access/refresh secrets. The SQL store seals them.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{EncryptionKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}
	ctx := context.Background()
	if err := s.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "super-secret-access", RefreshToken: "super-secret-refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var rawAccess, rawRefresh string
	row := db.QueryRowContext(ctx, "SELECT access_token, refresh_token FROM oauth_tokens WHERE user_id='u1'")
	if err := row.Scan(&rawAccess, &rawRefresh); err != nil {
		t.Fatal(err)
	}
	if rawAccess == "super-secret-access" || rawRefresh == "super-secret-refresh" {
		t.Fatalf("tokens stored as plaintext: access=%q refresh=%q", rawAccess, rawRefresh)
	}
	// And it still round-trips back to plaintext.
	got, err := s.Get(ctx, "u1", "google")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "super-secret-access" || got.RefreshToken != "super-secret-refresh" {
		t.Fatalf("encrypted round-trip mismatch: %+v", got)
	}
}

// fakeTokenEndpoint serves a refresh_token grant returning a fresh access token.
func fakeTokenEndpoint(t *testing.T, newAccess string, expiresIn int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") == "" {
			http.Error(w, "expected refresh_token grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": newAccess,
			"expires_in":   expiresIn,
			"token_type":   "Bearer",
		})
	}))
}

func TestRefresh_ExpiredTokenRefreshed(t *testing.T) {
	srv := fakeTokenEndpoint(t, "fresh-access", 3600)
	defer srv.Close()

	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL

	store := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	// Stored token is already expired but has a refresh token.
	if err := store.Save(ctx, OAuthTokenRecord{
		UserID:       "u1",
		Provider:     "google",
		AccessToken:  "stale-access",
		RefreshToken: "rt-valid",
		Expiry:       time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	rec, err := RefreshOAuthToken(ctx, store, prov, "u1")
	if err != nil {
		t.Fatalf("RefreshOAuthToken: %v", err)
	}
	if rec.AccessToken != "fresh-access" {
		t.Fatalf("access token not refreshed: got %q", rec.AccessToken)
	}
	// Store updated.
	got, err := store.Get(ctx, "u1", "google")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "fresh-access" {
		t.Fatalf("store not updated: got %q", got.AccessToken)
	}
	if !got.Expiry.After(time.Now()) {
		t.Fatalf("expiry not advanced: %v", got.Expiry)
	}
}

func TestRefresh_KeepsRefreshTokenWhenOmitted(t *testing.T) {
	// Google does not re-issue the refresh token on a refresh grant; the
	// stored one must be retained.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh", "expires_in": 3600})
	}))
	defer srv.Close()
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	store := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	_ = store.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "old", RefreshToken: "rt-keep", Expiry: time.Now().Add(-time.Minute)})
	rec, err := RefreshOAuthToken(ctx, store, prov, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.RefreshToken != "rt-keep" {
		t.Fatalf("refresh token dropped: got %q want rt-keep", rec.RefreshToken)
	}
}

func TestValidToken_RefreshesNearExpiry(t *testing.T) {
	srv := fakeTokenEndpoint(t, "renewed", 3600)
	defer srv.Close()
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	store := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	// Expires in 10s — inside the default skew window, so ValidToken refreshes.
	_ = store.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "almost-dead", RefreshToken: "rt", Expiry: time.Now().Add(10 * time.Second)})
	at, err := ValidOAuthToken(ctx, store, prov, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if at != "renewed" {
		t.Fatalf("ValidOAuthToken did not refresh near expiry: got %q", at)
	}
}

func TestValidToken_NoRefreshWhenFresh(t *testing.T) {
	// Endpoint that fails the test if hit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("token endpoint should not be called for a fresh token")
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	store := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	_ = store.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "google", AccessToken: "fresh", RefreshToken: "rt", Expiry: time.Now().Add(time.Hour)})
	at, err := ValidOAuthToken(ctx, store, prov, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if at != "fresh" {
		t.Fatalf("got %q want fresh", at)
	}
}

func TestLogin_PersistsRefreshToken(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		tokenResp: &OAuth2Token{
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			Expiry:       time.Now().Add(time.Hour),
		},
		userResp: &OAuth2UserInfo{ID: "ext-1", Email: "bob@example.com", Name: "Bob", Provider: "mock"},
	}
	userStore := newMemoryUserStore()
	tokStore := newSQLOAuthTokenStore(t)
	mgr := New(AuthConfig{SessionTTL: 24 * time.Hour, SessionCookie: "session_id", UserStore: userStore})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"mock": mock},
		StateSecret: "test-secret-key",
		TokenStore:  tokStore,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, userStore, "bob@example.com", "pw-existing")

	r := mountOAuth2Routes(mgr)
	// Redirect to mint a valid state.
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	state := strings.TrimPrefix(rw.Header().Get("Location"), "https://example.com/auth?state=")

	cbW := httptest.NewRecorder()
	r.ServeHTTP(cbW, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?code=c&state="+state, nil))
	if cbW.Code != http.StatusFound {
		t.Fatalf("callback code = %d: %s", cbW.Code, cbW.Body.String())
	}

	u, _, err := userStore.FindByEmail(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := tokStore.Get(context.Background(), u.GetID(), "mock")
	if err != nil {
		t.Fatalf("token not persisted at login: %v", err)
	}
	if rec.RefreshToken != "refresh-1" {
		t.Fatalf("refresh token = %q, want refresh-1", rec.RefreshToken)
	}
}

func TestGoogleRefreshToken_ParsesResponse(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ga", "expires_in": 1800})
	}))
	defer srv.Close()
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	tok, err := prov.RefreshToken(context.Background(), "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ga" {
		t.Fatalf("access = %q", tok.AccessToken)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("client_id") != "cid" {
		t.Fatalf("unexpected form: %v", gotForm)
	}
}

func TestOAuthStore_RequiresKey(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	// Empty EncryptionKey must fail closed — refresh tokens are password-
	// equivalent and must never be sealed with a default key.
	if _, err := NewSQLOAuthTokenStore(db); err == nil {
		t.Fatal("expected error for empty EncryptionKey, got nil")
	}
	if _, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{EncryptionKey: []byte("real-key-material-xyz")}); err != nil {
		t.Fatalf("non-empty key should succeed: %v", err)
	}
}
