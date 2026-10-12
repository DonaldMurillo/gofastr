package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// changeHarness is a manager with the core and reset plugins, one user
// whose password is "oldpw123", and a live session for them.
type changeHarness struct {
	mgr    *AuthManager
	r      *router.Router
	store  *userStoreWithPassword
	sender *stubEmailSender
	user   *BasicUser
	sess   *Session
}

func newChangeHarness(t *testing.T, tune func(*AuthConfig)) *changeHarness {
	t.Helper()
	store := newUserStoreWithPassword()
	cfg := AuthConfig{SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, DevMode: true}
	if tune != nil {
		tune(&cfg)
	}
	mgr := New(cfg)
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", TokenTTL: time.Hour, EmailSender: sender}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-1", Email: "a@example.com", Roles: []string{"user"}}
	store.users["a@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["a@example.com"]
	r := router.New()
	mgr.RegisterRoutes(r)
	sess, err := mgr.SessionStore().Create(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return &changeHarness{mgr: mgr, r: r, store: store, sender: sender, user: user, sess: sess}
}

// change posts a JSON body to /auth/password with the given session
// token ("" sends no cookie).
func (h *changeHarness) change(token string, body map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: token})
	}
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w
}

func (h *changeHarness) passwordIs(pw string) bool {
	return CheckPassword(pw, h.store.users["a@example.com"].hash)
}

// fieldError is the message the 422 envelope names for field.
func fieldError(t *testing.T, w *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var env struct {
		Fields map[string][]string `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not JSON: %s", w.Body.String())
	}
	return strings.Join(env.Fields[field], ", ")
}

func TestPasswordChangeNeedsSession(t *testing.T) {
	h := newChangeHarness(t, nil)
	w := h.change("", map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("SECURITY: the password changed without a session")
	}
}

func TestPasswordChangeWrongCurrentRefused(t *testing.T) {
	h := newChangeHarness(t, nil)
	w := h.change(h.sess.Token, map[string]string{"current_password": "guess-123", "password": "brandnewpw1"})
	if w.Code != http.StatusUnprocessableEntity || fieldError(t, w, "current_password") == "" {
		t.Fatalf("wrong current password: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("SECURITY: a wrong current password changed the password")
	}
	if _, err := h.mgr.SessionStore().Get(context.Background(), h.sess.Token); err != nil {
		t.Fatal("a refused change revoked the caller's session")
	}
}

func TestPasswordChangeWeakRefused(t *testing.T) {
	h := newChangeHarness(t, nil)
	for _, pw := range []string{"short", "oldpw123"} {
		w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": pw})
		if w.Code != http.StatusUnprocessableEntity || fieldError(t, w, "password") == "" {
			t.Fatalf("new password %q: %d %s", pw, w.Code, w.Body.String())
		}
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("a refused new password was stored")
	}
}

func TestPasswordChangeConfirmMustMatch(t *testing.T) {
	h := newChangeHarness(t, nil)
	w := h.change(h.sess.Token, map[string]string{
		"current_password": "oldpw123", "password": "brandnewpw1", "confirm_password": "brandnewpw2",
	})
	if w.Code != http.StatusUnprocessableEntity || fieldError(t, w, "confirm_password") == "" {
		t.Fatalf("mismatched confirmation: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("a mismatched confirmation changed the password")
	}
	w = h.change(h.sess.Token, map[string]string{
		"current_password": "oldpw123", "password": "brandnewpw1", "confirm_password": "brandnewpw1",
	})
	if w.Code != http.StatusOK || !h.passwordIs("brandnewpw1") {
		t.Fatalf("matching confirmation: %d %s", w.Code, w.Body.String())
	}
}

func TestPasswordChangeCrossSiteRefused(t *testing.T) {
	h := newChangeHarness(t, nil)
	form := url.Values{"current_password": {"oldpw123"}, "password": {"brandnewpw1"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: h.sess.Token})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden && w.Code != http.StatusSeeOther {
		t.Fatalf("cross-site form post: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("SECURITY: a cross-site form post changed the password")
	}
}

func TestPasswordChangePendingTwoFARefused(t *testing.T) {
	h := newChangeHarness(t, nil)
	if err := h.mgr.SessionStore().(SessionPendingMarker).MarkPendingTwoFactor(context.Background(), h.sess.Token); err != nil {
		t.Fatal(err)
	}
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("pending 2FA session: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("SECURITY: a session short of its second factor changed the password")
	}
}

// Success stores the new password, signs every other session out,
// spends the user's outstanding reset links, and keeps the caller
// signed in on a fresh session.
func TestPasswordChangeRevokesOthers(t *testing.T) {
	h := newChangeHarness(t, nil)
	ctx := context.Background()
	other, _ := h.mgr.SessionStore().Create(ctx, h.user.ID, time.Hour)

	forgot, _ := json.Marshal(map[string]string{"email": "a@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(forgot))
	req.Header.Set("Content-Type", "application/json")
	h.r.ServeHTTP(httptest.NewRecorder(), req)
	_, mail := h.sender.snapshot()
	link := extractTokenFromBody(mail)
	if link == "" {
		t.Fatal("no reset link was issued")
	}

	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if !h.passwordIs("brandnewpw1") || h.passwordIs("oldpw123") {
		t.Fatal("the new password was not stored")
	}
	if _, err := h.mgr.SessionStore().Get(ctx, other.Token); err == nil {
		t.Error("SECURITY: another session survived the change")
	}
	var fresh string
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			fresh = c.Value
		}
	}
	if fresh == "" {
		t.Fatal("the caller got no session cookie back")
	}
	if s, err := h.mgr.SessionStore().Get(ctx, fresh); err != nil || s.UserID != h.user.ID || s.PendingTwoFactor {
		t.Fatalf("the caller's fresh session does not resolve: %v %+v", err, s)
	}

	reset, _ := json.Marshal(map[string]string{"token": link, "password": "attackerpw1"})
	req = httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(reset))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	h.r.ServeHTTP(rw, req)
	if rw.Code == http.StatusOK || !h.passwordIs("brandnewpw1") {
		t.Fatalf("SECURITY: a reset link from before the change still set the password: %d", rw.Code)
	}
}

// A user with a second factor keeps it on the fresh session: the
// caller's session had passed the challenge, so the new one has too.
func TestPasswordChangeKeepsTwoFAProof(t *testing.T) {
	h := newChangeHarness(t, nil)
	ctx := context.Background()
	h.mgr.Use(&fixedTwoFAChecker{enabled: true})
	if err := h.mgr.SessionStore().(SessionTwoFAMarker).MarkTwoFactorVerified(ctx, h.sess.Token); err != nil {
		t.Fatal(err)
	}
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	var fresh string
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			fresh = c.Value
		}
	}
	s, err := h.mgr.SessionStore().Get(ctx, fresh)
	if err != nil || s.PendingTwoFactor || !s.TwoFactorVerified {
		t.Fatalf("the fresh session lost the second factor: %v %+v", err, s)
	}
}

// A session that never passed the challenge, on an account with a
// second factor, cannot change the password.
func TestPasswordChangeNeedsTwoFAProof(t *testing.T) {
	h := newChangeHarness(t, nil)
	h.mgr.Use(&fixedTwoFAChecker{enabled: true})
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusForbidden || !h.passwordIs("oldpw123") {
		t.Fatalf("SECURITY: an unproven session changed a 2FA account's password: %d", w.Code)
	}
}

// The per-account login limiter guards the current-password check, so
// a stolen session cannot brute-force the password through this route.
func TestPasswordChangeRateLimited(t *testing.T) {
	h := newChangeHarness(t, func(c *AuthConfig) {
		c.LoginRateLimitPerAccount = &RateLimiterConfig{MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Minute}
	})
	var last int
	for range 5 {
		last = h.change(h.sess.Token, map[string]string{"current_password": "guess-123", "password": "brandnewpw1"}).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("the sixth guess answered %d, want 429", last)
	}
}

// fixedTwoFAChecker reports every user as having a second factor.
type fixedTwoFAChecker struct{ enabled bool }

func (f *fixedTwoFAChecker) Name() string                          { return "fixed-2fa" }
func (f *fixedTwoFAChecker) Init(*AuthManager) error               { return nil }
func (f *fixedTwoFAChecker) RegisterRoutes(*router.Router, string) {}
func (f *fixedTwoFAChecker) HasTwoFactorEnabled(context.Context, string) (bool, error) {
	return f.enabled, nil
}
