package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Pins: a config-supplied origin that builds user-facing
// credential-bearing links (BaseURL on the password-reset,
// email-verification, and magic-link plugins) is scheme-validated at
// Init (url.Parse, http/https only), so init fails loudly next to the
// declaration instead of shipping attacker-shaped links. Found by the
// 2026-09-06/07 adversarial round 5, phase 2 (family enumeration, tier
// T3 CONTRACT-QUESTION). Maintainer answer (2026-09-07): validate — the
// in-repo precedent is strict mode's invalidSitemapBaseURL
// (framework/uihost/strict.go:578-596), which refuses any scheme that
// is not http/https; the battery's invalidLinkBaseURL mirrors it.
// Pinned sibling: strict.go::invalidSitemapBaseURL's scheme arm
// ("needs an http or https scheme") — the repo's contract that a
// config-supplied origin building user-facing URLs is scheme-validated
// at load.
// Property: a config-supplied origin that builds user-facing
// credential-bearing links is scheme-validated at load (http/https only),
// so init fails loudly next to the declaration instead of shipping
// attacker-shaped links.
// Surfaces: battery/auth/password_reset.go:217-218, email_verification.go
// :160-161, magiclink.go:404-408 — fmt.Sprintf(BaseURL+...) verbatim into
// the reset/verify/magic-link URL; no validation anywhere in the battery
// (NewPasswordResetPlugin and Init are pass-through).
// Finding: BaseURL "//evil.example" (scheme-relative) is accepted at init
// and the emailed reset link carries it verbatim —
// "//evil.example/auth/reset-password?token=…" — a credential-bearing URL
// with an attacker-chosen origin shape; a misconfigured value ships
// silently instead of failing at boot.
// Fix direction: validate cfg.BaseURL at plugin Init (url.Parse; refuse
// anything whose scheme is not http/https, mirroring
// invalidSitemapBaseURL) across all three plugins.

func TestEmailBaseURLRedSchemeValidated(t *testing.T) {
	// Control leg: an https origin inits cleanly and the link carries it.
	// Proves the demanded validation is scheme-shape, not blanket refusal.
	ctlStore := newUserStoreWithPassword()
	ctlMgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     ctlStore,
		DevMode:       true,
	})
	ctlMgr.Use(NewCorePlugin())
	ctlSender := &stubEmailSender{}
	ctlMgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "https://app.example.com",
		TokenTTL:    time.Hour,
		EmailSender: ctlSender,
	}))
	if err := ctlMgr.Init(nil); err != nil {
		t.Fatalf("setup broken: control https BaseURL refused at init: %v", err)
	}
	ctlUser := &BasicUser{ID: "u-ctl", Email: "ctl@example.com", Roles: []string{"user"}}
	ctlHash, _ := HashPassword("ctlpw123")
	ctlStore.users["ctl@example.com"] = &storeEntry{user: ctlUser, hash: ctlHash}
	ctlStore.byID[ctlUser.ID] = ctlStore.users["ctl@example.com"]
	ctlRouter := router.New()
	ctlMgr.RegisterRoutes(ctlRouter)
	ctlBody, _ := json.Marshal(map[string]string{"email": "ctl@example.com"})
	ctlReq := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(ctlBody))
	ctlReq.Header.Set("Content-Type", "application/json")
	ctlW := httptest.NewRecorder()
	ctlRouter.ServeHTTP(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		t.Fatalf("setup broken: control forgot-password: %d", ctlW.Code)
	}
	_, ctlEmailBody := ctlSender.snapshot()
	if !strings.Contains(ctlEmailBody, "https://app.example.com/auth/reset-password?token=") {
		t.Fatalf("setup broken: control reset link does not carry the https origin: %q", ctlEmailBody)
	}

	// Hostile leg: a scheme-relative origin must fail loudly at init.
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "//evil.example",
		TokenTTL:    time.Hour,
		EmailSender: sender,
	}))
	if err := mgr.Init(nil); err == nil {
		// Evidence: the plugin marches on and emails the hostile origin.
		user := &BasicUser{ID: "u-v", Email: "v@example.com", Roles: []string{"user"}}
		hash, _ := HashPassword("victimpw123")
		store.users["v@example.com"] = &storeEntry{user: user, hash: hash}
		store.byID[user.ID] = store.users["v@example.com"]
		r := router.New()
		mgr.RegisterRoutes(r)
		body, _ := json.Marshal(map[string]string{"email": "v@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		carried := false
		if w.Code == http.StatusOK {
			if _, emailBody := sender.snapshot(); strings.Contains(emailBody, "//evil.example") {
				carried = true
			}
		}
		t.Errorf("SECURITY: [auth-email-baseurl-scheme] PasswordResetPlugin.Init accepted BaseURL %q (scheme is not http/https): the config error surfaces only as an emailed link, and the reset URL carries the hostile origin verbatim (observed in the sent body: %t). A credential-bearing link origin is load-time input: init must fail loudly next to the declaration, like strict mode's invalidSitemapBaseURL.", "//evil.example", carried)
	}
}
