package authui_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/auth/authui"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// linkCatcher keeps the last magic link the plugin sent.
type linkCatcher struct{ link string }

func (c *linkCatcher) SendMagicLink(_ context.Context, _, link string) error {
	c.link = link
	return nil
}

// magicLinkRig wires auth with authui.Pages over a real UIHost, behind
// the auth CSRF middleware, and returns the router and a fresh link path.
func magicLinkRig(t *testing.T) (http.Handler, string) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	host := uihost.New(app.NewApp("authui"))
	mgr := auth.New(auth.AuthConfig{
		SessionTTL: time.Hour, SessionCookie: "session_id", DevMode: true,
		UserStore: auth.NewEntityUserStore(db, "authui_users"),
		Pages:     authui.Pages(host),
	})
	catch := &linkCatcher{}
	mgr.Use(auth.NewMagicLinkPlugin(auth.MagicLinkConfig{
		BaseURL: "http://localhost", TokenTTL: time.Hour, DevMode: true, EmailSender: catch,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	send := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", strings.NewReader(`{"email":"alice@example.com"}`))
	send.Header.Set("Content-Type", "application/json")
	sent := httptest.NewRecorder()
	r.ServeHTTP(sent, send)
	if sent.Code != http.StatusOK {
		t.Fatalf("send = %d %s", sent.Code, sent.Body.String())
	}
	u, err := url.Parse(catch.link)
	if err != nil || u.Path == "" {
		t.Fatalf("magic link %q: %v", catch.link, err)
	}
	return auth.CSRF()(r), u.RequestURI()
}

var csrfRe = regexp.MustCompile(`name="_csrf"\s+value="([^"]*)"`)

// The confirmation screen draws as a design-system AuthCard in the app's
// document, and its own form signs in: the token and CSRF field it
// carries are the ones the verify POST needs.
func TestConfirmScreenSignsIn(t *testing.T) {
	h, link := magicLinkRig(t)
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, link, nil))
	body := get.Body.String()
	if get.Code != http.StatusOK {
		t.Fatalf("GET confirm = %d %s", get.Code, body)
	}
	for _, want := range []string{
		`data-cui-comp="ui-auth-card"`,
		"<strong>alice@example.com</strong>",
		`action="/auth/magic-link/verify"`,
		"/__gofastr/runtime.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm page lacks %q", want)
		}
	}
	if got := get.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := get.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	csrf := csrfRe.FindAllStringSubmatch(body, -1)
	if len(csrf) != 1 {
		t.Fatalf("confirm form carries %d _csrf fields, want 1", len(csrf))
	}
	token := regexp.MustCompile(`name="token"[^>]*value="([^"]*)"|value="([^"]*)"[^>]*name="token"`).FindStringSubmatch(body)
	if token == nil {
		t.Fatal("confirm form has no token field")
	}
	tok := token[1] + token[2]
	form := url.Values{"token": {tok}, "_csrf": {csrf[0][1]}}
	post := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range get.Result().Cookies() {
		post.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	signedIn := false
	for _, c := range rec.Result().Cookies() {
		signedIn = signedIn || (c.Name == "session_id" && c.Value != "")
	}
	if !signedIn {
		t.Fatalf("submitting the styled form = %d %s, want a session", rec.Code, rec.Body.String())
	}
}

// The hop draws as an AuthCard whose link goes back to the same URL.
func TestHopScreenLinksBack(t *testing.T) {
	host := uihost.New(app.NewApp("authui"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token=abc", nil)
	authui.Pages(host).Hop(rec, req, auth.HopPageData{
		Target: "/auth/verify-email?token=abc&x=<y>", Title: "Verifying your email", Continue: "Continue",
	})
	body := rec.Body.String()
	for _, want := range []string{
		`data-cui-comp="ui-auth-card"`,
		"Verifying your email",
		`href="/auth/verify-email?token=abc&amp;x=&lt;y&gt;"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hop page lacks %q:\n%s", want, body)
		}
	}
}

// With no CSRF token on the context the battery read the cookie, and
// that field rides the form; with one, ui.Form stamps it and the
// battery's copy stays out, so the form never carries two.
func TestConfirmScreenCSRFFallback(t *testing.T) {
	host := uihost.New(app.NewApp("authui"))
	rec := httptest.NewRecorder()
	authui.Pages(host).MagicLinkConfirm(rec, httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token=t", nil), auth.ConfirmPageData{
		Token: "t", Action: "/auth/magic-link/verify",
		CSRFField: `<input type="hidden" name="_csrf" value="from-cookie">`, // not-a-secret: fixture
		Words:     auth.ConfirmWords{Title: "Confirm sign-in", Submit: "Sign in"},
	})
	if got := strings.Count(rec.Body.String(), `value="from-cookie"`); got != 1 {
		t.Errorf("cookie-read CSRF field appears %d times, want 1", got)
	}
}
