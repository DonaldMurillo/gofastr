package auth

// Default per-IP throttles on the brute-force- and flood-sensitive auth
// endpoints.
//
// Property: every email-dispatch, secret-verification, and code-guessing
// endpoint ships a DEFAULT per-IP throttle when its config is left at
// zero, the same opt-out posture AuthConfig.defaults gives login (30/min
// per IP) and register (10/min). The floors: forgot/reset/magic-send/
// send-verification at 10/min with a 15-minute block (the register
// floor, they all mint tokens and dispatch mail), magic-link verify at
// 30/min with a 5-minute block (the login per-IP floor, it redeems a
// secret and mints a session), and 2FA challenge/verify at 10/min with
// a 15-minute block (6-digit code guessing needs ~333k expected attempts
// at skew=1). Loosening is explicit: pass a RateLimiterConfig with a
// large MaxAttempts.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// setupDefaultResetRouter mounts core + password-reset with config left
// at its defaults, the posture an app that never read the plugin's
// RateLimit doc ships with.
func setupDefaultResetRouter(t *testing.T) (*userStoreWithPassword, *router.Router) {
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

// 100 rapid forgot-password requests from one IP must hit a 429: every
// known-email request mints a reset token and dispatches an email, an
// unauthenticated mail-bomb and token-flood primitive.
func TestResetRequestDefaultThrottle(t *testing.T) {
	_, r := setupDefaultResetRouter(t)

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

// The secret-checking half of the reset flow carries the same floor.
func TestResetConfirmDefaultThrottle(t *testing.T) {
	_, r := setupDefaultResetRouter(t)

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

// setupDefaultMagicRouter mounts core + magic-link with config left at
// its defaults, DevMode on so send answers 200 without a sender.
func setupDefaultMagicRouter(t *testing.T) *router.Router {
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

// Magic-link send is an unauthenticated mail primitive: the default
// floor must apply with no config.
func TestMagicLinkSendDefaultThrottle(t *testing.T) {
	r := setupDefaultMagicRouter(t)

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

// Magic-link verify redeems a secret and mints a session: a bound must
// exist by default (and the VerifyRateLimit knob must be able to shape
// it), mirroring the login floor.
func TestMagicLinkVerifyDefaultThrottle(t *testing.T) {
	r := setupDefaultMagicRouter(t)

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
		t.Errorf("100 rapid wrong-token magic-link verify attempts from one IP were never throttled (0 of 100 responses were 429); token redemption needs a default per-IP rate limit and a knob (MagicLinkConfig.VerifyRateLimit) to shape it")
	}
}

// Send-verification is session-gated but still a token-minting mail
// surface: the default floor bounds one compromised session's send
// volume.
func TestVerifyEmailSendDefaultThrottle(t *testing.T) {
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

// TestTwoFADefaultChallengeThrottle: the 2FA challenge endpoint carries
// a default per-IP floor — a stolen session brute-forcing the 6-digit
// TOTP must not get unlimited guesses.
func TestTwoFADefaultChallengeThrottle(t *testing.T) {
	_, twofa, r := setupP17(t)
	tok := loginP17(t, r)

	// A syntactically valid 6-digit code from step 0 (1970): always outside
	// the ±skew window, so every attempt fails validation deterministically.
	state, err := twofa.store.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("seeded 2FA state missing: state=%v err=%v", state, err)
	}
	wrongCode := GenerateTOTP(state.Secret, 0)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"code": wrongCode})
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized, http.StatusBadRequest:
			// wrong code refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 wrong 6-digit codes from one IP against /auth/2fa/challenge were never throttled (0 of 100 responses were 429); the 2FA endpoints need a default per-IP rate limit like login/register")
	}
}

// TestTwoFAVerifyDefaultThrottle: the /2fa/verify half of the floor — a
// stolen session of an account mid-enrollment must not get unlimited
// guesses at the 6-digit code that would enable 2FA on the account.
func TestTwoFAVerifyDefaultThrottle(t *testing.T) {
	twofa, r, session := setupTwoFAEnrollment(t)

	// A syntactically valid 6-digit code from step 0 (1970): always outside
	// the ±skew window, so every attempt fails validation deterministically.
	// The enrollment is pending (Enabled=false) so verifyHandler reaches the
	// TOTP check, not an early "already enabled" / auth refusal.
	state, err := twofa.store.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || state.Enabled {
		t.Fatalf("seeded pending 2FA enrollment missing: state=%v err=%v", state, err)
	}
	wrongCode := GenerateTOTP(state.Secret, 0)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"code": wrongCode})
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized, http.StatusBadRequest:
			// wrong code refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 wrong 6-digit codes from one IP against /auth/2fa/verify were never throttled (0 of 100 responses were 429); the endpoint needs the same default per-IP rate limit as login/register")
	}
}
