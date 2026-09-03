//go:build red

package auth

// RED TESTS — open finding, 2026-09-03 adversarial pass round 6 (tests-only; no fix applied).
// Property: a cookie-authenticated mutating route must refuse cross-site
// requests (rejectCrossSiteForm) — the same-origin/CSRF posture every
// sibling form-mutable auth handler already enforces (login, register,
// logout, magic-link verify, 2FA, password reset all call it first).
// Surface: email_verification.go sendHandler:99-170 — cookie-authenticated
// POST /auth/send-verification that parses NO body, so there is no JSON
// content-type structural guard keeping it fetch-only: a bodyless POST is
// CORS-simple (isForgeableRequest treats the absent Content-Type as
// forgeable, form_decode.go:49-55), exactly the shape an attacker page
// auto-submits. The handler never calls rejectCrossSiteForm.
// Finding: a cross-site POST with the victim's session cookie riding along
// (Sec-Fetch-Site: cross-site, Origin: evil.example) dispatches a fresh
// verification email to the victim's address — 200 {"sent":true}. No
// privilege is gained directly, but the attacker gets a free mail-spam
// primitive on the victim (each forged POST mints and mails a live
// takeover-credential URL) and can burn the per-user send budget.
// Severity: P3 — session-gated nuisance/DoS-shaped gap, not takeover.
// Fix direction: rejectCrossSiteForm(w, r) as the first statement of
// sendHandler, mirroring logoutHandler (core.go:339-341) and the 2FA
// handlers. verifyHandler needs no equivalent: it is token-authenticated,
// not cookie-authenticated, so no ambient credential rides a forged POST.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendVerificationRedRejectsCrossSite(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
	})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:     "http://localhost",
		EmailSender: sender,
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

	// post sends an authenticated bodyless POST (a CORS-simple request:
	// no Content-Type at all) with same-origin or cross-site markers.
	post := func(crossSite bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-verification", nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		if crossSite {
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		} else {
			req.Header.Set("Origin", "http://example.com")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	// Positive control: same-origin send works end to end — otherwise the
	// harness, not the seam, is what refuses.
	if rr := post(false); rr.Code != http.StatusOK {
		t.Fatalf("setup: same-origin send-verification got %d (body=%s), want 200 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	_, sent1 := sender.snapshot()
	if sent1 == "" {
		t.Fatalf("setup: same-origin send dispatched no email — harness broken, not the seam")
	}

	// The attack: the victim's browser auto-submits a cross-site bodyless
	// POST; the SameSite cookie rides along (a fresh token is minted per
	// send, so a second dispatched email is observable as a body change).
	rr := post(true)
	_, sent2 := sender.snapshot()
	refused := rr.Code >= 400 && rr.Code < 500
	if mailed := sent2 != sent1; !refused || mailed {
		t.Errorf("SECURITY: [emailverify-csrf] cross-site POST /auth/send-verification (Sec-Fetch-Site: cross-site, Origin: evil.example, session cookie attached) returned %d and dispatched a fresh email (mailed=%v) — "+
			"sendHandler authenticates by cookie and parses no body, so nothing structural keeps it fetch-only, yet it never calls rejectCrossSiteForm "+
			"(every sibling form-mutable auth handler does, core.go:161/339/466 et al.): an attacker page can drive verification-mail sends on a signed-in user's session",
			rr.Code, mailed)
	}
}
