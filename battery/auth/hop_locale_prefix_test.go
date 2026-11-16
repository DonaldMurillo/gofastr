package auth

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
)

// underLocalePrefix serves h the way an app built with
// framework.WithLocalePrefixes does: /es/<path> reaches h as /<path>.
func underLocalePrefix(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	cat := i18n.NewMapCatalog()
	cat.Set("es", oauthHopContinue.Key(), i18n.Message{Text: "Continuar"})
	app := framework.NewApp(framework.WithI18n(i18n.NewTranslator(cat, "en")), framework.WithLocalePrefixes())
	app.Router().NotFound(h)
	return app.Router()
}

// linkHopPages draws the hop the way authui does: a page with a link to
// the target, and no meta refresh.
type linkHopPages struct{ *recordPages }

func (linkHopPages) Hop(w http.ResponseWriter, _ *http.Request, d HopPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><title>hop</title><a href="`+html.EscapeString(d.Target)+`">go</a>`)
}

var (
	hopBodyURL    = regexp.MustCompile(`(?:href="|http-equiv="refresh" content="0;url=)([^"]*)"`)
	hopRefreshURL = regexp.MustCompile(`^0;url=(.*)$`)
)

// hopURLs returns every URL a hop answer carries: the Refresh header's
// first, then the page's meta refresh and links.
func hopURLs(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	m := hopRefreshURL.FindStringSubmatch(w.Header().Get("Refresh"))
	if w.Code != http.StatusOK || m == nil {
		t.Fatalf("got %d Refresh %q, want the hop page: %s", w.Code, w.Header().Get("Refresh"), w.Body.String())
	}
	urls := []string{m[1]}
	for _, b := range hopBodyURL.FindAllStringSubmatch(w.Body.String(), -1) {
		urls = append(urls, html.UnescapeString(b[1]))
	}
	if len(urls) < 2 {
		t.Fatalf("the hop page carries no URL of its own: %s", w.Body.String())
	}
	return urls
}

// localeHopSite is one place the battery answers with the hop page.
type localeHopSite struct {
	name string
	// build returns the app, the request that hops (its path as the app
	// routes it, with no locale prefix) and the path the hop must load.
	build func(t *testing.T, pages Pages) (h http.Handler, req *http.Request, want string)
}

func localeHopSites() []localeHopSite {
	crossSite := func(target string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for k, v := range navigation("cross-site") {
			req.Header[k] = v
		}
		return req
	}
	verifyLink := func(t *testing.T, pages Pages) (*claimRig, string) {
		c := newClaimRig(t)
		c.mgr.config.Pages = pages
		const email, pw = "owner@example.com", "owner-chosen-pw-1"
		c.json("/auth/register", map[string]string{"email": email, "password": pw}, "")
		if w := c.json("/auth/send-verification", map[string]string{}, c.login(email, pw)); w.Code != http.StatusOK {
			t.Fatalf("send-verification: %d %s", w.Code, w.Body.String())
		}
		_, body := c.verifies.snapshot()
		return c, "/auth/verify-email?token=" + url.QueryEscape(extractTokenFromBody(body))
	}
	return []localeHopSite{
		{"session arrival", func(t *testing.T, pages Pages) (http.Handler, *http.Request, string) {
			mgr, _ := newTestManager(t)
			mgr.config.Pages = pages
			guarded := RequireSession()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "orders")
			}))
			const target = "/app/orders?status=open&q=two%20words"
			return SessionMiddleware(mgr, WithSessionLogger(nil))(guarded), crossSite(target), target
		}},
		{"email verification", func(t *testing.T, pages Pages) (http.Handler, *http.Request, string) {
			c, link := verifyLink(t, pages)
			return c.r, crossSite(link), link
		}},
		{"email verification without fetch metadata", func(t *testing.T, pages Pages) (http.Handler, *http.Request, string) {
			c, link := verifyLink(t, pages)
			return c.r, httptest.NewRequest(http.MethodGet, link, nil), link + "&gofastr_hop=1"
		}},
		{"oauth sign-in callback", func(t *testing.T, pages Pages) (http.Handler, *http.Request, string) {
			provider := &mockProvider{
				name:       "mock",
				tokenResp:  &OAuth2Token{AccessToken: "at"},
				userResp:   &OAuth2UserInfo{ID: "idp-1", Email: "new@example.com", EmailVerified: true},
				authURLFmt: "https://idp.example/authorize?state=%s",
			}
			mgr := New(AuthConfig{JWTSecret: "test-secret", SessionTTL: time.Hour, UserStore: newLinkingUserStore(), DevMode: true, Pages: pages})
			mgr.Use(NewOAuth2Plugin(OAuth2Config{Providers: map[string]OAuth2Provider{"mock": provider}, StateSecret: "test-secret-key-state"}))
			if err := mgr.Init(nil); err != nil {
				t.Fatal(err)
			}
			r := router.New()
			mgr.RegisterRoutes(r)
			req := startedFlow(t, r)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			return r, req, "/"
		}},
		{"oauth link callback", func(t *testing.T, pages Pages) (http.Handler, *http.Request, string) {
			_, r, store, mgr := linkFixture(t, &OAuth2UserInfo{ID: "prov", Email: "owner@example.com", Provider: "stub", EmailVerified: true})
			mgr.config.Pages = pages
			sess, err := mgr.SessionStore().Create(context.Background(), store.preExistingUser("owner@example.com").GetID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			start := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/link", nil)
			start.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, start)
			loc, _ := url.Parse(w.Header().Get("Location"))
			target := "/auth/oauth/stub/callback?state=" + url.QueryEscape(loc.Query().Get("state")) + "&code=c"
			req := crossSite(target)
			for _, c := range w.Result().Cookies() {
				req.AddCookie(c)
			}
			return r, req, target
		}},
	}
}

// A reader on /es/ who meets the hop page stays on /es/: the Refresh
// header and every URL in the page name the path the browser asked for,
// its locale prefix on once. Without it the reload left the reader's
// language, and a host with no unprefixed route for the path lost them.
func TestHopKeepsLocalePrefix(t *testing.T) {
	for _, site := range localeHopSites() {
		for _, pv := range []struct {
			name  string
			pages Pages
		}{{"plain page", nil}, {"host page", linkHopPages{&recordPages{}}}} {
			t.Run(site.name+"/"+pv.name, func(t *testing.T) {
				h, req, want := site.build(t, pv.pages)

				// With no prefix in the URL the hop loads the path as it is.
				plain := httptest.NewRecorder()
				h.ServeHTTP(plain, req.Clone(req.Context()))
				for _, u := range hopURLs(t, plain) {
					if u != want {
						t.Fatalf("unprefixed hop URL = %q, want %q", u, want)
					}
				}

				h, req, want = site.build(t, pv.pages)
				req.URL.Path = "/es" + req.URL.Path
				wantES := "/es" + strings.TrimSuffix(want, "/")
				rec := httptest.NewRecorder()
				underLocalePrefix(t, h).ServeHTTP(rec, req)
				for _, u := range hopURLs(t, rec) {
					if u != wantES {
						t.Errorf("hop URL under /es = %q, want %q\nRefresh: %s\n%s", u, wantES, rec.Header().Get("Refresh"), rec.Body.String())
					}
				}
			})
		}
	}
}

// The prefix goes on a same-origin path only: a request path that would
// leave the origin still gets no hop under a locale prefix.
func TestPrefixedHopIsNoOpenRedirect(t *testing.T) {
	mgr, _ := newTestManager(t)
	guarded := RequireSession()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h := underLocalePrefix(t, SessionMiddleware(mgr, WithSessionLogger(nil))(guarded))
	hops := 0
	for _, path := range []string{"/es//evil.example/x", "/es/%2Fevil.example/x", `/es/\evil.example/x`, "/es/https:/evil.example"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = path
		if strings.Contains(path, "%2F") {
			req.URL.Path, req.URL.RawPath = "/es//evil.example/x", path
		}
		for k, v := range navigation("cross-site") {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		for _, u := range append(hopBodyURLs(rec.Body.String()), strings.TrimPrefix(rec.Header().Get("Refresh"), "0;url=")) {
			if u == "" {
				continue
			}
			hops++
			if !strings.HasPrefix(u, "/es/") || strings.HasPrefix(u, "/es//") {
				t.Errorf("SECURITY: %q answered a hop to %q (status %d)", path, u, rec.Code)
			}
		}
	}
	if hops == 0 {
		t.Fatal("no path hopped: the check above saw nothing")
	}
}

func hopBodyURLs(body string) []string {
	var urls []string
	for _, b := range hopBodyURL.FindAllStringSubmatch(body, -1) {
		urls = append(urls, html.UnescapeString(b[1]))
	}
	return urls
}
