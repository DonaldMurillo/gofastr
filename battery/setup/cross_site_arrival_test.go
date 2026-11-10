package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// crossSiteGet is a link to target clicked on another site's page: a
// top-level navigation the browser marks cross-site.
func crossSiteGet(h http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	h.ServeHTTP(w, req)
	return w
}

// The setup URL is printed in the startup log, and a hosted log viewer
// or a chat message makes it a link. A click there is a cross-site
// navigation: the browser stores the SameSite=Strict setup cookie the
// exchange sets and then withholds it from the redirect to /setup,
// because the navigation began on another site. The exchange spent the
// one-time token on that click, so /setup answered 403 and the logged
// link answered "token already used" from then on.
//
// A cross-site arrival gets a page from this origin that loads the same
// URL again. That navigation starts here, so the exchange and its
// redirect carry the cookie. The hop spends nothing.
func TestCrossSiteArrivalKeepsToken(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	link := "/setup?token=" + r.token

	w := crossSiteGet(h, link)
	if w.Code != http.StatusOK {
		t.Fatalf("cross-site arrival answered %d, want the hop page (200)", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("the hop set a cookie: the browser would withhold it from this navigation's redirect")
	}
	if got := w.Header().Get("Refresh"); got != "0;url="+link {
		t.Fatalf("Refresh = %q, want a reload of the same link from this origin", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q: the page's URL carries the token", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	// The link is in the page for a browser that ignores Refresh.
	if !strings.Contains(w.Body.String(), `href="`+link+`"`) {
		t.Fatalf("the hop page has no link to continue:\n%s", w.Body.String())
	}

	// The reload starts on this origin, and the token is still good.
	follow := httptest.NewRequest(http.MethodGet, link, nil)
	follow.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, follow)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the reload answered %d, want the exchange's 303: the hop spent the token", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == setupCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("the exchange set no setup cookie")
	}
	page := httptest.NewRequest(http.MethodGet, "/setup", nil)
	page.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, page)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup with the cookie answered %d, want the wizard", rec.Code)
	}
}

// The hop answers the same for a wrong token: it checks nothing, so it
// tells nobody whether a token is good.
func TestCrossSiteArrivalIsNoTokenOracle(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	good := crossSiteGet(h, "/setup?token="+r.token)
	bad := crossSiteGet(h, "/setup?token="+strings.Repeat("0", len(r.token)))
	if good.Code != bad.Code {
		t.Fatalf("good token %d, bad token %d: the hop tells them apart", good.Code, bad.Code)
	}
}

// A link that would leave this origin is not reloaded.
func TestCrossSiteArrivalRefusesForeignTarget(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/setup?token="+r.token, nil)
	req.URL.Path = "//evil.example/setup"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Refresh"); got != "" {
		t.Fatalf("Refresh = %q for a scheme-relative path", got)
	}
}
