package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// purgeHarness has the core, reset and magic-link plugins and one user
// whose stored address is email and whose password is "oldpw123".
type purgeHarness struct {
	mgr   *AuthManager
	r     *router.Router
	reset *PasswordResetPlugin
	magic *MagicLinkPlugin
	links *mockEmailSender
	user  *BasicUser
}

func newPurgeHarness(t *testing.T, email string, tokens MagicLinkTokenStore) *purgeHarness {
	t.Helper()
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, DevMode: true})
	mgr.Use(NewCorePlugin())
	reset := NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", TokenTTL: time.Hour, EmailSender: &stubEmailSender{}})
	mgr.Use(reset)
	links := &mockEmailSender{}
	magic := NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", TokenTTL: time.Hour, EmailSender: links, TokenStore: tokens})
	mgr.Use(magic)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-ml", Email: email, Roles: []string{"user"}}
	store.users[email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[email]
	r := router.New()
	mgr.RegisterRoutes(r)
	return &purgeHarness{mgr: mgr, r: r, reset: reset, magic: magic, links: links, user: user}
}

func (h *purgeHarness) post(t *testing.T, path string, body map[string]string, cookie string) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w.Code
}

// mailLink requests a magic link for addr and returns its token.
func (h *purgeHarness) mailLink(t *testing.T, addr string) string {
	t.Helper()
	if code := h.post(t, "/auth/magic-link/send", map[string]string{"email": addr}, ""); code != http.StatusOK {
		t.Fatalf("magic-link send = %d", code)
	}
	u, err := url.Parse(h.links.lastURL)
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("no token in link %q", h.links.lastURL)
	}
	return u.Query().Get("token")
}

func (h *purgeHarness) linkDead(t *testing.T, tok string) {
	t.Helper()
	_, err := h.magic.tokenStore.(MagicLinkTokenPeeker).PeekToken(context.Background(), tok)
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("SECURITY: a magic link mailed before the password changed still signs in (peek err = %v)", err)
	}
}

// A magic link mailed before a reset, the link an attacker with mailbox
// access may hold, signed in after the reset that was meant to lock them
// out. The magic-link store keys on the canonical address, so a stored
// "Mixed@Example.com" must purge the link minted for mixed@example.com.
func TestResetDropsMagicLinks(t *testing.T) {
	for _, stored := range []string{"m@example.com", "Mixed@Example.com"} {
		t.Run(stored, func(t *testing.T) {
			h := newPurgeHarness(t, stored, nil)
			link := h.mailLink(t, stored)
			reset, err := createPurposeToken(context.Background(), h.reset.store, purposeReset, h.user.ID, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if code := h.post(t, "/auth/reset-password", map[string]string{"token": reset, "password": "brandnewpw1"}, ""); code != http.StatusOK {
				t.Fatalf("reset = %d", code)
			}
			h.linkDead(t, link)
		})
	}
}

// The same on a password change from a signed-in session.
func TestChangeDropsMagicLinks(t *testing.T) {
	for _, stored := range []string{"m@example.com", "Mixed@Example.com"} {
		t.Run(stored, func(t *testing.T) {
			h := newPurgeHarness(t, stored, nil)
			link := h.mailLink(t, stored)
			sess, err := h.mgr.SessionStore().Create(context.Background(), h.user.ID, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"}
			if code := h.post(t, "/auth/password", body, sess.Token); code != http.StatusOK {
				t.Fatalf("change = %d", code)
			}
			h.linkDead(t, link)
		})
	}
}

// failingLinkPurge refuses every purge.
type failingLinkPurge struct{ *MemoryMagicLinkTokenStore }

func (failingLinkPurge) DeleteTokensForPayload(context.Context, string) (int, error) {
	return 0, errors.New("purge down")
}

// A reset whose link purge failed answered 200 with the mailed link still
// live; it answers 500, as a failed API token or passkey purge does.
func TestResetFailsIfLinkPurgeFails(t *testing.T) {
	h := newPurgeHarness(t, "m@example.com", failingLinkPurge{NewMemoryMagicLinkTokenStore()})
	reset, err := createPurposeToken(context.Background(), h.reset.store, purposeReset, h.user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if code := h.post(t, "/auth/reset-password", map[string]string{"token": reset, "password": "brandnewpw1"}, ""); code != http.StatusInternalServerError {
		t.Fatalf("reset with a failed link purge = %d, want 500", code)
	}
}
