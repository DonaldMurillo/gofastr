package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A tab, CR or LF in a Location is deleted by the browser's URL parser
// before it parses the value (WHATWG URL, "remove all ASCII tab or
// newline"). A catch-all RedirectPattern that substitutes the decoded
// request path into "/{rest...}" therefore turned GET /old/%09/evil.example
// into Location "/\t/evil.example", which a browser follows to
// //evil.example: an open redirect. Three layers close it, and each is
// pinned here: core-ui/app refuses the substituted target, the 308 emit
// site gates it with handler.IsSafeRelativePath, and scrubCtl percent-
// encodes a tab instead of passing it through.

// browserLocation applies the WHATWG URL parser's first step: every ASCII
// tab and newline is removed from the input before parsing.
func browserLocation(loc string) string {
	return strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(loc)
}

func tabRedirectHost() *UIHost {
	a := app.NewApp("redirect-tab")
	a.Register("/", &testHomeComp{}, nil)
	a.RedirectPattern("/old/{rest...}", "/{rest...}")
	return New(a)
}

func assertNotOffOrigin(t *testing.T, where, loc string) {
	t.Helper()
	got := browserLocation(loc)
	if strings.HasPrefix(got, "//") || strings.HasPrefix(got, "/\\") || strings.Contains(got, "://") {
		t.Errorf("SECURITY: [uihost] %s: Location %q reads as %q once a browser strips tab/CR/LF: off-origin redirect", where, loc, got)
	}
	if strings.ContainsAny(loc, "\t\r\n") {
		t.Errorf("SECURITY: [uihost] %s: Location %q carries a raw tab/CR/LF", where, loc)
	}
}

func TestRedirectPatternTabIsNotOpenRedirect(t *testing.T) {
	ds := tabRedirectHost()
	for _, p := range []string{"/old/%09/evil.example", "/old/%0a/evil.example", "/old/%0d/evil.example", "/old/%09%09/evil.example"} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assertNotOffOrigin(t, "GET "+p, rec.Header().Get("Location"))
		if rec.Code == http.StatusPermanentRedirect {
			t.Errorf("SECURITY: [uihost] GET %s: 308 to %q; a control byte in the substituted target must fail closed", p, rec.Header().Get("Location"))
		}

		// The navigation arm (X-Gofastr-Location) refuses it too.
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		nav := httptest.NewRecorder()
		ds.ServeHTTP(nav, req)
		if loc := nav.Header().Get("X-Gofastr-Location"); loc != "" {
			t.Errorf("SECURITY: [uihost] navigate %s: X-Gofastr-Location %q for a control-byte target", p, loc)
		}
	}

	// The safe passthrough still redirects.
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/old/docs/intro", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/docs/intro" {
		t.Errorf("GET /old/docs/intro: want 308 /docs/intro, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// The same request through a real server: the bytes on the wire, not a
// recorder's header map.
func TestRedirectPatternTabOnTheWire(t *testing.T) {
	srv := httptest.NewServer(tabRedirectHost())
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/old/%09/evil.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assertNotOffOrigin(t, "wire", resp.Header.Get("Location"))
	if resp.StatusCode == http.StatusPermanentRedirect {
		t.Errorf("SECURITY: [uihost] wire: 308 to %q for a tab-bearing target", resp.Header.Get("Location"))
	}
}

// scrubCtl is the last layer: whatever reaches it, a tab leaves encoded.
func TestScrubCtlEncodesTab(t *testing.T) {
	for in, want := range map[string]string{
		"/\t/evil.example": "/%09/evil.example",
		"/a\nb\rc":         "/a%0ab%0dc",
		"/a\x7fb":          "/a%7fb",
		"/plain":           "/plain",
	} {
		if got := scrubCtl(in); got != want {
			t.Errorf("scrubCtl(%q) = %q, want %q", in, got, want)
		}
	}
}
