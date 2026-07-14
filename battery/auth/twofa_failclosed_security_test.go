package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: when a user has 2FA enabled but the pending-2FA mark cannot be
// established — the session store doesn't implement SessionPendingMarker,
// the checker errors, or the mark call fails — login must fail CLOSED.
// Otherwise a custom SessionStore silently downgrades every 2FA-enrolled
// account to password-only auth.

// baseOnlySessionStore implements SessionStore and nothing else — the
// shape of a host-supplied Redis/DB store that never heard of the
// optional pending-marker extension.
type baseOnlySessionStore struct{ inner *MemorySessionStore }

func (s *baseOnlySessionStore) Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error) {
	return s.inner.Create(ctx, userID, ttl)
}
func (s *baseOnlySessionStore) Get(ctx context.Context, token string) (*Session, error) {
	return s.inner.Get(ctx, token)
}
func (s *baseOnlySessionStore) Delete(ctx context.Context, token string) error {
	return s.inner.Delete(ctx, token)
}
func (s *baseOnlySessionStore) Cleanup(ctx context.Context) (int, error) {
	return s.inner.Cleanup(ctx)
}

// errorCheckerPlugin reports "I couldn't determine 2FA state" — the DB
// being down must not mean "no second factor required".
type errorCheckerPlugin struct{}

func (errorCheckerPlugin) Name() string                { return "errchecker" }
func (errorCheckerPlugin) Init(mgr *AuthManager) error { return nil }
func (errorCheckerPlugin) HasTwoFactorEnabled(context.Context, string) (bool, error) {
	return false, errors.New("2fa state lookup failed")
}

func setupFailClosed(t *testing.T, store SessionStore, extra ...AuthPlugin) *router.Router {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true, // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
		SessionStore:        store,
	})
	mgr.Use(NewCorePlugin())
	for _, p := range extra {
		mgr.Use(p)
	}
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

// attemptLogin posts valid credentials and returns the response plus any
// session cookie that was set.
func attemptLogin(t *testing.T, r *router.Router) (*httptest.ResponseRecorder, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			return w, c.Value
		}
	}
	return w, ""
}

// assertNoUsableSession proves any granted cookie cannot pass /auth/me.
func assertNoUsableSession(t *testing.T, r *router.Router, tok string) {
	t.Helper()
	if tok == "" {
		return
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("session minted despite 2FA fail-closed condition: /auth/me returned 200 (body=%s)", w.Body.String())
	}
}

func TestTwoFAFailClosed_NoPendingMarker(t *testing.T) {
	store := &baseOnlySessionStore{inner: NewMemorySessionStore()}
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	r := setupFailClosed(t, store, twofa)
	if err := twofa.store.SetTwoFA(context.Background(), "u-1", &TwoFAState{
		Enabled: true, Secret: GenerateSecret(), Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	w, tok := attemptLogin(t, r)
	if w.Code == http.StatusOK {
		t.Fatalf("login must fail closed when store can't mark pending 2FA; got 200 (body=%s)", w.Body.String())
	}
	assertNoUsableSession(t, r, tok)
}

// A pending-2FA login must not hand out a JWT: the token is stateless,
// so it would let the password-only caller skip the challenge on every
// JWT-authenticated route.
func TestPendingTwoFA_NoJWTIssued(t *testing.T) {
	_, _, r := setupP17(t) // memory store + enrolled user: happy pending path
	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("pending login: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["token"]; ok {
		t.Fatalf("pending-2FA login returned a JWT — stateless second-factor bypass (body=%s)", w.Body.String())
	}
	if resp["two_factor_required"] != true {
		t.Fatalf("pending-2FA login should signal two_factor_required (body=%s)", w.Body.String())
	}
}

func TestTwoFAFailClosed_CheckerError(t *testing.T) {
	r := setupFailClosed(t, NewMemorySessionStore(), errorCheckerPlugin{})

	w, tok := attemptLogin(t, r)
	if w.Code == http.StatusOK {
		t.Fatalf("login must fail closed when 2FA state lookup errors; got 200 (body=%s)", w.Body.String())
	}
	assertNoUsableSession(t, r, tok)
}
