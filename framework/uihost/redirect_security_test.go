package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// Redirect Location headers never carry raw C0/DEL bytes: the substituted
// target is built from percent-decoded request-path params, and net/http's
// http.Redirect hex-escapes only non-ASCII, so unscrubbed control bytes
// would land verbatim in the header value where they forge log lines and
// headers downstream. Both emit sites (the 308 pattern redirect and the
// trailing-slash 301) scrub through scrubCtl, the contract
// sanitizeHeaderValue pins for every other outbound header.

// TestRedirect308LocationScrubbed: a pattern-redirect target substituted
// with control bytes still 308s, percent-encoded.
func TestRedirect308LocationScrubbed(t *testing.T) {
	a := app.NewApp("redirect-ctl")
	a.Register("/", &testHomeComp{}, nil)
	a.RedirectPattern("/old/{rest...}", "/new/{rest...}")
	a.Register("/new/{rest...}", &paramJSONComp{}, nil)
	ds := New(a)

	req := httptest.NewRequest(http.MethodGet, "/old/a%01b%7fc", nil)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusPermanentRedirect || loc == "" {
		t.Fatalf("setup: expected 308 + Location from the pattern redirect, got %d %q (body %.200s)",
			rec.Code, loc, rec.Body.String())
	}
	for i := range len(loc) {
		c := loc[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			t.Errorf("SECURITY: [uihost] 308 Location %q carries raw control byte 0x%02x at %d — "+
				"percent-decoded request-path params flow through substituteRedirect into the "+
				"Location header; the emit site must scrubCtl them like every other header sink",
				loc, c, i)
			break
		}
	}
}

// TestTrailingSlashLocationScrubbed: the same scrub on the trailing-slash
// 301, whose target is the raw request path plus "/". The route itself is
// registered with the control byte so the slash-form resolves and the
// redirect arm fires with hostile bytes in the target.
func TestTrailingSlashLocationScrubbed(t *testing.T) {
	a := app.NewApp("slash-ctl")
	a.Register("/", &testHomeComp{}, nil)
	a.RegisterScreen(app.NewScreen("/fo\x01o/", &testHomeComp{}).WithTitle("Foo"), nil)
	ds := New(a)

	req := httptest.NewRequest(http.MethodGet, "/fo%01o", nil)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusMovedPermanently || loc == "" {
		t.Fatalf("setup: expected 301 + Location from the trailing-slash redirect, got %d %q (body %.200s)",
			rec.Code, loc, rec.Body.String())
	}
	for i := range len(loc) {
		c := loc[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			t.Errorf("SECURITY: [uihost] trailing-slash Location %q carries raw control byte 0x%02x at %d — "+
				"the redirect target is built from the percent-decoded request path and must be "+
				"scrubbed before it reaches the Location header", loc, c, i)
			break
		}
	}
}
