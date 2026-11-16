package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// resetFormRig is a manager with the password-reset plugin whose emailed
// link opens linkPath, one account, and the router serving both.
func resetFormRig(t *testing.T, linkPath string) (*router.Router, *stubEmailSender, *userStoreWithPassword) {
	t.Helper()
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, DevMode: true})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: sender, LinkPath: linkPath}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-1", Email: "a@example.com", Roles: []string{"user"}}
	store.users["a@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["a@example.com"]
	r := router.New()
	mgr.RegisterRoutes(r)
	return r, sender, store
}

func postForm(r http.Handler, path string, form url.Values, referer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// An HTML sign-in page can run the whole reset: the forgot form answers
// every address with the same 303 to its next, the mail links to the
// app's reset page, and the reset form sets the password and 303s on.
func TestPasswordResetHTMLForms(t *testing.T) {
	r, sender, store := resetFormRig(t, "/reset-password")

	for _, addr := range []string{"a@example.com", "nobody@example.com"} {
		w := postForm(r, "/auth/forgot-password", url.Values{"email": {addr}, "next": {"/forgot-password?sent=1"}}, "")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/forgot-password?sent=1" {
			t.Fatalf("forgot form for %s: %d Location=%q, want 303 to /forgot-password?sent=1", addr, w.Code, w.Header().Get("Location"))
		}
	}
	_, body := sender.snapshot()
	if !strings.Contains(body, "http://localhost/reset-password?token=") {
		t.Fatalf("the mail does not link to LinkPath: %q", body)
	}
	tok := extractTokenFromBody(body)

	bad := postForm(r, "/auth/reset-password", url.Values{"token": {"nope"}, "password": {"brandnewpw1"}}, "http://example.com/reset-password?token=nope")
	if bad.Code != http.StatusSeeOther || !strings.HasPrefix(bad.Header().Get("Location"), "/reset-password?error=") {
		t.Errorf("a bad token: %d Location=%q, want 303 back to /reset-password?error=…", bad.Code, bad.Header().Get("Location"))
	}

	ok := postForm(r, "/auth/reset-password", url.Values{"token": {tok}, "password": {"brandnewpw1"}, "next": {"/login?reset=1"}}, "")
	if ok.Code != http.StatusSeeOther || ok.Header().Get("Location") != "/login?reset=1" {
		t.Fatalf("reset form: %d Location=%q body=%s, want 303 to /login?reset=1", ok.Code, ok.Header().Get("Location"), ok.Body.String())
	}
	if !verifyPassword("brandnewpw1", store.users["a@example.com"].hash) {
		t.Error("the reset form answered 303 but the password did not change")
	}
}

// LinkPath goes into a mailed URL, so only a same-origin path is accepted.
func TestPasswordResetLinkPathMustBeAPath(t *testing.T) {
	for _, bad := range []string{"https://evil.example/r", "//evil.example/r", "/r?x=1", "reset"} {
		mgr := New(AuthConfig{UserStore: newUserStoreWithPassword(), DevMode: true})
		mgr.Use(NewCorePlugin())
		mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", LinkPath: bad}))
		if err := mgr.Init(nil); err == nil {
			t.Errorf("LinkPath %q was accepted", bad)
		}
	}
}
