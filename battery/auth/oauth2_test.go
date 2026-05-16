package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Aliases used only by mintBackdatedState — keep them at file scope so
// edits to the production token shape force matching updates in tests.
var (
	stateTestRand = rand.Reader
	stateTestB64  = base64.RawURLEncoding
)

// ─── Mock provider ──────────────────────────────────────────────────────────

type mockProvider struct {
	name       string
	tokenResp  *OAuth2Token
	tokenErr   error
	userResp   *OAuth2UserInfo
	userErr    error
	authURLFmt string
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) AuthURL(state string) string {
	if m.authURLFmt != "" {
		return fmt.Sprintf(m.authURLFmt, state)
	}
	return "https://example.com/auth?state=" + state
}

func (m *mockProvider) ExchangeCode(_ context.Context, code string) (*OAuth2Token, error) {
	if m.tokenErr != nil {
		return nil, m.tokenErr
	}
	return m.tokenResp, nil
}

func (m *mockProvider) FetchUserInfo(_ context.Context, token string) (*OAuth2UserInfo, error) {
	if m.userErr != nil {
		return nil, m.userErr
	}
	return m.userResp, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func newOAuth2Manager(t *testing.T, provider OAuth2Provider) (*AuthManager, *memoryUserStore) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		SessionTTL:    24 * time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
	})

	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{
			provider.Name(): provider,
		},
		StateSecret: "test-secret-key",
	})
	mgr.Use(plugin)

	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, userStore
}

func mountOAuth2Routes(mgr *AuthManager) *router.Router {
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestOAuth2Plugin_Name(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test"})
	if p.Name() != "oauth2" {
		t.Fatalf("expected name 'oauth2', got %q", p.Name())
	}
}

func TestOAuth2Plugin_Init(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test"})
	mgr := New(AuthConfig{})
	if err := p.Init(mgr); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.mgr == nil {
		t.Fatal("mgr should be set after Init")
	}
}

func TestOAuth2Plugin_RegisterProvider(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test"})
	mock := &mockProvider{name: "custom"}
	p.RegisterProvider("custom", mock)
	if _, ok := p.providers["custom"]; !ok {
		t.Fatal("provider should be registered")
	}
}

func TestOAuth2Plugin_Redirect_UnknownProvider(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{
		name: "mock",
	})
	r := mountOAuth2Routes(mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown oauth provider") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestOAuth2Plugin_Redirect_Success(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{name: "mock"})
	r := mountOAuth2Routes(mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "https://example.com/auth?state=") {
		t.Fatalf("expected redirect to mock auth URL, got %q", loc)
	}
}

func TestOAuth2Plugin_StateGenerationAndValidation(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-secret"})

	state, err := p.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}

	if !p.validateAndConsumeState(state, "mock") {
		t.Fatal("state should be valid")
	}

	// Replay should fail (state consumed)
	if p.validateAndConsumeState(state, "mock") {
		t.Fatal("replayed state should be rejected")
	}
}

func TestOAuth2Plugin_StateWrongProvider(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-secret"})

	state, err := p.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}

	if p.validateAndConsumeState(state, "other") {
		t.Fatal("state should not validate for different provider")
	}
}

func TestOAuth2Plugin_StateExpiry(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-secret"})

	// The stateless token now carries the expiry, so simulate expiry
	// by minting a token with an explicit past expiry and the same
	// HMAC the production code would produce.
	state := mintBackdatedState(t, p, "mock", time.Now().Add(-15*time.Minute))

	if p.validateAndConsumeState(state, "mock") {
		t.Fatal("expired state should be rejected")
	}
}

// mintBackdatedState recreates the on-wire encoding of generateState
// with a caller-chosen expiry — used by tests that need to plant an
// already-expired (but otherwise well-signed) token. Mirrors the format
// in oauth2.go:generateState so any change there must update this too.
func mintBackdatedState(t *testing.T, p *OAuth2Plugin, providerName string, expiry time.Time) string {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := stateTestRand.Read(nonce); err != nil {
		t.Fatalf("rand: %v", err)
	}
	nonceB64 := stateTestB64.EncodeToString(nonce)
	expiryStr := strconv.FormatInt(expiry.Unix(), 10)
	payload := nonceB64 + "." + providerName + "." + expiryStr
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	sig := stateTestB64.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// TestOAuth2Plugin_StateSurvivesRestart_Stateless pins the
// stateless-token contract: a freshly-minted token can be validated by
// a SECOND plugin instance that shares the same signing key — i.e. the
// redirect-side server can restart before the callback arrives without
// invalidating the in-flight OAuth flow. Pre-stateless, the per-process
// stateStore made this impossible.
func TestOAuth2Plugin_StateSurvivesRestart_Stateless(t *testing.T) {
	p1 := NewOAuth2Plugin(OAuth2Config{StateSecret: "shared-key"})
	p2 := NewOAuth2Plugin(OAuth2Config{StateSecret: "shared-key"})

	state, err := p1.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if !p2.validateAndConsumeState(state, "mock") {
		t.Fatal("stateless state must validate on a freshly-restarted process with the same key")
	}
}

// TestOAuth2Plugin_StateReplayRejected pins the nonce-replay defense:
// a second validate of the same state token returns false. With
// stateless tokens, replay would otherwise pass HMAC + expiry checks
// trivially because the token contains everything it needs to
// re-verify. The usedNonces LRU is what catches it.
func TestOAuth2Plugin_StateReplayRejected(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "replay-test-key"})

	state, err := p.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if !p.validateAndConsumeState(state, "mock") {
		t.Fatal("first validate must succeed")
	}
	if p.validateAndConsumeState(state, "mock") {
		t.Fatal("replayed state must be rejected by nonce dedup")
	}
}

func TestOAuth2Plugin_StateTampered(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-secret"})

	state, err := p.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}

	// Tamper with the state
	tampered := "tampered" + state[5:]

	if p.validateAndConsumeState(tampered, "mock") {
		t.Fatal("tampered state should be rejected")
	}
}

func TestOAuth2Plugin_Callback_SuccessExistingUser(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		tokenResp: &OAuth2Token{
			AccessToken: "test-access-token",
			Expiry:      time.Now().Add(time.Hour),
		},
		userResp: &OAuth2UserInfo{
			ID:       "ext-123",
			Email:    "alice@example.com",
			Name:     "Alice",
			Provider: "mock",
		},
	}
	mgr, userStore := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)

	// Pre-seed a user
	seedUser(t, userStore, "alice@example.com", "existingpassword")

	// First, hit redirect to get a valid state
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, redirectReq)
	loc := redirectW.Header().Get("Location")
	state := strings.TrimPrefix(loc, "https://example.com/auth?state=")

	// Now hit the callback
	callbackURL := "/auth/oauth/mock/callback?code=test-code&state=" + state
	cbReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	cbW := httptest.NewRecorder()
	r.ServeHTTP(cbW, cbReq)

	if cbW.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", cbW.Code, cbW.Body.String())
	}

	// Check session cookie was set
	var cookie *http.Cookie
	for _, c := range cbW.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// Verify session is valid
	sess, err := mgr.SessionStore().Get(context.Background(), cookie.Value)
	if err != nil {
		t.Fatalf("session should be valid: %v", err)
	}
	if sess.UserID == "" {
		t.Fatal("session should have a user ID")
	}
}

func TestOAuth2Plugin_Callback_SuccessNewUser(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		tokenResp: &OAuth2Token{
			AccessToken: "test-access-token",
			Expiry:      time.Now().Add(time.Hour),
		},
		userResp: &OAuth2UserInfo{
			ID:       "ext-456",
			Email:    "newuser@example.com",
			Name:     "New User",
			Provider: "mock",
		},
	}
	mgr, userStore := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)

	// No pre-seeded user — should auto-create

	// Get state
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, redirectReq)
	loc := redirectW.Header().Get("Location")
	state := strings.TrimPrefix(loc, "https://example.com/auth?state=")

	// Callback
	callbackURL := "/auth/oauth/mock/callback?code=test-code&state=" + state
	cbReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	cbW := httptest.NewRecorder()
	r.ServeHTTP(cbW, cbReq)

	if cbW.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", cbW.Code, cbW.Body.String())
	}

	// Verify user was created
	user, _, err := userStore.FindByEmail(context.Background(), "newuser@example.com")
	if err != nil {
		t.Fatalf("user should have been auto-created: %v", err)
	}
	if user.GetEmail() != "newuser@example.com" {
		t.Fatalf("expected email newuser@example.com, got %q", user.GetEmail())
	}
}

func TestOAuth2Plugin_Callback_InvalidState(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{Email: "x@x.com"},
	}
	mgr, _ := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)

	cbURL := "/auth/oauth/mock/callback?code=test-code&state=bad-state"
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid state, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid or expired state") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestOAuth2Plugin_Callback_MissingCode(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{
		Providers:  map[string]OAuth2Provider{"mock": &mockProvider{name: "mock"}},
		StateSecret: "test",
	})
	mgr := New(AuthConfig{})
	p.Init(mgr)

	// Generate a valid state manually
	state, _ := p.generateState("mock")

	r := router.New()
	p.RegisterRoutes(r, "/auth")

	cbURL := "/auth/oauth/mock/callback?state=" + state
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing code, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing authorisation code") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestOAuth2Plugin_Callback_ExchangeError(t *testing.T) {
	mock := &mockProvider{
		name:      "mock",
		tokenErr:  fmt.Errorf("exchange failed"),
	}
	mgr, _ := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)

	// Get state
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, redirectReq)
	loc := redirectW.Header().Get("Location")
	state := strings.TrimPrefix(loc, "https://example.com/auth?state=")

	cbURL := "/auth/oauth/mock/callback?code=bad-code&state=" + state
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for exchange failure, got %d", w.Code)
	}
}

func TestOAuth2Plugin_Callback_UserInfoError(t *testing.T) {
	mock := &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userErr:   fmt.Errorf("userinfo failed"),
	}
	mgr, _ := newOAuth2Manager(t, mock)
	r := mountOAuth2Routes(mgr)

	// Get state
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, redirectReq)
	loc := redirectW.Header().Get("Location")
	state := strings.TrimPrefix(loc, "https://example.com/auth?state=")

	cbURL := "/auth/oauth/mock/callback?code=test-code&state=" + state
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for userinfo failure, got %d", w.Code)
	}
}

func TestOAuth2Plugin_Callback_UnknownProvider(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{name: "mock"})
	r := mountOAuth2Routes(mgr)

	cbURL := "/auth/oauth/unknown/callback?code=x&state=y"
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown provider, got %d", w.Code)
	}
}

func TestOAuth2Plugin_Callback_NoUserStore(t *testing.T) {
	mock := &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{Email: "x@x.com"},
	}
	// Create manager WITHOUT a user store
	mgr := New(AuthConfig{})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"mock": mock},
		StateSecret: "test",
	})
	mgr.Use(plugin)
	mgr.Init(nil)

	r := router.New()
	mgr.RegisterRoutes(r)

	// Get state
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	redirectW := httptest.NewRecorder()
	r.ServeHTTP(redirectW, redirectReq)
	loc := redirectW.Header().Get("Location")
	state := strings.TrimPrefix(loc, "https://example.com/auth?state=")

	cbURL := "/auth/oauth/mock/callback?code=test-code&state=" + state
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no user store, got %d", w.Code)
	}
}

// ─── Built-in provider tests ────────────────────────────────────────────────

func TestGoogleProvider_Name(t *testing.T) {
	p := NewGoogleProvider("id", "secret", "http://localhost/cb")
	if p.Name() != "google" {
		t.Fatalf("expected 'google', got %q", p.Name())
	}
}

func TestGoogleProvider_AuthURL(t *testing.T) {
	p := NewGoogleProvider("test-id", "secret", "http://localhost/cb")
	u := p.AuthURL("mystate")
	if !strings.Contains(u, "accounts.google.com") {
		t.Fatalf("expected google auth URL, got %q", u)
	}
	if !strings.Contains(u, "client_id=test-id") {
		t.Fatalf("expected client_id in URL, got %q", u)
	}
	if !strings.Contains(u, "state=mystate") {
		t.Fatalf("expected state in URL, got %q", u)
	}
}

func TestGitHubProvider_Name(t *testing.T) {
	p := NewGitHubProvider("id", "secret", "http://localhost/cb")
	if p.Name() != "github" {
		t.Fatalf("expected 'github', got %q", p.Name())
	}
}

func TestGitHubProvider_AuthURL(t *testing.T) {
	p := NewGitHubProvider("test-id", "secret", "http://localhost/cb")
	u := p.AuthURL("mystate")
	if !strings.Contains(u, "github.com/login/oauth/authorize") {
		t.Fatalf("expected github auth URL, got %q", u)
	}
	if !strings.Contains(u, "client_id=test-id") {
		t.Fatalf("expected client_id in URL, got %q", u)
	}
	if !strings.Contains(u, "state=mystate") {
		t.Fatalf("expected state in URL, got %q", u)
	}
}


// OAuth providers must use an http.Client with a request timeout.
// Today both GoogleProvider and GitHubProvider use http.DefaultClient
// (no timeout). An IdP that hangs the connection pins one goroutine
// + two TCP fds per inflight callback until the kernel kills the
// socket — minutes. At 200 callbacks/s a one-minute stall = ~12k
// stuck goroutines, easy OOM/EMFILE.
//
// We verify by pointing the provider at a stub server that sleeps
// longer than any sensible deadline. The provider call must return
// an error within a tight bound, NOT hang for the full sleep.

// newHangServer returns a server that holds open the connection for at
// most `sleep` but exits early if the client disconnects (so test
// cleanup isn't blocked when the client times out).
func newHangServer(t *testing.T, sleep time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(sleep):
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func TestGoogleProvider_TimeoutOnSlowTokenEndpoint(t *testing.T) {
	hang := newHangServer(t, 1*time.Second)
	defer hang.Close()

	prov := NewGoogleProvider("client-id", "client-secret", "http://localhost/cb")
	prov.tokenEndpoint = hang.URL
	prov.userInfoEndpoint = hang.URL
	// 500ms timeout for fast feedback; production default is 10s.
	prov.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	start := time.Now()
	_, err := prov.ExchangeCode(context.Background(), "fake-code")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected error from hanging endpoint, got nil")
	}
	if elapsed > 1*time.Second {
		t.Fatalf("ExchangeCode took %v — must time out near the configured 200ms (likely no client timeout)", elapsed)
	}
}

func TestGitHubProvider_TimeoutOnSlowTokenEndpoint(t *testing.T) {
	hang := newHangServer(t, 1*time.Second)
	defer hang.Close()

	prov := NewGitHubProvider("client-id", "client-secret", "http://localhost/cb")
	prov.tokenEndpoint = hang.URL
	prov.userInfoEndpoint = hang.URL
	prov.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	start := time.Now()
	_, err := prov.ExchangeCode(context.Background(), "fake-code")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected error from hanging endpoint, got nil")
	}
	if elapsed > 1*time.Second {
		t.Fatalf("ExchangeCode took %v — must time out near the configured 200ms (likely no client timeout)", elapsed)
	}
}

// TestOAuthProviders_DefaultClientHasTimeout pins the default-construction
// invariant: a developer who calls NewGoogleProvider/NewGitHubProvider with
// no override gets a client with a non-zero Timeout. Without this, the
// "I forgot to configure a client" path hangs forever.
func TestOAuthProviders_DefaultClientHasTimeout(t *testing.T) {
	g := NewGoogleProvider("a", "b", "c")
	if g.httpClient == nil || g.httpClient.Timeout == 0 {
		t.Fatalf("GoogleProvider.httpClient must have a non-zero Timeout by default")
	}
	gh := NewGitHubProvider("a", "b", "c")
	if gh.httpClient == nil || gh.httpClient.Timeout == 0 {
		t.Fatalf("GitHubProvider.httpClient must have a non-zero Timeout by default")
	}
}


// GitHub returns email="" on /user when the primary email is hidden.
// We must fall back to /user/emails (the user:email scope is already
// requested) and pick the verified primary, NOT synthesize
// "<login>@github" (a non-routable address that breaks recovery).

func TestGitHubProvider_FallsBackToUserEmailsForHiddenPrimary(t *testing.T) {
	// Stub /user → empty email. Stub /user/emails → list with verified primary.
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         42,
			"login":      "alice",
			"email":      "",
			"name":       "Alice",
			"avatar_url": "https://example.com/a.png",
		})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "alice+other@example.com", "primary": false, "verified": true},
			{"email": "alice@example.com", "primary": true, "verified": true},
			{"email": "bogus@example.com", "primary": false, "verified": false},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prov := NewGitHubProvider("cid", "csec", "http://localhost/cb")
	prov.userInfoEndpoint = srv.URL + "/user"

	info, err := prov.FetchUserInfo(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "alice@example.com" {
		t.Fatalf("expected verified primary email alice@example.com, got %q", info.Email)
	}
	if info.Email == "alice@github" {
		t.Fatalf("must not synthesize %q — that's not a real email and breaks recovery", info.Email)
	}
}


// linkingUserStore is a stub UserStore that ALSO implements OAuthLinker.
// Tracks calls so tests can verify what the handler did.
type linkingUserStore struct {
	mu    sync.Mutex
	users map[string]User // by email
	byID  map[string]User // by ID
	links map[string]string // (provider+":"+providerID) -> userID
	nextID int

	createCalls int
	linkCalls   int
}

func newLinkingUserStore() *linkingUserStore {
	return &linkingUserStore{
		users: map[string]User{},
		byID:  map[string]User{},
		links: map[string]string{},
	}
}

func (s *linkingUserStore) FindByEmail(_ context.Context, email string) (User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[email]; ok {
		return u, "hash", nil
	}
	return nil, "", ErrUserNotFound
}

func (s *linkingUserStore) FindByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func (s *linkingUserStore) CreateUser(_ context.Context, email, _ string, roles []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	s.nextID++
	u := &BasicUser{ID: idFmt(s.nextID), Email: email, Roles: roles}
	s.users[email] = u
	s.byID[u.ID] = u
	return u, nil
}

func idFmt(n int) string { return "user-" + string(rune('0'+n)) }

func (s *linkingUserStore) FindByOAuth(_ context.Context, provider, providerID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, ok := s.links[provider+":"+providerID]
	if !ok {
		return nil, ErrUserNotFound
	}
	return s.byID[uid], nil
}

func (s *linkingUserStore) LinkOAuth(_ context.Context, userID, provider, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linkCalls++
	s.links[provider+":"+providerID] = userID
	return nil
}

// preExistingUser seeds the store with a user that already exists
// (e.g. created via password registration earlier).
func (s *linkingUserStore) preExistingUser(email string) User {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	u := &BasicUser{ID: idFmt(s.nextID), Email: email, Roles: []string{"user"}}
	s.users[email] = u
	s.byID[u.ID] = u
	return u
}

// runCallback issues a fully-formed callback request against r.
func runCallback(t *testing.T, mgr *AuthManager, r *router.Router, providerName string) *httptest.ResponseRecorder {
	t.Helper()
	plugin, _ := mgr.Plugin("oauth2")
	op := plugin.(*OAuth2Plugin)
	state, err := op.generateState(providerName)
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth/"+providerName+"/callback?state="+state+"&code=fakecode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Test A — same provider+id resolves to same local user even if the
// provider-side email changed between logins.
func TestOAuth_StableUserAcrossEmailChange(t *testing.T) {
	store := newLinkingUserStore()
	mgr := New(AuthConfig{
		SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store,
	})
	prov := &stubOAuthProvider{
		name:     "stub",
		userInfo: &OAuth2UserInfo{ID: "ext-1", Email: "first@example.com", Provider: "stub"},
	}
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"stub": prov},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// First login: creates user + link.
	w1 := runCallback(t, mgr, r, "stub")
	if w1.Code != http.StatusFound {
		t.Fatalf("first callback: expected 302, got %d (%s)", w1.Code, w1.Body.String())
	}
	if store.createCalls != 1 {
		t.Fatalf("expected 1 create on first login, got %d", store.createCalls)
	}
	if store.linkCalls != 1 {
		t.Fatalf("expected 1 link on first login, got %d", store.linkCalls)
	}

	// Provider-side email changes; same provider+id.
	prov.userInfo = &OAuth2UserInfo{ID: "ext-1", Email: "second@example.com", Provider: "stub"}

	w2 := runCallback(t, mgr, r, "stub")
	if w2.Code != http.StatusFound {
		t.Fatalf("second callback: expected 302, got %d", w2.Code)
	}
	if store.createCalls != 1 {
		t.Fatalf("must NOT create a new user when provider+id matches an existing link; got %d creates", store.createCalls)
	}
}

// Test B — local account exists with this email (created via password).
// An OAuth login with the same email but a NEW provider+id must not
// silently take over that account.
func TestOAuth_RefusesEmailCollisionWithExistingAccount(t *testing.T) {
	store := newLinkingUserStore()
	store.preExistingUser("victim@example.com")

	mgr := New(AuthConfig{
		SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store,
	})
	prov := &stubOAuthProvider{
		name:     "stub",
		userInfo: &OAuth2UserInfo{ID: "attacker-id", Email: "victim@example.com", Provider: "stub"},
	}
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"stub": prov},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	w := runCallback(t, mgr, r, "stub")

	// Must not be a successful login (302). Must not have linked.
	if w.Code == http.StatusFound {
		t.Fatalf("OAuth callback silently logged in as victim@example.com — account takeover possible. Status=302")
	}
	if store.linkCalls != 0 {
		t.Fatalf("must not link OAuth identity to a pre-existing email account; got %d link calls", store.linkCalls)
	}
	if store.createCalls != 0 {
		t.Fatalf("must not create a new user when email already taken; got %d create calls", store.createCalls)
	}
	// 409 is the conventional response for "email already in use, link from settings".
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d (body=%s)", w.Code, w.Body.String())
	}
}
