package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

// TestSPAFallbackIsNotCachedUnderMaxAge: the SPA fallback answers a URL
// that names no file with the index, under the handler's MaxAge. A host
// that sets a long MaxAge for its fingerprinted assets then let a browser
// or CDN keep the app shell, as text/html, at any URL for that long:
// during a rolling deploy a page from a newer replica asks for
// app.<new>.js, lands on an older one, and the shell is pinned under the
// asset's name until it expires. The fallback revalidates every time
// (no-cache keeps the index's ETag, so it is a body-less 304); a file
// that exists keeps MaxAge. (A missing asset with an extension now
// gets a 404 instead of the shell; TestSPAFallbackOnlyForExtensionlessPaths
// holds that. The no-cache rule still covers every client route.)
func TestSPAFallbackIsNotCachedUnderMaxAge(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<p>shell</p>")},
		"app.abc123.js": {Data: []byte("js")},
	}
	h := Handler(Config{FS: fsys, SPA: true, MaxAge: 365 * 24 * time.Hour})

	for _, u := range []string{"/users/42", "/dashboard/settings"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "<p>shell</p>" {
			t.Fatalf("GET %s = %d %q, want the SPA index", u, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s (SPA fallback) Cache-Control = %q, want no-cache: the shell must not be stored under a URL that names no file", u, cc)
		}
	}
	for _, u := range []string{"/app.abc123.js", "/", "/index.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000" {
			t.Errorf("GET %s Cache-Control = %q, want MaxAge for a file that exists", u, cc)
		}
	}
}
