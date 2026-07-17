package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Hammer an endpoint from a single IP and assert 429 takes over after the
// configured threshold. The endpoint's own status (200/401) doesn't matter
// — what matters is that beyond MaxAttempts the limiter short-circuits.

func hammer(t *testing.T, r *router.Router, method, path string, body []byte, ip string, want429AfterN int) {
	t.Helper()
	for i := 0; i < want429AfterN+5; i++ {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if i < want429AfterN {
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d: rate limit fired too early (want first %d to pass through)", i+1, want429AfterN)
			}
			continue
		}
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: expected 429, got %d (body=%s)", i+1, w.Code, w.Body.String())
		}
		// Retry-After header should be set
		if ra := w.Header().Get("Retry-After"); ra == "" {
			t.Errorf("attempt %d: expected Retry-After header on 429", i+1)
		}
		return
	}
	t.Fatal("never saw 429")
}

func TestRateLimit_Login(t *testing.T) {
	mgr := newRLAuthManager(t, RateLimiterConfig{MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Minute})
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "x@y.com", "password": "wrong"})
	hammer(t, r, http.MethodPost, "/auth/login", body, "1.2.3.4", 3)
}

func TestRateLimit_MagicLinkSend(t *testing.T) {
	mgr := newRLAuthManager(t, RateLimiterConfig{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute})
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "x@y.com"})
	hammer(t, r, http.MethodPost, "/auth/magic-link/send", body, "5.6.7.8", 2)
}

func TestRateLimit_TwoFAChallenge(t *testing.T) {
	mgr := newRLAuthManager(t, RateLimiterConfig{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute})
	r := router.New()
	mgr.RegisterRoutes(r)

	// Need an authenticated session for /2fa/challenge.
	sess, err := mgr.SessionStore().Create(t.Context(), "user-x", time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	body := []byte(`{"code":"000000"}`)
	for i := 0; i < 2+5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "9.9.9.9:1234"
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if i < 2 {
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d: rate limit fired too early", i+1)
			}
			continue
		}
		if w.Code == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("never saw 429 on /2fa/challenge")
}

// newRLAuthManager builds an AuthManager with all plugins and the given
// rate-limit config applied to the relevant endpoints.
func newRLAuthManager(t *testing.T, rlCfg RateLimiterConfig) *AuthManager {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           newMemoryUserStore(),
		LoginRateLimit:      &rlCfg,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:   "http://localhost",
		TokenTTL:  time.Minute,
		RateLimit: &rlCfg,
	}))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{RateLimit: &rlCfg}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr
}

// Compile-time check: all wiring fields exist.
var _ = fmt.Sprintf

// X-Forwarded-For must NOT be trusted by default. Today an attacker
// rotates the header per request and bypasses every per-IP rate limit.
// The fix: clientIP() ignores XFF unless RateLimiter is configured to
// trust it.

func TestRateLimit_IgnoresUnconfiguredXFF(t *testing.T) {
	// MaxAttempts=2 with default config (TrustForwardedFor=false).
	mgr := newRLAuthManager(t, RateLimiterConfig{
		MaxAttempts:   2,
		Window:        time.Minute,
		BlockDuration: time.Minute,
	})
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "x@y.com", "password": "wrong"})

	// All requests come from the SAME real IP but rotate XFF.
	// With XFF trusted, each looks like a different IP and rate-limit
	// never fires. With XFF ignored, the real IP is the key and we
	// hit 429 on the third attempt.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		req.RemoteAddr = "5.6.7.8:9999"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if i >= 2 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: with XFF spoofing and untrusted XFF default, expected 429, got %d (body=%s)",
				i+1, w.Code, w.Body.String())
		}
	}
}

// TestRateLimit_PerAccount_BlocksIPRotationAttack pins the per-account
// limiter: an attacker who pivots IPs (so per-IP rate limit doesn't
// trip) and pounds a single account email must still be blocked by the
// per-account key after MaxAttempts. Without this limiter, the
// per-IP-only posture is bypassable by any botnet.
func TestRateLimit_PerAccount_BlocksIPRotationAttack(t *testing.T) {
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           newMemoryUserStore(),
		// Generous per-IP limit so it never fires in this test.
		LoginRateLimit: &RateLimiterConfig{MaxAttempts: 1000, Window: time.Minute, BlockDuration: time.Minute},
		LoginRateLimitPerAccount: &RateLimiterConfig{
			MaxAttempts:   3,
			Window:        time.Minute,
			BlockDuration: time.Minute,
		},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "victim@example.com", "password": "wrong"})

	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// Different RemoteAddr each call — simulates IP rotation.
		req.RemoteAddr = fmt.Sprintf("10.0.0.%d:9999", i+1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Attempts 1..3 should be 401 (wrong password). The 4th — the
		// MaxAttempts+1 — must be 429 from the per-account limiter.
		if i >= 3 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d to victim@example.com from rotating IP must 429; got %d (body=%s)",
				i+1, w.Code, w.Body.String())
		}
	}
}

// TestRateLimit_PerAccount_DifferentEmailsNotBlocked pins the scoping:
// hammering one email must not penalise a different email — the keys
// are independent. Otherwise an attacker could DoS legitimate users by
// burning through the per-account budget on their own account.
func TestRateLimit_PerAccount_DifferentEmailsNotBlocked(t *testing.T) {
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           newMemoryUserStore(),
		LoginRateLimitPerAccount: &RateLimiterConfig{
			MaxAttempts:   2,
			Window:        time.Minute,
			BlockDuration: time.Minute,
		},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// Burn the budget for victim@example.com.
	bodyVictim, _ := json.Marshal(map[string]string{"email": "victim@example.com", "password": "wrong"})
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(bodyVictim))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "1.2.3.4:9999"
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	// Different email — must not be blocked.
	bodyOther, _ := json.Marshal(map[string]string{"email": "other@example.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(bodyOther))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:9999"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("per-account limiter must scope by email — other@example.com got 429 after victim@ was burned")
	}
}

func TestRateLimit_TrustForwardedFor_OptIn(t *testing.T) {
	// When the operator explicitly opts in (TrustForwardedFor=true),
	// XFF is honoured and rotating it bypasses the per-IP limiter as
	// before — this is the legitimate case (you sit behind a trusted
	// proxy). Pin the opt-in semantics.
	cfg := RateLimiterConfig{
		MaxAttempts:       2,
		Window:            time.Minute,
		BlockDuration:     time.Minute,
		TrustForwardedFor: true,
	}
	mgr := newRLAuthManager(t, cfg)
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "x@y.com", "password": "wrong"})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		req.RemoteAddr = "5.6.7.8:9999"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Each XFF is unique so we should never hit the limiter.
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d: with TrustForwardedFor and rotating XFF, must NOT 429; got 429", i+1)
		}
	}
}

// TestRateLimit_DevModeRelaxesPerIPLogin pins issue #71: local
// screenshot / verification tooling that hammers /auth/login from one
// IP (localhost) must not trip the per-IP login limiter when the app
// runs in DevMode. Production (DevMode=false) keeps the limiter
// fail-closed — see TestRateLimit_Login. The per-ACCOUNT limiter is
// deliberately NOT relaxed in DevMode: it guards brute-force even in
// dev, pinned by TestAuthBypass_BruteForceNoLockout.
func TestRateLimit_DevModeRelaxesPerIPLogin(t *testing.T) {
	mgr := New(AuthConfig{
		JWTSecret: "dev-secret",
		DevMode:   true,
		UserStore: newMemoryUserStore(),
		// Tight per-IP limit so the defect reproduces in a handful of
		// requests; DevMode must relax it regardless of MaxAttempts.
		LoginRateLimit: &RateLimiterConfig{
			MaxAttempts:   3,
			Window:        time.Minute,
			BlockDuration: time.Minute,
		},
		// Neutralise the per-account limiter so only per-IP is under
		// test (per-account is intentionally kept on in dev).
		LoginRateLimitPerAccount: &RateLimiterConfig{
			MaxAttempts: 1_000_000,
			Window:      time.Minute,
		},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	for i := range 10 {
		// Distinct emails so the per-account key never repeats.
		body, _ := json.Marshal(map[string]string{
			"email":    fmt.Sprintf("tool-%d@example.com", i),
			"password": "wrong",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234" // same IP — local tooling
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d: DevMode per-IP login limiter throttled local tooling (429) — must be relaxed in dev", i+1)
		}
	}
}
