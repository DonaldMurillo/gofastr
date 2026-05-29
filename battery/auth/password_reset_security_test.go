package auth

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

// Property: a successful password reset must revoke the victim's pre-existing
// sessions, so a credential compromised before the reset cannot retain access
// afterwards. Resetting the password is exactly how a victim tries to lock out
// an attacker holding a live stolen cookie.
func TestPasswordReset_RevokesExistingSessions(t *testing.T) {
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

	oldHash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-9", Email: "v@example.com", Roles: []string{"user"}}
	store.users["v@example.com"] = &storeEntry{user: user, hash: oldHash}
	store.byID[user.ID] = store.users["v@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)

	// Attacker holds a live session for the victim (stolen cookie).
	stolen, err := mgr.SessionStore().Create(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create stolen session: %v", err)
	}
	// Sanity: the stolen session resolves before the reset.
	if _, err := mgr.SessionStore().Get(context.Background(), stolen.Token); err != nil {
		t.Fatalf("precondition: stolen session should resolve, got %v", err)
	}

	// Victim runs the reset flow.
	body, _ := json.Marshal(map[string]string{"email": "v@example.com"})
	forgotReq := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	forgotReq.Header.Set("Content-Type", "application/json")
	forgotW := httptest.NewRecorder()
	r.ServeHTTP(forgotW, forgotReq)
	if forgotW.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", forgotW.Code)
	}
	_, emailBody := sender.snapshot()
	tok := extractTokenFromBody(emailBody)
	if tok == "" {
		t.Fatalf("no token in reset email body: %q", emailBody)
	}

	resetBody, _ := json.Marshal(map[string]string{"token": tok, "password": "brandnewpw1"})
	resetReq := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(resetBody))
	resetReq.Header.Set("Content-Type", "application/json")
	resetW := httptest.NewRecorder()
	r.ServeHTTP(resetW, resetReq)
	if resetW.Code != http.StatusOK {
		t.Fatalf("reset-password: %d (body=%s)", resetW.Code, resetW.Body.String())
	}

	// The attacker's pre-existing session must no longer resolve.
	if _, err := mgr.SessionStore().Get(context.Background(), stolen.Token); err == nil {
		t.Fatalf("stolen session still resolves after password reset; sessions were not revoked")
	}
}

// Property: a single-use reset token must survive a request that fails
// validation (weak password) so the legitimate user can retry without
// restarting the whole forgot-password flow. The token is only consumed when
// it actually results in a password change.
func TestPasswordReset_TokenSurvivesBadPassword(t *testing.T) {
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

	oldHash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-11", Email: "u@example.com", Roles: []string{"user"}}
	store.users["u@example.com"] = &storeEntry{user: user, hash: oldHash}
	store.byID[user.ID] = store.users["u@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)

	// Issue a reset token.
	body, _ := json.Marshal(map[string]string{"email": "u@example.com"})
	forgotReq := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	forgotReq.Header.Set("Content-Type", "application/json")
	forgotW := httptest.NewRecorder()
	r.ServeHTTP(forgotW, forgotReq)
	if forgotW.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", forgotW.Code)
	}
	_, emailBody := sender.snapshot()
	tok := extractTokenFromBody(emailBody)
	if tok == "" {
		t.Fatalf("no token in reset email body: %q", emailBody)
	}

	reset := func(password string) *httptest.ResponseRecorder {
		rb, _ := json.Marshal(map[string]string{"token": tok, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(rb))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// Attempt 1: a too-weak password fails validation. Token must NOT be burned.
	weakW := reset("short")
	if weakW.Code != http.StatusBadRequest {
		t.Fatalf("weak password should 400, got %d (body=%s)", weakW.Code, weakW.Body.String())
	}

	// Attempt 2: same token with a strong password must still succeed.
	goodW := reset("brandnewpw1")
	if goodW.Code != http.StatusOK {
		t.Fatalf("token was burned by the failed attempt; retry got %d (body=%s)", goodW.Code, goodW.Body.String())
	}

	// And only NOW is the token consumed — a third use must be rejected.
	replayW := reset("anotherpw12")
	if replayW.Code == http.StatusOK {
		t.Fatalf("token must be single-use; replay after success succeeded")
	}
}

// SessionUserPurger must be implemented by both built-in stores so the reset
// flow can revoke sessions. Pin the contract: a store that loses this method
// silently re-opens the post-reset takeover window.
func TestSessionStores_ImplementUserPurge(t *testing.T) {
	if _, ok := any((*MemorySessionStore)(nil)).(SessionUserPurger); !ok {
		t.Fatalf("*MemorySessionStore must implement SessionUserPurger")
	}
	if _, ok := any((*EntitySessionStore)(nil)).(SessionUserPurger); !ok {
		t.Fatalf("*EntitySessionStore must implement SessionUserPurger")
	}
}
