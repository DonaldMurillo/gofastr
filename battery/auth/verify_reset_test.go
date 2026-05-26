package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// EmailVerification + PasswordReset plugins. Tests cover the happy paths
// and the security-critical "no enumeration" property of forgot-password.

// stubEmailSender records the most recent email sent. Reused for both
// verification and reset flows.
type stubEmailSender struct {
	mu       sync.Mutex
	lastTo   string
	lastBody string
}

func (s *stubEmailSender) Send(_ context.Context, to, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastTo = to
	s.lastBody = body
	return nil
}
func (s *stubEmailSender) snapshot() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTo, s.lastBody
}

// extractTokenFromBody pulls the token query-string value out of the
// "magic" URL the email-sender stub captured.
func extractTokenFromBody(body string) string {
	idx := strings.Index(body, "token=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("token="):]
	if amp := strings.IndexByte(rest, '&'); amp > 0 {
		return rest[:amp]
	}
	return rest
}

// userStoreWithPassword is a UserStore that ALSO implements
// PasswordSetter and EmailVerifier so the new plugins can persist
// changes during tests.
type userStoreWithPassword struct {
	*memoryUserStore
	verifiedIDs map[string]bool
}

func newUserStoreWithPassword() *userStoreWithPassword {
	return &userStoreWithPassword{
		memoryUserStore: newMemoryUserStore(),
		verifiedIDs:     map[string]bool{},
	}
}

func (s *userStoreWithPassword) SetPassword(_ context.Context, userID, hashedPassword string) error {
	if e, ok := s.byID[userID]; ok {
		e.hash = hashedPassword
		return nil
	}
	return ErrUserNotFound
}

func (s *userStoreWithPassword) MarkEmailVerified(_ context.Context, userID string) error {
	if _, ok := s.byID[userID]; !ok {
		return ErrUserNotFound
	}
	s.verifiedIDs[userID] = true
	return nil
}

// ---------- email verification --------------

func TestEmailVerification_Flow(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	plugin := NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: sender,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Seed user + login to get a session cookie.
	hash, _ := HashPassword("pwlong123")
	user := &BasicUser{ID: "u-1", Email: "u@example.com", Roles: []string{"user"}}
	store.users["u@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["u@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)

	// Login
	body, _ := json.Marshal(map[string]string{"email": "u@example.com", "password": "pwlong123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login: %d", loginW.Code)
	}
	var sessTok string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "session_id" {
			sessTok = c.Value
		}
	}

	// Send verification.
	sendReq := httptest.NewRequest(http.MethodPost, "/auth/send-verification", nil)
	sendReq.AddCookie(&http.Cookie{Name: "session_id", Value: sessTok})
	sendW := httptest.NewRecorder()
	r.ServeHTTP(sendW, sendReq)
	if sendW.Code != http.StatusOK {
		t.Fatalf("send-verification: %d", sendW.Code)
	}

	to, emailBody := sender.snapshot()
	if to != "u@example.com" {
		t.Fatalf("verification email recipient mismatch: %q", to)
	}
	tok := extractTokenFromBody(emailBody)
	if tok == "" {
		t.Fatalf("no token in email body: %q", emailBody)
	}

	// Verify.
	verifyReq := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+tok, nil)
	verifyW := httptest.NewRecorder()
	r.ServeHTTP(verifyW, verifyReq)
	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify-email: %d (body=%s)", verifyW.Code, verifyW.Body.String())
	}

	if !store.verifiedIDs[user.ID] {
		t.Fatalf("user must be marked verified after token consume")
	}
}

// ---------- password reset --------------

func TestPasswordReset_Flow(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	plugin := NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: sender,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldHash, _ := HashPassword("oldpw")
	user := &BasicUser{ID: "u-2", Email: "r@example.com", Roles: []string{"user"}}
	store.users["r@example.com"] = &storeEntry{user: user, hash: oldHash}
	store.byID[user.ID] = store.users["r@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)

	// Forgot.
	body, _ := json.Marshal(map[string]string{"email": "r@example.com"})
	forgotReq := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	forgotReq.Header.Set("Content-Type", "application/json")
	forgotW := httptest.NewRecorder()
	r.ServeHTTP(forgotW, forgotReq)
	if forgotW.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", forgotW.Code)
	}

	to, emailBody := sender.snapshot()
	if to != "r@example.com" {
		t.Fatalf("reset email recipient mismatch: %q", to)
	}
	tok := extractTokenFromBody(emailBody)
	if tok == "" {
		t.Fatalf("no token in body")
	}

	// Reset.
	resetBody, _ := json.Marshal(map[string]string{"token": tok, "password": "newpw123"})
	resetReq := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(resetBody))
	resetReq.Header.Set("Content-Type", "application/json")
	resetW := httptest.NewRecorder()
	r.ServeHTTP(resetW, resetReq)
	if resetW.Code != http.StatusOK {
		t.Fatalf("reset-password: %d (body=%s)", resetW.Code, resetW.Body.String())
	}

	if !CheckPassword("newpw123", store.byID[user.ID].hash) {
		t.Fatalf("password not updated")
	}
	if CheckPassword("oldpw", store.byID[user.ID].hash) {
		t.Fatalf("old password still works")
	}
}

func TestPasswordReset_ForgotDoesNotEnumerate(t *testing.T) {
	// Forgot-password must return 200 whether the email exists or not.
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	sender := &stubEmailSender{}
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: sender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("forgot-password for unknown email must return 200 (no enumeration); got %d", w.Code)
	}
	to, _ := sender.snapshot()
	if to != "" {
		t.Fatalf("must not send email to unknown address; sent to %q", to)
	}
}
