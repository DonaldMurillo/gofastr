package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// memoryUserStore is a simple in-memory UserStore for testing.
type memoryUserStore struct {
	users  map[string]*storeEntry // keyed by email
	byID   map[string]*storeEntry // keyed by id
	nextID int
}

type storeEntry struct {
	user User
	hash string
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{
		users: make(map[string]*storeEntry),
		byID:  make(map[string]*storeEntry),
	}
}

func (s *memoryUserStore) FindByEmail(_ context.Context, email string) (User, string, error) {
	e, ok := s.users[email]
	if !ok {
		return nil, "", ErrUserNotFound
	}
	return e.user, e.hash, nil
}

func (s *memoryUserStore) FindByID(_ context.Context, id string) (User, error) {
	e, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return e.user, nil
}

func (s *memoryUserStore) CreateUser(_ context.Context, email, hashedPassword string, roles []string) (User, error) {
	if _, exists := s.users[email]; exists {
		return nil, ErrEmailTaken
	}
	s.nextID++
	id := fmt.Sprintf("user-%d", s.nextID)
	user := &BasicUser{ID: id, Email: email, Roles: roles}
	entry := &storeEntry{user: user, hash: hashedPassword}
	s.users[email] = entry
	s.byID[id] = entry
	return user, nil
}

// ============================================================================
// AuthManager construction
// ============================================================================

func TestAuthManager_New(t *testing.T) {
	mgr := New(AuthConfig{
		JWTSecret: "test-secret",
	})
	if mgr.Name() != "auth" {
		t.Fatalf("expected name 'auth', got %q", mgr.Name())
	}
	if mgr.SessionStore() == nil {
		t.Fatal("expected default session store")
	}
}

func TestAuthManager_UseRegistersPlugins(t *testing.T) {
	mgr := New(AuthConfig{})
	mgr.Use(NewCorePlugin())

	if _, ok := mgr.Plugin("core"); !ok {
		t.Fatal("core plugin should be registered")
	}
}

func TestAuthManager_UseDuplicatePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate plugin")
		}
	}()
	mgr := New(AuthConfig{})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewCorePlugin())
}

// ============================================================================
// CorePlugin routes via AuthManager
// ============================================================================

func newTestManager(t *testing.T) (*AuthManager, *memoryUserStore) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:  "test-secret",
		JWTExpiry:  time.Hour,
		SessionTTL: 24 * time.Hour,
		UserStore:  userStore,
		// httptest serves over plain HTTP — DevMode keeps the cookie
		// readable (no __Host- prefix) and Secure=false so AddCookie works.
		DevMode: true,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, userStore
}

func mountRoutes(mgr *AuthManager) *router.Router {
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

func seedUser(t *testing.T, store *memoryUserStore, email, password string) User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := store.CreateUser(context.Background(), email, hash, []string{"user"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestCorePlugin_Register(t *testing.T) {
	mgr, _ := newTestManager(t)
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{
		"email":    "new@example.com",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	userMap := resp["user"].(map[string]any)
	if userMap["email"] != "new@example.com" {
		t.Fatalf("expected email new@example.com, got %v", userMap["email"])
	}
}

func TestCorePlugin_LoginLogout(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	// Login
	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "session_id=") {
		t.Fatalf("expected session cookie, got %q", w.Header().Get("Set-Cookie"))
	}

	// Verify JWT token in response
	var loginResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	if _, hasToken := loginResp["token"]; !hasToken {
		t.Fatal("expected JWT token in login response")
	}

	// Extract cookie for logout
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}

	// Logout
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, logoutReq)

	if w2.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", w2.Code)
	}
}

func TestCorePlugin_LoginBadPassword(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCorePlugin_Me(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	// Login first
	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)

	var cookie *http.Cookie
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c
		}
	}

	// Me
	meReq := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meReq.AddCookie(cookie)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)

	if meW.Code != http.StatusOK {
		t.Fatalf("me: expected 200, got %d: %s", meW.Code, meW.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(meW.Body.Bytes(), &resp)
	if resp["userId"] == nil {
		t.Fatal("expected userId in me response")
	}
}

func TestCorePlugin_MeUnauthenticated(t *testing.T) {
	mgr, _ := newTestManager(t)
	r := mountRoutes(mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCorePlugin_RegisterDuplicateEmail(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "newpass1"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// ============================================================================
// AuthPlugin lifecycle
// ============================================================================

func TestAuthManager_PluginLifecycle(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "secret"})
	core := NewCorePlugin()
	mgr.Use(core)

	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx := context.Background()
	if err := mgr.OnStart(ctx); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if err := mgr.OnStop(ctx); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
}

// TestAuthManager_DevModeMintsJWT: with no explicit JWTSecret, the only
// reachable boot path is DevMode (production fails closed — see
// TestInit_ProdEmptySecretFailsClosed). DevMode mints a per-process
// secret, so the JWT helper is configured rather than nil.
func TestAuthManager_DevModeMintsJWT(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if mgr.JWT() == nil {
		t.Fatal("DevMode should mint a JWT secret, leaving JWT() non-nil")
	}
}

// AuthConfig must default to Secure cookies and the __Host- prefix.
// Today SessionSecure defaults to false (zero value) and the cookie
// name to plain "session_id" — first-deploy-to-prod ships cleartext
// cookies vulnerable to subdomain injection.

func TestAuthConfig_DefaultsAreSecure(t *testing.T) {
	cfg := AuthConfig{}
	cfg.defaults()

	if !cfg.SessionSecure {
		t.Errorf("SessionSecure must default to true; got false (insecure cookies in production)")
	}
	if cfg.SessionCookie != "__Host-session" {
		t.Errorf("SessionCookie should default to __Host-session for prefix-based protection; got %q", cfg.SessionCookie)
	}
}

func TestAuthConfig_DevMode_RelaxedDefaults(t *testing.T) {
	cfg := AuthConfig{DevMode: true}
	cfg.defaults()

	if cfg.SessionSecure {
		t.Errorf("DevMode must allow Secure=false (HTTP dev); got true")
	}
	if cfg.SessionCookie != "session_id" {
		t.Errorf("DevMode should use plain session_id (no __Host- prefix); got %q", cfg.SessionCookie)
	}
}

// CorePlugin.loginHandler must be timing-safe: a request with a known-
// missing email should run the same bcrypt cost as a request with a
// known-existing email + wrong password. Today missing-email skips
// bcrypt entirely, leaking user existence via response time.
//
// Strategy: take many samples of each path, compare the medians. The
// per-bcrypt cost (≥10ms at default cost) is large enough to swamp
// httptest jitter. We assert the medians are within a tight ratio.

func TestLogin_TimingSafe_NoEnumerationByResponseTime(t *testing.T) {
	mgr := newRLAuthManager(t, RateLimiterConfig{
		MaxAttempts: 1000, Window: time.Hour, BlockDuration: time.Hour,
	})
	r := router.New()
	mgr.RegisterRoutes(r)

	// Seed an existing user.
	hash, err := HashPassword("realpassword")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := mgr.UserStore().(*memoryUserStore)
	store.users["existing@example.com"] = &storeEntry{
		user: &BasicUser{ID: "u-1", Email: "existing@example.com", Roles: []string{"user"}},
		hash: hash,
	}

	// Warm-up — discard one of each so the JIT/init overhead doesn't bias.
	measure(t, r, "existing@example.com", "wrongpassword")
	measure(t, r, "missing@example.com", "anypassword")

	const samples = 5

	var existSum, missingSum time.Duration
	for i := 0; i < samples; i++ {
		existSum += measure(t, r, "existing@example.com", "wrongpassword")
		missingSum += measure(t, r, "missing@example.com", "anypassword")
	}
	existMean := existSum / samples
	missingMean := missingSum / samples

	t.Logf("existing-mean=%v  missing-mean=%v", existMean, missingMean)

	// Both code paths should perform a bcrypt comparison. Bcrypt at
	// default cost is ≥30ms even on fast hardware. If the missing-email
	// path skips bcrypt, missingMean is two orders of magnitude smaller.
	// We require the missing-email mean to be at least 50% of the
	// existing-email mean — this catches "no bcrypt at all" without
	// being fragile under CI jitter.
	if missingMean*2 < existMean {
		t.Fatalf("login is user-enumerable via timing: existing=%v missing=%v (missing must be ≥50%% of existing)",
			existMean, missingMean)
	}
}

func measure(t *testing.T, r *router.Router, email, password string) time.Duration {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(w, req)
	return time.Since(start)
}
