package auth

// Cross-flow token purpose binding.
//
// Property: a token minted by one auth flow must be REFUSED at every
// other flow's redemption endpoint — refused means 401 "invalid or
// expired token": not redeemed, not interpreted, not consumed.
//
// Surfaces. All three plugins share ONE MagicLinkTokenStore — the
// documented production wiring: PasswordResetConfig.TokenStore,
// EmailVerificationConfig.TokenStore and MagicLinkConfig.TokenStore
// all carry the same "set a durable store (e.g.
// NewSQLMagicLinkTokenStore(db)) in production" doc comment, so a
// single shared instance is the happy path the configs steer hosts
// into, not a misconfiguration.
//
//   - POST /auth/reset-password    (password-reset; token payload = userID)
//   - GET  /auth/verify-email      (email-verification; payload = userID)
//   - POST /auth/magic-link/verify (magic-link; payload = email)

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// tokenPurposeHarness wires one manager with all three token-minting
// plugins against a single shared token store, mirroring the documented
// production wiring, and drives the real HTTP endpoints.
type tokenPurposeHarness struct {
	t     *testing.T
	mgr   *AuthManager
	r     *router.Router
	store *userStoreWithPassword

	resetSender  *stubEmailSender // EmailSender (reset plugin)
	verifySender *stubEmailSender // EmailSender (verification plugin)
	magicSender  *mockEmailSender // MagicLinkEmailSender (magic-link plugin)

	victimEmail string
	victimID    string
}

func newTokenPurposeHarness(t *testing.T) *tokenPurposeHarness {
	t.Helper()
	return newTokenPurposeHarnessOn(t, newSQLStore(t))
}

// newTokenPurposeHarnessOn builds the same wiring on a caller-supplied
// store, so the cross-flow cases can run against a store that does NOT
// implement MagicLinkTokenPeeker. tokenStore is the ONE shared namespace
// the configs document.
func newTokenPurposeHarnessOn(t *testing.T, tokenStore MagicLinkTokenStore) *tokenPurposeHarness {
	t.Helper()
	store := newUserStoreWithPassword()
	h := &tokenPurposeHarness{
		t: t,
		mgr: New(AuthConfig{
			SessionTTL:    time.Hour,
			SessionCookie: "session_id",
			UserStore:     store,
			DevMode:       true,
		}),
		store:        store,
		resetSender:  &stubEmailSender{},
		verifySender: &stubEmailSender{},
		magicSender:  &mockEmailSender{},
		victimEmail:  "v@example.com",
		victimID:     "u-victim",
	}
	h.mgr.Use(NewCorePlugin())
	h.mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: h.resetSender,
		TokenStore:  tokenStore,
	}))
	h.mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:     "http://localhost",
		EmailSender: h.verifySender,
		TokenStore:  tokenStore,
	}))
	h.mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:     "http://localhost",
		EmailSender: h.magicSender,
		TokenStore:  tokenStore,
	}))
	if err := h.mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldHash, err := HashPassword("oldpw123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	entry := &storeEntry{
		user:        &BasicUser{ID: h.victimID, Email: h.victimEmail, Roles: []string{"user"}},
		hash:        oldHash,
		passwordSet: true,
	}
	store.users[h.victimEmail] = entry
	store.byID[h.victimID] = entry

	h.r = router.New()
	h.mgr.RegisterRoutes(h.r)
	return h
}

func (h *tokenPurposeHarness) postJSON(path string, payload any, cookie *http.Cookie) int {
	h.t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w.Code
}

// mintResetToken drives the real forgot-password endpoint and captures
// the emailed token.
func (h *tokenPurposeHarness) mintResetToken() string {
	h.t.Helper()
	if code := h.postJSON("/auth/forgot-password", map[string]string{"email": h.victimEmail}, nil); code != http.StatusOK {
		h.t.Fatalf("forgot-password: %d", code)
	}
	_, body := h.resetSender.snapshot()
	tok := extractTokenFromBody(body)
	if tok == "" {
		h.t.Fatalf("no token in reset email body: %q", body)
	}
	return tok
}

// mintVerificationToken drives the authenticated send-verification
// endpoint (the victim's own session) and captures the emailed token.
func (h *tokenPurposeHarness) mintVerificationToken() string {
	h.t.Helper()
	sess, err := h.mgr.SessionStore().Create(context.Background(), h.victimID, time.Hour)
	if err != nil {
		h.t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: "session_id", Value: sess.Token}
	if code := h.postJSON("/auth/send-verification", map[string]string{}, cookie); code != http.StatusOK {
		h.t.Fatalf("send-verification: %d", code)
	}
	_, body := h.verifySender.snapshot()
	tok := extractTokenFromBody(body)
	if tok == "" {
		h.t.Fatalf("no token in verification email body: %q", body)
	}
	return tok
}

// mintMagicLinkToken drives the real magic-link send endpoint.
func (h *tokenPurposeHarness) mintMagicLinkToken() string {
	h.t.Helper()
	if code := h.postJSON("/auth/magic-link/send", map[string]string{"email": h.victimEmail}, nil); code != http.StatusOK {
		h.t.Fatalf("magic-link/send: %d", code)
	}
	tok := extractTokenFromBody(h.magicSender.lastURL)
	if tok == "" {
		h.t.Fatalf("no token in magic link URL: %q", h.magicSender.lastURL)
	}
	return tok
}

func (h *tokenPurposeHarness) attemptReset(tok string) int {
	h.t.Helper()
	return h.postJSON("/auth/reset-password",
		map[string]string{"token": tok, "password": "attackerpw1"}, nil)
}

func (h *tokenPurposeHarness) attemptVerifyEmail(tok string) int {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+tok, nil)
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w.Code
}

func (h *tokenPurposeHarness) attemptMagicVerify(tok string) int {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify",
		strings.NewReader("token="+tok))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin") // pass the Fetch-Metadata gate
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w.Code
}

func (h *tokenPurposeHarness) passwordHash() string {
	h.t.Helper()
	_, hash, err := h.store.FindByEmail(context.Background(), h.victimEmail)
	if err != nil {
		h.t.Fatalf("FindByEmail: %v", err)
	}
	return hash
}

// Harness sanity (GREEN today, must stay green under any fix): with all
// three plugins sharing one token store, each flow still serves its OWN
// token. If this broke, a RED below would be a wiring artifact, not the
// vulnerability.
func TestSharedStoreStillServesOwnFlows(t *testing.T) {
	h := newTokenPurposeHarness(t)

	if code := h.attemptReset(h.mintResetToken()); code != http.StatusOK {
		t.Errorf("reset token at /auth/reset-password: got %d, want 200", code)
	}
	if code := h.attemptVerifyEmail(h.mintVerificationToken()); code != http.StatusOK {
		t.Errorf("verification token at /auth/verify-email: got %d, want 200", code)
	}
	if code := h.attemptMagicVerify(h.mintMagicLinkToken()); code != http.StatusFound {
		t.Errorf("magic-link token at /auth/magic-link/verify: got %d, want 302", code)
	}
}

// The sharpest edge of the shared namespace: an email-verification
// token (24h TTL, minted behind only the user's own session, delivered
// as a low-sensitivity GET link) must not drive a password change.
// resetHandler redeems ANY token from the shared store and trusts the
// payload as a userID, so the verification token's userID payload
// reaches SetPassword and the attacker holds a persistent credential
// for up to 24h — vs the 1h window the reset flow deliberately chose.
func TestVerificationTokenCannotResetPassword(t *testing.T) {
	h := newTokenPurposeHarness(t)
	before := h.passwordHash()

	code := h.attemptReset(h.mintVerificationToken())
	if code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] verification token at /auth/reset-password: got %d, want 401 (must not redeem as a password reset)", code)
	}
	if after := h.passwordHash(); after != before {
		t.Error("SECURITY: [token-purpose] verification token changed the victim's password hash")
	}
}

// A reset token must not be consumed by the other two endpoints either:
// at verify-email it marks the email verified, and at magic-link/verify
// it is redeemed, its userID payload is looked up as an email, misses,
// and auto-creates a junk account — burning the victim's reset link.
func TestResetTokenCannotDriveOtherFlows(t *testing.T) {
	h := newTokenPurposeHarness(t)

	if code := h.attemptVerifyEmail(h.mintResetToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] reset token at /auth/verify-email: got %d, want 401", code)
	}
	if h.store.verifiedIDs[h.victimID] {
		t.Error("SECURITY: [token-purpose] reset token marked the victim's email verified")
	}

	if code := h.attemptMagicVerify(h.mintResetToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] reset token at /auth/magic-link/verify: got %d, want 401", code)
	}
	if _, _, err := h.store.FindByEmail(context.Background(), h.victimID); err == nil {
		t.Error("SECURITY: [token-purpose] reset token auto-created an account keyed by the victim's userID as its email")
	}
}

// A magic-link login token (EMAIL payload) must be refused at the reset
// endpoint, not redeemed-and-misinterpreted: resetHandler calls
// SetPassword(email-as-userID), which 404s for random-hex user IDs but
// would take the password set on any host store whose IDs are emails.
// Refusal also means NOT consumed — the link must still sign its owner
// in at its own endpoint afterwards.
func TestMagicLinkTokenCannotResetPassword(t *testing.T) {
	h := newTokenPurposeHarness(t)
	tok := h.mintMagicLinkToken()

	if code := h.attemptReset(tok); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] magic-link token at /auth/reset-password: got %d, want 401 (redeemed, email payload used as a userID)", code)
	}
	if code := h.attemptMagicVerify(tok); code != http.StatusFound {
		t.Errorf("SECURITY: [token-purpose] magic-link token was consumed by the reset endpoint; its home endpoint then got %d, want 302", code)
	}
}
