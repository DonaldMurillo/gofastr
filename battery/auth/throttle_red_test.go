//go:build red

// RED TESTS — open findings, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Family: brute-force- and flood-sensitive auth endpoints ship a default
// per-IP throttle, like login/register do. AuthConfig.defaults installs
// LoginRateLimit (30/min), LoginRateLimitPerAccount (10/min) and
// RegisterRateLimit (10/min) unconditionally (manager.go:157-177), and
// RegisterRateLimit's own doc names unthrottled email-sending an
// "email-bombing primitive". The 2FA default-throttle gap is already pinned
// (twofa_red_test.go TestTwoFARedDefaultChallengeThrottle,
// TestTwoFAVerifyRedDefaultThrottle); these reds cover the four sibling
// surfaces that still treat their RateLimit config as opt-in.

package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// setupRedResetRouter mounts core + password-reset with config left at its
// defaults (no RateLimit), the posture an app that never read the plugin's
// RateLimit doc ships with.
func setupRedResetRouter(t *testing.T) (*userStoreWithPassword, *router.Router) {
	t.Helper()
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL: "http://localhost",
		DevMode: true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	store.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["alice@example.com"]
	return store, mountRoutes(mgr)
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: unauthenticated email-dispatch endpoints ship a default per-IP
// throttle, like register does (AuthConfig.defaults, manager.go:171-177).
// Surfaces: password_reset.go:NewPasswordResetPlugin:89-91 (limiter built
// only `if cfg.RateLimit != nil`), password_reset.go:forgotHandler:124-126
// (`if p.limit != nil` guard skips the throttle entirely by default).
// Finding: with config left at defaults, 100 rapid POST /auth/forgot-password
// from one IP all get plain 200s — never a 429. Every known-email request
// mints a reset token and dispatches an email: an unauthenticated mail-bomb
// and token-flood primitive, the exact shape RegisterRateLimit's 10/min
// default exists to prevent.
// Severity: P2 — unauthenticated, unbounded token issuance + email dispatch
// by default.
// Fix direction: install a default per-IP RateLimit in NewPasswordResetPlugin
// (opt-out like AuthConfig.defaults' login/register floors, not the current
// opt-in), shared by both handlers.
func TestResetRequestRedDefaultThrottle(t *testing.T) {
	_, r := setupRedResetRouter(t)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusOK:
			// anti-enumeration 200, as designed
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 rapid forgot-password requests from one IP were never throttled (0 of 100 responses were 429); the endpoint needs a default per-IP rate limit like login/register")
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: token-redemption endpoints (the secret-checking surface) ship a
// default per-IP throttle, like login does.
// Surfaces: password_reset.go:NewPasswordResetPlugin:89-91 (opt-in-only
// limiter), password_reset.go:resetHandler:235-237 (`if p.limit != nil`
// guard; the throttle is the first thing the handler does).
// Finding: with config left at defaults, 100 rapid wrong-token POSTs to
// /auth/reset-password all get plain 401s — never a 429. Honest materiality
// note: reset tokens are 256-bit random (magiclink.go:145-149), so blind
// guessing is not practically viable; what is missing is the default floor
// every other secret-verification endpoint in this package is held to (the
// pinned 2FA reds pin the same property for 6-digit codes, where it is
// directly exploitable).
// Severity: P3 — default-posture gap on a high-entropy token surface; the
// assertion pins the missing floor, not a practical brute-force.
// Fix direction: same default per-IP RateLimit as the forgot endpoint above
// (one limiter, both handlers).
func TestResetConfirmRedDefaultThrottle(t *testing.T) {
	_, r := setupRedResetRouter(t)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{
			"token":    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			"password": "newpassword123",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized:
			// wrong token refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 rapid wrong-token reset-password attempts from one IP were never throttled (0 of 100 responses were 429); the token-redemption endpoint needs a default per-IP rate limit like login/register")
	}
}

// setupRedMagicRouter mounts core + magic-link with config left at its
// defaults (no RateLimit), DevMode on so send answers 200 without a sender.
func setupRedMagicRouter(t *testing.T) *router.Router {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           newMemoryUserStore(),
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL: "http://localhost",
		DevMode: true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mountRoutes(mgr)
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: unauthenticated email-dispatch endpoints ship a default per-IP
// throttle, like register does.
// Surfaces: magiclink.go:NewMagicLinkPlugin:246-248 (sendLimit built only
// `if config.RateLimit != nil`), magiclink.go:sendHandler:275-277
// (`if p.sendLimit != nil` guard).
// Finding: with config left at defaults, 100 rapid POST /auth/magic-link/send
// from one IP all get plain 200s — never a 429. Each request mints a token
// and dispatches a magic-link email to any submitted address: the same
// unauthenticated mail-bomb primitive RegisterRateLimit's default closes.
// Severity: P2 — unauthenticated, unbounded email dispatch by default.
// Fix direction: install a default sendLimit in NewMagicLinkPlugin (opt-out
// like AuthConfig.defaults' floors, not the current opt-in).
func TestMagicLinkSendRedDefaultThrottle(t *testing.T) {
	r := setupRedMagicRouter(t)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusOK:
			// link minted + dispatched (DevMode), as designed
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 rapid magic-link send requests from one IP were never throttled (0 of 100 responses were 429); the endpoint needs a default per-IP rate limit like login/register")
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: an endpoint that redeems a secret credential and mints a session
// has SOME per-IP bound — a default like login's, or at least an opt-in that
// can be wired.
// Surfaces: magiclink.go:MagicLinkPlugin (carries only sendLimit; there is no
// verifyLimit field at all), magiclink.go:RegisterRoutes:262-270 (verify
// mounted bare), magiclink.go:verifyHandler:489+ (no limiter reference —
// unlike sendHandler, not even an `if limit != nil` guard exists).
// Finding: POST /auth/magic-link/verify has no throttle by construction: no
// default is installed AND no configuration knob exists to add one. 100 rapid
// wrong-token POSTs all get plain 401s. Honest materiality note: the tokens
// are 256-bit random (magiclink.go:145-149), so guessing is not the practical
// attack; this red demands the bound the package's own discipline gives every
// other secret-verification surface (login, 2FA challenge/verify per the
// pinned reds), so an operator can shape redemption traffic at all.
// Severity: P3 — high-entropy surface, but the property ("some bound exists")
// is currently unsatisfiable by configuration.
// Fix direction: add a verifyLimit to MagicLinkPlugin with a default per-IP
// config (and expose it on MagicLinkConfig.RateLimit for both handlers or a
// dedicated field), mirroring the login floor.
func TestMagicLinkVerifyRedNoLimiter(t *testing.T) {
	r := setupRedMagicRouter(t)

	throttled := 0
	for range 100 {
		req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify?token=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized:
			// wrong token refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 rapid wrong-token magic-link verify attempts from one IP were never throttled (0 of 100 responses were 429); token redemption has no limiter at all — neither a default nor a config knob to add one")
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: email-dispatch endpoints ship a default per-IP throttle, like
// register does.
// Surfaces: email_verification.go:NewEmailVerificationPlugin:81-83 (limiter
// built only `if cfg.RateLimit != nil`), email_verification.go:sendHandler:100-102
// (`if p.limit != nil` guard).
// Finding: with config left at defaults, 100 rapid POST /auth/send-verification
// from one session all get plain 200s — never a 429. Each request mints a
// 24h verification token and dispatches an email.
// Severity: P3 — lower than the unauthenticated siblings above on purpose:
// the endpoint requires an authenticated, non-pending-2FA session, so the
// mail-bomb primitive needs a logged-in session and the trail carries the
// user id. The missing default floor still lets one compromised session
// mail-bomb arbitrary addresses (the recipient is the stored email, but the
// send volume is unbounded).
// Fix direction: install a default per-IP (or per-session) RateLimit in
// NewEmailVerificationPlugin, mirroring the opt-out floors.
func TestVerifyEmailSendRedDefaultThrottle(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL: "http://localhost",
		DevMode: true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	store.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["alice@example.com"]
	r := mountRoutes(mgr)
	session := loginP17(t, r)

	throttled := 0
	for range 100 {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-verification", nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusOK:
			// verification email dispatched (DevMode), as designed
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 rapid send-verification requests from one session were never throttled (0 of 100 responses were 429); the endpoint needs a default rate limit like login/register")
	}
}
