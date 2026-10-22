package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The default locale's 308 never sends the reader off the site: the
// path after /en/ was the redirect target as written, so /en//evil.com
// answered Location: //evil.com, and /en/%5Cevil.com answered /\evil.com,
// which browsers also read as a host.
func TestDefaultPrefixRedirectStaysLocal(t *testing.T) {
	for target, want := range map[string]string{
		"/en//evil.com":         "/",
		"/en/%2F%2Fevil.com":    "/",
		"/en/%5Cevil.com":       "/",
		"/en///evil.com/x?q=1":  "/",
		"/en/docs/a%3Fb?x=1":    "/docs/a%3Fb?x=1",
		"/en/caf%C3%A9?menu=on": "/caf%C3%A9?menu=on",
		// An escaped tag names the locale too; the target is never the
		// request itself, or the 308 loops.
		"/%65n/page?x=1": "/page?x=1",
		"/%65n":          "/",
	} {
		rec := servePrefixed(prefixApp(t), httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
			t.Errorf("%s: got %d Location %q, want %q", target, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}
