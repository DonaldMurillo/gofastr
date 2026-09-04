package auth

// CSRF posture of the send-verification route.
//
// Property: a cookie-authenticated mutating route refuses cross-site
// requests — the same rejectCrossSiteForm gate every sibling
// form-mutable auth handler applies (login, register, logout,
// magic-link verify, 2FA, password reset). sendHandler parses no body,
// so without the gate a bodyless POST is CORS-simple and an attacker
// page can auto-submit it with the victim's session cookie riding
// along, driving verification-email sends (and burning the send budget)
// on a signed-in user's session.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendVerificationRejectsCrossSite(t *testing.T) {
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
			"the route is cookie-authenticated and parses no body, so without rejectCrossSiteForm an attacker page can drive verification-mail sends on a signed-in user's session",
			rr.Code, mailed)
	}
}
