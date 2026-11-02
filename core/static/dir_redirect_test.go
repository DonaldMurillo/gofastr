package static

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// resolved is where a browser goes after following rec's Location from
// reqURL.
func resolved(t *testing.T, reqURL string, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("GET %s = %d with no Location", reqURL, rec.Code)
	}
	base, err := url.Parse(reqURL)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Location %q does not parse: %v", loc, err)
	}
	return base.ResolveReference(ref)
}

// TestDirectoryWithoutSlashRedirects: a request for a directory that has
// an index file, spelled without the trailing slash, used to get the
// index body at /docs, so every relative link in it (style.css, ./intro)
// resolved against / instead of /docs/. It now answers 301 to /docs/,
// query kept, as net/http's FileServer does. The Location is relative
// (./docs/), so it lands right under the handler's own Prefix and under
// an outer http.StripPrefix that the handler never sees.
func TestDirectoryWithoutSlashRedirects(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":            {Data: []byte("root")},
		"docs/index.html":       {Data: []byte("docs index")},
		"docs/guide/index.html": {Data: []byte("guide")},
		"empty/readme.txt":      {Data: []byte("no index here")},
	}
	type tc struct {
		name    string
		h       http.Handler
		req     string
		wantLoc string // path?query after resolving Location
	}
	cases := []tc{
		{"root", Handler(Config{FS: fsys}), "/docs", "/docs/"},
		{"query kept", Handler(Config{FS: fsys}), "/docs?tab=a&x=1", "/docs/?tab=a&x=1"},
		{"nested", Handler(Config{FS: fsys}), "/docs/guide", "/docs/guide/"},
		{"prefix", Handler(Config{FS: fsys, Prefix: "/static"}), "/static/docs", "/static/docs/"},
		{"outer StripPrefix", http.StripPrefix("/assets", Handler(Config{FS: fsys})), "/assets/docs", "/assets/docs/"},
		{"SPA mode", Handler(Config{FS: fsys, SPA: true}), "/docs", "/docs/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := httptest.NewRecorder()
				c.h.ServeHTTP(rec, httptest.NewRequest(method, c.req, nil))
				if rec.Code != http.StatusMovedPermanently {
					t.Fatalf("%s %s = %d %q, want 301", method, c.req, rec.Code, rec.Body.String())
				}
				if got := resolved(t, "http://example.test"+c.req, rec); got.Host != "example.test" || got.RequestURI() != c.wantLoc {
					t.Errorf("%s %s redirects to %s, want http://example.test%s", method, c.req, got, c.wantLoc)
				}
			}
		})
	}

	// The slash form serves the index; a directory with no index is not
	// redirected (nothing is there to serve).
	h := Handler(Config{FS: fsys})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "docs index" {
		t.Errorf("GET /docs/ = %d %q, want the index", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/empty", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /empty (no index) = %d, want 404", rec.Code)
	}
}

// TestDirectoryRedirectStaysOnHost holds the redirect to the request's
// own origin. A Location built from the raw path would send //evil.com
// to evil.com, a browser reads /\evil.com and a relative \\evil.com/ the
// same way, and a relative http:evil.com/ parses as a scheme. Each
// spelling here names a real directory with an index, so only the
// Location's construction stands between the request and an open
// redirect.
func TestDirectoryRedirectStaysOnHost(t *testing.T) {
	fsys := fstest.MapFS{
		"evil.com/index.html":       {Data: []byte("x")},
		`\evil.com/index.html`:      {Data: []byte("x")},
		`\\evil.com/index.html`:     {Data: []byte("x")},
		"http:evil.com/index.html":  {Data: []byte("x")},
		"javascript:x()/index.html": {Data: []byte("x")},
	}
	h := Handler(Config{FS: fsys})
	for _, p := range []string{"//evil.com", "///evil.com", `/\evil.com`, `/\\evil.com`, "/http:evil.com", "/javascript:x()"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			continue // not redirected at all: nothing to hijack
		}
		loc := rec.Header().Get("Location")
		ref, err := url.Parse(loc)
		if err != nil {
			t.Errorf("path %q: Location %q does not parse: %v", p, loc, err)
			continue
		}
		// A browser folds \ into / in an http(s) URL before resolving.
		browser, _ := url.Parse(strings.ReplaceAll(loc, `\`, "/"))
		if ref.Scheme != "" || ref.Host != "" || strings.HasPrefix(loc, "//") || browser == nil || browser.Host != "" || browser.Scheme != "" {
			t.Errorf("SECURITY: [static] path %q redirects to %q, which leaves the origin", p, loc)
		}
	}
}

// TestDirectoryRedirectOnDisk runs the redirect through the os.Root
// branch the disk-backed FS takes.
func TestDirectoryRedirectOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "index.html"), []byte("docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(Config{FS: os.DirFS(dir)}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs?q=1", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /docs on disk = %d, want 301", rec.Code)
	}
	if got := resolved(t, "http://example.test/docs?q=1", rec); got.RequestURI() != "/docs/?q=1" {
		t.Errorf("redirect = %s, want /docs/?q=1", got)
	}
}
