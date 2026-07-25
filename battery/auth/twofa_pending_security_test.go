package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: a session in the PendingTwoFactor (pre-step-up) state must not
// reach any 2FA self-service endpoint except /2fa/challenge. The pending
// state proves only the password, so it must not be able to disable, re-
// enroll, or refresh the second factor — doing so would defeat 2FA with
// the password alone (full account takeover).
//
// Surfaces: /2fa/disable, /2fa/enroll, /2fa/verify, /2fa/backup-codes.
// Each is reached via the same getSessionUser path, which previously did
// not inspect PendingTwoFactor. Contrast meHandler (gated) and the doc
// comment on Session.PendingTwoFactor ("ONLY valid for /auth/2fa/challenge").
func TestPendingTwoFA_MutationEndpointsRejected(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"disable", http.MethodPost, "/auth/2fa/disable", ""},
		{"enroll", http.MethodPost, "/auth/2fa/enroll", ""},
		{"verify", http.MethodPost, "/auth/2fa/verify", `{"code":"000000"}`},
		{"backup-codes", http.MethodGet, "/auth/2fa/backup-codes", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, twofa, r := setupP17(t)
			tok := loginP17(t, r) // pending-2FA session (password only)

			// Capture the victim's live secret to prove it is untouched.
			before, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			if before == nil || !before.Enabled {
				t.Fatalf("precondition: victim must have 2FA enabled")
			}

			var bodyReader *strings.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			} else {
				bodyReader = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Fatalf("%s with pending session: expected 401/403, got %d (body=%s)",
					tc.path, w.Code, w.Body.String())
			}

			// The live second factor must survive unchanged.
			after, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			if after == nil || !after.Enabled {
				t.Fatalf("%s: pending session disabled the victim's live 2FA", tc.path)
			}
			if after.Secret != before.Secret {
				t.Fatalf("%s: pending session overwrote the victim's live 2FA secret", tc.path)
			}
			// Backup codes must not have been regenerated/returned.
			if strings.Contains(w.Body.String(), "backup_codes") {
				t.Fatalf("%s: pending session received fresh backup codes (2FA bypass)", tc.path)
			}
		})
	}
}

// setupP17WithMagicLink is setupP17 plus the magic-link plugin, so the
// same 2FA-enrolled user can be logged in through a second minting path.
func setupP17WithMagicLink(t *testing.T) (*AuthManager, *TwoFAPlugin, *MagicLinkPlugin, *router.Router) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // nosecret: test fixture
		AllowInMemoryStores: true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	core := NewCorePlugin()
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	magic := NewMagicLinkPlugin(MagicLinkConfig{})
	// DevMode so send-verification does not fail closed on a missing
	// EmailSender — a 503 would hide whether the step-up check fired.
	verify := NewEmailVerificationPlugin(EmailVerificationConfig{DevMode: true, BaseURL: "http://localhost"})
	mgr.Use(core)
	mgr.Use(twofa)
	mgr.Use(magic)
	mgr.Use(verify)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]
	if err := twofa.store.SetTwoFA(context.Background(), user.ID, &TwoFAState{
		Enabled: true, Secret: GenerateSecret(), Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return mgr, twofa, magic, r
}

// TestTwoFA_EveryLoginPathMintsPending pins that every path that mints a
// session marks it PendingTwoFactor when the user has a second factor
// enabled — not just the password path.
//
// Attack: enforcement reads the negative flag PendingTwoFactor, and that
// flag is set in exactly one place — markPendingIfTwoFactorEnabled,
// called only from loginHandler. Magic-link verify and the OAuth callback
// create their session directly, so both hand back a session with
// PendingTwoFactor=false, which every gate treats as fully authenticated.
// The bypass is persistent, too: such a session can call
// POST /auth/2fa/disable and remove the factor for good.
//
// The property — "a session minted for a 2FA-enrolled user is never fully
// privileged until the factor is proven" — is asserted at each minting
// surface rather than with more cases at one surface. TestPendingTwoFA_*
// above covers what a correctly-pending session may then do.
func TestTwoFA_EveryLoginPathMintsPending(t *testing.T) {
	surfaces := []struct {
		name  string
		login func(t *testing.T, magic *MagicLinkPlugin, r *router.Router) string
	}{
		{"password", func(t *testing.T, _ *MagicLinkPlugin, r *router.Router) string {
			return loginP17(t, r)
		}},
		{"magic-link", func(t *testing.T, magic *MagicLinkPlugin, r *router.Router) string {
			token, err := magic.tokenStore.CreateToken(context.Background(), "alice@example.com", time.Hour)
			if err != nil {
				t.Fatalf("CreateToken: %v", err)
			}
			req := magicConfirmReq(token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			for _, c := range w.Result().Cookies() {
				if c.Name == "session_id" {
					return c.Value
				}
			}
			t.Fatalf("magic-link verify set no session cookie (status %d, body=%s)", w.Code, w.Body.String())
			return ""
		}},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			mgr, twofa, magic, r := setupP17WithMagicLink(t)
			tok := s.login(t, magic, r)

			sess, err := mgr.SessionStore().Get(context.Background(), tok)
			if err != nil || sess == nil {
				t.Fatalf("session lookup after %s login: %v", s.name, err)
			}
			if !sess.PendingTwoFactor {
				t.Errorf("SECURITY: [authz] %s login minted PendingTwoFactor=false for a 2FA-enrolled user — the second factor is skipped entirely", s.name)
			}

			before, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			req := httptest.NewRequest(http.MethodPost, "/auth/2fa/disable", strings.NewReader(""))
			req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			after, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			if after == nil || !after.Enabled || after.Secret != before.Secret {
				t.Errorf("SECURITY: [authz] a %s-minted session disabled the user's second factor without ever proving it (status %d)", s.name, w.Code)
			}
		})
	}
}

// TestTwoFA_StepUpNeedsVerifiedFlag pins that step-up is checked
// positively.
//
// Attack: requireStepUpUser refuses only sessions carrying
// PendingTwoFactor. A session minted BEFORE the user enrolled has that
// flag false forever, so it stays "stepped up" for its whole lifetime and
// can disable a factor it never proved. Reading TwoFactorVerified — the
// positive flag the challenge handler sets — is what makes this a real
// step-up. Enrolling should also not leave sibling sessions privileged.
func TestTwoFA_StepUpNeedsVerifiedFlag(t *testing.T) {
	mgr, twofa, _, r := setupP17WithMagicLink(t)

	// A session as it would exist from before enrolment:
	// PendingTwoFactor=false, TwoFactorVerified=false.
	sess, err := mgr.SessionStore().Create(context.Background(), "u-1", time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, path := range []string{"/auth/2fa/disable", "/auth/2fa/enroll"} {
		before, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(""))
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Errorf("SECURITY: [authz] %s accepted a session that never proved the second factor (TwoFactorVerified=false)", path)
		}
		after, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
		if after == nil || !after.Enabled || after.Secret != before.Secret {
			t.Errorf("SECURITY: [authz] %s mutated the live second factor from a never-verified session", path)
		}
	}
}

// TestSendVerificationNeedsStepUp pins that /auth/send-verification
// applies the pending-2FA check its four siblings have.
//
// Attack: the handler resolves the session without consulting
// PendingTwoFactor, so a half-authenticated session (password proven,
// factor not) can drive verification-email sends — an email-bombing
// primitive aimed at someone else's address.
func TestSendVerificationNeedsStepUp(t *testing.T) {
	_, _, _, r := setupP17WithMagicLink(t)
	tok := loginP17(t, r) // pending-2FA session (password only)

	req := httptest.NewRequest(http.MethodPost, "/auth/send-verification", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Errorf("SECURITY: [dos] /auth/send-verification accepted a pending-2FA session (status %d) — its four siblings refuse one", w.Code)
	}
}

// TestMagicLinkVerifyNeedsConfirmation pins that following a magic link
// does not, by itself, sign anyone in.
//
// Attack (login CSRF / account confusion): the link IS the credential,
// so an attacker requests one for THEIR OWN account and gets a victim to
// click it. The verify endpoint was a GET that minted a session
// outright, so the victim's browser silently ended up signed into the
// attacker's account — everything they typed next landed there. The
// GET is invisible to rejectCrossSiteForm, and the usual browser-binding
// fixes do not apply: a binding cookie breaks cross-device links (request
// on a laptop, open on a phone) and refusing Sec-Fetch-Site: cross-site
// breaks every webmail client.
//
// A confirmation step is what actually fits the flow: GET renders "sign
// in as x@y?", and only a same-origin POST from that page redeems the
// token. Cross-device still works; a silent sign-in does not.
func TestMagicLinkVerifyNeedsConfirmation(t *testing.T) {
	_, _, magic, r := setupP17WithMagicLink(t)
	token, err := magic.tokenStore.CreateToken(context.Background(), "victim@example.com", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Following the link must NOT hand back a session…
	get := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	gw := httptest.NewRecorder()
	r.ServeHTTP(gw, get)
	for _, c := range gw.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			t.Errorf("SECURITY: [csrf] GET on the magic link minted a session outright — a link the attacker generated signs the victim into the attacker's account on click")
		}
	}
	if gw.Code != http.StatusOK {
		t.Fatalf("magic-link GET should render a confirmation page, got %d (body=%s)", gw.Code, gw.Body.String())
	}
	body := gw.Body.String()
	if !strings.Contains(body, "victim@example.com") {
		t.Errorf("the confirmation page must name the account being signed into, so the victim can recognise it is not theirs. Body: %s", body)
	}
	if !strings.Contains(body, token) {
		t.Errorf("the confirmation page must carry the token forward for the POST. Body: %s", body)
	}

	// …and the token must survive, since the GET did not consume it.
	post := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify",
		strings.NewReader("token="+token))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	pw := httptest.NewRecorder()
	r.ServeHTTP(pw, post)

	var got string
	for _, c := range pw.Result().Cookies() {
		if c.Name == "session_id" {
			got = c.Value
		}
	}
	if got == "" {
		t.Fatalf("confirming the sign-in did not mint a session: %d (body=%s)", pw.Code, pw.Body.String())
	}
}

// A cross-site POST cannot skip the confirmation the interstitial exists
// to force — otherwise the attacker just auto-submits the form from
// their own page with the token they already hold.
func TestMagicLinkConfirmRejectsCrossSite(t *testing.T) {
	_, _, magic, r := setupP17WithMagicLink(t)
	token, err := magic.tokenStore.CreateToken(context.Background(), "victim@example.com", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	post := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify",
		strings.NewReader("token="+token))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "cross-site")
	pw := httptest.NewRecorder()
	r.ServeHTTP(pw, post)

	for _, c := range pw.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			t.Errorf("SECURITY: [csrf] a cross-site POST redeemed the magic link, bypassing the confirmation step entirely")
		}
	}
}
