package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A record whose key holds "/" is linked with the slash escaped
// ("/orders/a%2Fb"); its screen answers the page and the client
// navigation, with the decoded key as the param.
func TestScreenServesKeyWithSlash(t *testing.T) {
	a := app.NewApp("slashkey")
	a.Register("/orders/{id}", &paramJSONComp{}, nil)
	ds := New(a)
	probe := &matchProbe{}
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(probe.middleware)
	ds.Mount(rt)

	for _, nav := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/orders/a%2Fb", nil)
		if nav {
			req.Header.Set("X-Gofastr-Navigate", "1")
		}
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<p>a/b</p>") {
			t.Fatalf("navigate=%v: %d %s", nav, rec.Code, rec.Body.String())
		}
		if got := probe.match.Param("id"); got != "a/b" {
			t.Errorf("navigate=%v: match id = %q, want a/b", nav, got)
		}
	}
}

// The hreflang alternates keep a key value's "/" escaped.
func TestLocaleLinksKeepEscapedSlash(t *testing.T) {
	links := localeLinks(i18n.PathLocales{}, "https://x.test", app.RoutePath("/orders/a%2Fb"))
	if got := links[len(links)-1].URL; got != "https://x.test/orders/a%2Fb" {
		t.Fatalf("x-default = %q", got)
	}
}
