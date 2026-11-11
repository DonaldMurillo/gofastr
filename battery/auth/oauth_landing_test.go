package auth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// landingApp is an app with one OAuth provider and a home page that says
// whether the request carried a session. authURLFmt is the provider's
// authorize URL with one %s for the state.
func landingApp(t *testing.T, authURLFmt string) *router.Router {
	t.Helper()
	provider := &mockProvider{
		name:       "mock",
		tokenResp:  &OAuth2Token{AccessToken: "at"},
		userResp:   &OAuth2UserInfo{ID: "idp-1", Email: "new@example.com", EmailVerified: true},
		authURLFmt: authURLFmt,
	}
	mgr := New(AuthConfig{JWTSecret: "test-secret", SessionTTL: time.Hour, UserStore: newLinkingUserStore(), DevMode: true})
	mgr.Use(NewOAuth2Plugin(OAuth2Config{Providers: map[string]OAuth2Provider{"mock": provider}, StateSecret: "test-secret-key-state"}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	r.Use(SessionMiddleware(mgr))
	mgr.RegisterRoutes(r)
	r.Get("/{$}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		who := "anonymous"
		if GetCurrentUser(req.Context()) != nil {
			who = "signed-in"
		}
		_, _ = io.WriteString(w, `<!doctype html><title>home</title><p id="who">`+who+`</p><a id="login" href="/auth/oauth/mock">sign in</a>`)
	}))
	return r
}

// startedFlow starts a sign-in on r and returns the callback request the
// provider's redirect back makes, state cookie included.
func startedFlow(t *testing.T, r *router.Router) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil))
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Query().Get("state") == "" {
		t.Fatalf("no state in the provider redirect %q (status %d)", rec.Header().Get("Location"), rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?code=c1&state="+url.QueryEscape(loc.Query().Get("state")), nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

// The provider's redirect back is a cross-site navigation, and a 302
// from the callback continues it: the browser withholds the
// SameSite=Strict session cookie from the landing page, which rendered
// signed out. A cross-site callback answers a page from this origin that
// loads the landing page, with the session cookie on that answer.
func TestOAuthCrossSiteCallbackHopsToLanding(t *testing.T) {
	r := landingApp(t, "https://idp.example/authorize?state=%s")
	req := startedFlow(t, r)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-site callback answered %d, want the hop page (200): %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Refresh"); got != "0;url=/" {
		t.Fatalf("Refresh = %q, want a load of the landing page from this origin", got)
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("the hop answer carries no session cookie")
	}
	if session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie SameSite = %v, want Strict", session.SameSite)
	}
	if !strings.Contains(rec.Body.String(), `href="/"`) {
		t.Fatalf("the hop page has no link to continue:\n%s", rec.Body.String())
	}
}

// A browser sends no Fetch Metadata to a plain-HTTP LAN address, so an
// IdP's return there looks like curl. It gets the hop too: the plain
// redirect carried the Strict session cookie it had just set to a page
// that rendered signed out. The hop's target is the landing page, not
// this callback, so it carries no marker and never hops again.
func TestOAuthCallbackHopsWithoutFetchMeta(t *testing.T) {
	r := landingApp(t, "https://idp.example/authorize?state=%s")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, startedFlow(t, r))
	if rec.Code != http.StatusOK || rec.Header().Get("Refresh") != "0;url=/" {
		t.Fatalf("got %d Refresh %q, want the 200 hop page loading /", rec.Code, rec.Header().Get("Refresh"))
	}
	if sessionCookie(rec) == "" {
		t.Fatal("the hop page set no session cookie")
	}
}

// An arrival that says same-origin keeps the plain redirect.
func TestOAuthSameOriginCallbackRedirects(t *testing.T) {
	r := landingApp(t, "https://idp.example/authorize?state=%s")
	req := startedFlow(t, r)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("got %d Location %q, want 302 to /", rec.Code, rec.Header().Get("Location"))
	}
}

// A real browser signs in through a provider page on another site and
// lands signed in. The provider is 127.0.0.1 and the app is localhost,
// two sites to the browser, and the user clicks through the provider's
// page, as a first consent does.
func TestOAuthSignInLandsSignedIn(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	browser, ok := browserpath.Find()
	if !ok {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	var appPort string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		back := "http://localhost:" + appPort + "/auth/oauth/mock/callback?code=c1&state=" + url.QueryEscape(req.URL.Query().Get("state"))
		fmt.Fprintf(w, `<!doctype html><title>provider</title><a id="approve" href="%s">approve</a>`, back)
	}))
	defer idp.Close()
	app := httptest.NewServer(landingApp(t, idp.URL+"/authorize?state=%s"))
	defer app.Close()
	_, appPort, err := net.SplitHostPort(strings.TrimPrefix(app.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browser)))
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var landed string
	if err := chromedp.Run(ctx,
		chromedp.Navigate("http://localhost:"+appPort+"/"),
		chromedp.Click(`#login`, chromedp.ByID),
		chromedp.WaitVisible(`#approve`, chromedp.ByID),
		chromedp.Click(`#approve`, chromedp.ByID),
		chromedp.WaitVisible(`#who`, chromedp.ByID),
		chromedp.Text(`#who`, &landed, chromedp.ByID),
	); err != nil {
		t.Fatalf("the sign-in never reached the home page: %v", err)
	}
	if landed != "signed-in" {
		t.Fatalf("the page a sign-in landed on rendered %q, want signed-in", landed)
	}
}
