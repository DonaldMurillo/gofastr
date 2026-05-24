package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestE2E_HappyPath_FullAuthLifecycle drives every plugin through real
// HTTP against an httptest.Server with a cookie jar — the closest thing
// to a live deployment without running a binary. Order matters: the 2FA
// gate, OAuth linking, and password reset interact through session
// state, and the test fails loudly if any plugin breaks the others.
//
// Flow:
//
//	1.  register (CorePlugin)                          → 201 + user
//	2.  login                                          → 200 + session
//	3.  send-verification (EmailVerificationPlugin)    → 200 (dev mode)
//	4.  verify-email                                   → 200, verified=true
//	5.  enroll 2FA (TwoFAPlugin)                       → 200 + secret
//	6.  verify enrollment with valid TOTP              → 200 + backup_codes
//	7.  logout                                         → 204
//	8.  re-login                                       → 200, but session is PendingTwoFactor
//	9.  /auth/me with pending session                  → 403 (not authenticated yet)
//	10. /auth/2fa/challenge with valid TOTP            → 200, session promoted
//	11. /auth/me                                       → 200 (now fully authenticated)
//	12. simulate OAuth link via store.LinkOAuth        → (no HTTP endpoint for in-session link today)
//	13. GET /auth/accounts                             → lists google
//	14. DELETE /auth/unlink/google                     → 200 (user has password)
//	15. POST /auth/forgot-password                     → 200 (dev mode)
//	16. POST /auth/reset-password                      → 200, password updated
//	17. login with NEW password                        → 200 + session (still 2FA-pending)
//	18. challenge again                                → 200 + me works

// e2eFullStore implements UserStore plus every optional extension the
// plugins above need: PasswordSetter, EmailVerifier, AccountLister,
// AccountUnlinker, OAuthLinker, PasswordChecker, OAuthUserCreator.
type e2eFullStore struct {
	mu         sync.Mutex
	byEmail    map[string]*e2eFullEntry
	byID       map[string]*e2eFullEntry
	verified   map[string]bool
	devURLs    chan string
	links      map[string]map[string]string // userID → provider → providerID
	devTokens  *capturedTokens
}

type e2eFullEntry struct {
	user        User
	hash        string
	passwordSet bool
}

func newE2EFullStore(devCapture *capturedTokens) *e2eFullStore {
	return &e2eFullStore{
		byEmail:   map[string]*e2eFullEntry{},
		byID:      map[string]*e2eFullEntry{},
		verified:  map[string]bool{},
		links:     map[string]map[string]string{},
		devTokens: devCapture,
	}
}

func (s *e2eFullStore) FindByEmail(_ context.Context, email string) (User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byEmail[email]
	if !ok {
		return nil, "", ErrUserNotFound
	}
	return e.user, e.hash, nil
}
func (s *e2eFullStore) FindByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return e.user, nil
}
func (s *e2eFullStore) CreateUser(_ context.Context, email, hashedPassword string, roles []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[email]; exists {
		return nil, ErrEmailTaken
	}
	id := "u-" + email
	u := &BasicUser{ID: id, Email: email, Roles: roles}
	entry := &e2eFullEntry{user: u, hash: hashedPassword, passwordSet: true}
	s.byEmail[email] = entry
	s.byID[id] = entry
	return u, nil
}
func (s *e2eFullStore) CreateUserNoPassword(_ context.Context, email string, roles []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[email]; exists {
		return nil, ErrEmailTaken
	}
	id := "u-" + email
	u := &BasicUser{ID: id, Email: email, Roles: roles}
	entry := &e2eFullEntry{user: u, hash: passwordPlaceholderHash, passwordSet: false}
	s.byEmail[email] = entry
	s.byID[id] = entry
	return u, nil
}
func (s *e2eFullStore) HasPassword(_ context.Context, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[userID]; ok {
		return e.passwordSet, nil
	}
	return false, ErrUserNotFound
}
func (s *e2eFullStore) SetPassword(_ context.Context, userID, hashedPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	e.hash = hashedPassword
	e.passwordSet = true
	return nil
}
func (s *e2eFullStore) MarkEmailVerified(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[userID]; !ok {
		return ErrUserNotFound
	}
	s.verified[userID] = true
	return nil
}
func (s *e2eFullStore) FindByOAuth(_ context.Context, provider, providerID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for uid, m := range s.links {
		if m[provider] == providerID {
			return s.byID[uid].user, nil
		}
	}
	return nil, ErrUserNotFound
}
func (s *e2eFullStore) LinkOAuth(_ context.Context, userID, provider, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.links[userID] == nil {
		s.links[userID] = map[string]string{}
	}
	s.links[userID][provider] = providerID
	return nil
}
func (s *e2eFullStore) ListAccounts(_ context.Context, userID string) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Account
	for prov, pid := range s.links[userID] {
		out = append(out, Account{Provider: prov, ProviderID: pid})
	}
	return out, nil
}
func (s *e2eFullStore) UnlinkOAuth(_ context.Context, userID, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.links[userID]; m != nil {
		delete(m, provider)
	}
	return nil
}

// capturedTokens is the dev-mode sink for verification + reset URLs.
// EmailVerificationPlugin and PasswordResetPlugin log to stdout in dev,
// but for an E2E we need to read them programmatically. We capture
// them via a fake EmailSender that the plugins use instead of dev-mode
// logging.
type capturedTokens struct {
	mu   sync.Mutex
	urls map[string]string // email → most-recent URL
}

func newCapturedTokens() *capturedTokens {
	return &capturedTokens{urls: map[string]string{}}
}

func (c *capturedTokens) Send(_ context.Context, to, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.urls[to] = body
	return nil
}
func (c *capturedTokens) urlFor(email string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.urls[email]
}
func (c *capturedTokens) tokenFor(email string) string {
	u, err := url.Parse(c.urlFor(email))
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}

func TestE2E_HappyPath_FullAuthLifecycle(t *testing.T) {
	emails := newCapturedTokens()
	resets := newCapturedTokens()
	store := newE2EFullStore(emails)

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true, // HTTP-friendly cookies
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:     "http://test",
		EmailSender: emails, // captures URL
	}))
	twofa := NewTwoFAPlugin(TwoFAConfig{Issuer: "GoFastr-E2E"})
	mgr.Use(twofa)
	mgr.Use(NewAccountsPlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://test",
		EmailSender: resets,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth Init: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		// 30s — generous because bcrypt + 2FA run ~3x slower under
		// `go test -race`, exceeding the previous 5s cap and producing
		// false-positive timeouts.
		Timeout: 30 * time.Second,
	}

	do := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		return resp.StatusCode, parsed
	}

	// 1. Register
	code, body := do(http.MethodPost, "/auth/register", map[string]string{
		"email":    "alice@e2e.test",
		"password": "starting-password",
	})
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, body)
	}
	userObj, _ := body["user"].(map[string]any)
	userID, _ := userObj["id"].(string)
	if userID == "" {
		t.Fatalf("register: missing user.id in %v", body)
	}

	// 2. Login
	code, _ = do(http.MethodPost, "/auth/login", map[string]string{
		"email":    "alice@e2e.test",
		"password": "starting-password",
	})
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}

	// 3. Send-verification (dev mode via fake EmailSender)
	code, _ = do(http.MethodPost, "/auth/send-verification", nil)
	if code != http.StatusOK {
		t.Fatalf("send-verification: %d", code)
	}
	verifyToken := emails.tokenFor("alice@e2e.test")
	if verifyToken == "" {
		t.Fatalf("no verification URL captured: %#v", emails.urls)
	}

	// 4. Verify-email
	code, _ = do(http.MethodGet, "/auth/verify-email?token="+url.QueryEscape(verifyToken), nil)
	if code != http.StatusOK {
		t.Fatalf("verify-email: %d", code)
	}
	store.mu.Lock()
	if !store.verified[userID] {
		store.mu.Unlock()
		t.Fatal("verified flag not set after /auth/verify-email")
	}
	store.mu.Unlock()

	// 5. Enroll 2FA
	code, body = do(http.MethodPost, "/auth/2fa/enroll", nil)
	if code != http.StatusOK {
		t.Fatalf("2fa/enroll: %d %v", code, body)
	}
	secret, _ := body["secret"].(string)
	if secret == "" {
		t.Fatalf("2fa/enroll missing secret: %v", body)
	}

	// 6. Verify enrollment
	totp := GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	code, body = do(http.MethodPost, "/auth/2fa/verify", map[string]string{"code": totp})
	if code != http.StatusOK {
		t.Fatalf("2fa/verify: %d %v", code, body)
	}
	if enabled, _ := body["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled=true: %v", body)
	}

	// 7. Logout (clears jar's session cookie via Set-Cookie expiry)
	code, _ = do(http.MethodPost, "/auth/logout", nil)
	if code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("logout: %d", code)
	}
	// jar may have stale cookies past expiry — clear explicitly.
	jar.SetCookies(mustParseURL(srv.URL), nil)

	// 8. Re-login → pending session
	code, _ = do(http.MethodPost, "/auth/login", map[string]string{
		"email":    "alice@e2e.test",
		"password": "starting-password",
	})
	if code != http.StatusOK {
		t.Fatalf("re-login: %d", code)
	}

	// 9. /auth/me with pending session → 403
	code, _ = do(http.MethodGet, "/auth/me", nil)
	if code != http.StatusForbidden {
		t.Fatalf("/auth/me after pending login should be 403; got %d", code)
	}

	// 10. 2FA challenge
	totp = GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	code, body = do(http.MethodPost, "/auth/2fa/challenge", map[string]string{"code": totp})
	if code != http.StatusOK {
		t.Fatalf("2fa/challenge: %d %v", code, body)
	}

	// 11. /auth/me works now
	code, body = do(http.MethodGet, "/auth/me", nil)
	if code != http.StatusOK {
		t.Fatalf("/auth/me post-challenge: %d %v", code, body)
	}

	// 12. Simulate OAuth link directly on the store — there is no
	// in-session link endpoint today; AccountsPlugin owns list+unlink.
	if err := store.LinkOAuth(context.Background(), userID, "google", "g-12345"); err != nil {
		t.Fatalf("LinkOAuth seed: %v", err)
	}

	// 13. List accounts
	code, body = do(http.MethodGet, "/auth/accounts", nil)
	if code != http.StatusOK {
		t.Fatalf("/auth/accounts: %d %v", code, body)
	}
	accts, _ := body["accounts"].([]any)
	if len(accts) != 1 {
		t.Fatalf("expected 1 linked account; got %v", body)
	}

	// 14. Unlink — user has a password so this must succeed.
	code, _ = do(http.MethodDelete, "/auth/unlink/google", nil)
	if code != http.StatusOK {
		t.Fatalf("/auth/unlink/google for password user: %d", code)
	}

	// 15. Forgot password (logged in — handler doesn't require auth)
	code, _ = do(http.MethodPost, "/auth/forgot-password", map[string]string{
		"email": "alice@e2e.test",
	})
	if code != http.StatusOK {
		t.Fatalf("/auth/forgot-password: %d", code)
	}
	resetToken := resets.tokenFor("alice@e2e.test")
	if resetToken == "" {
		t.Fatalf("no reset URL captured: %#v", resets.urls)
	}

	// 16. Reset password
	code, _ = do(http.MethodPost, "/auth/reset-password", map[string]string{
		"token":    resetToken,
		"password": "new-shiny-password",
	})
	if code != http.StatusOK {
		t.Fatalf("/auth/reset-password: %d", code)
	}

	// 17. Login with the new password (clear any lingering cookies first)
	jar.SetCookies(mustParseURL(srv.URL), nil)
	code, _ = do(http.MethodPost, "/auth/login", map[string]string{
		"email":    "alice@e2e.test",
		"password": "new-shiny-password",
	})
	if code != http.StatusOK {
		t.Fatalf("login after reset: %d", code)
	}

	// 18. Challenge + me, end-to-end shape preserved
	totp = GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	code, _ = do(http.MethodPost, "/auth/2fa/challenge", map[string]string{"code": totp})
	if code != http.StatusOK {
		t.Fatalf("post-reset 2fa challenge: %d", code)
	}
	code, _ = do(http.MethodGet, "/auth/me", nil)
	if code != http.StatusOK {
		t.Fatalf("post-reset /auth/me: %d", code)
	}

	// Final sanity: the OLD password must not work.
	jar.SetCookies(mustParseURL(srv.URL), nil)
	code, _ = do(http.MethodPost, "/auth/login", map[string]string{
		"email":    "alice@e2e.test",
		"password": "starting-password",
	})
	if code == http.StatusOK {
		t.Fatal("old password must not authenticate after reset")
	}
}

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(fmt.Sprintf("parse: %v", err))
	}
	return u
}

// noinspection GoUnusedFunction — kept available for follow-up tests.
var _ = strings.Contains
