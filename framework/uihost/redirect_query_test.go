package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A declarative redirect dropped the query string on both arms, so
// /old-pricing?plan=pro&utm_source=x landed on a bare /pricing.
func TestRedirectKeepsQuery(t *testing.T) {
	a := app.NewApp("redirects")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/pricing", &testHomeComp{}, nil)
	a.Redirect("/old-pricing", "/pricing")
	a.Redirect("/promo", "/pricing?plan=team")
	ds := New(a)

	for _, c := range []struct{ url, want string }{
		{"/old-pricing?plan=pro&utm_source=x", "/pricing?plan=pro&utm_source=x"},
		{"/old-pricing", "/pricing"},
		// The target's own keys win; the request's other pairs follow.
		{"/promo?plan=pro&utm_source=x", "/pricing?plan=team&utm_source=x"},
	} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != http.StatusPermanentRedirect {
			t.Fatalf("GET %s: got %d, want 308", c.url, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != c.want {
			t.Errorf("GET %s: Location = %q, want %q", c.url, loc, c.want)
		}

		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		rec = httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Gofastr-Location"); got != c.want {
			t.Errorf("nav %s: X-Gofastr-Location = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestCarryQueryKeepsFragmentLast(t *testing.T) {
	if got := carryQuery("/pricing#plans", "a=1"); got != "/pricing?a=1#plans" {
		t.Errorf("carryQuery = %q, want /pricing?a=1#plans", got)
	}
}
