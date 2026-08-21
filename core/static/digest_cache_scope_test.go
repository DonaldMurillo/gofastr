package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// An embed.FS reports the zero time.Time as the modtime of EVERY file, so a
// process-wide digest cache keyed on (name, modtime, size) collapses to
// (name, size). Two handlers over different filesystems, a site embed and
// an admin embed, both serving "app.css", would then share an ETag, and
// the second handler would answer 304 Not Modified for content the client
// has never seen. The cache is per-handler for exactly this reason.
func TestDigestCacheDoesNotLeakAcrossHandlers(t *testing.T) {
	// Same name, same size, same (zero) modtime, different content.
	site := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{color:red}")}}
	admin := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{color:tan}")}}

	etagOf := func(h http.Handler) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("first request: got %d, want 200", rec.Code)
		}
		return rec.Header().Get("ETag")
	}

	siteHandler := Handler(Config{FS: site})
	adminHandler := Handler(Config{FS: admin})

	siteETag := etagOf(siteHandler)
	adminETag := etagOf(adminHandler)

	if siteETag == adminETag {
		t.Fatalf("different content shares an ETag (%s) — the digest cache is not scoped to its filesystem", siteETag)
	}

	// The concrete harm: the site's ETag must not validate admin content.
	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	req.Header.Set("If-None-Match", siteETag)
	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotModified {
		t.Fatal("admin handler answered 304 to the site handler's ETag — the client keeps stale content")
	}
}

// Within one handler the cache must still work: a repeat request is served
// from the memoised digest rather than re-hashing the file.
func TestDigestCacheServesRepeatRequests(t *testing.T) {
	fsys := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{color:red}")}}
	h := Handler(Config{FS: fsys})

	get := func() (int, string, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
		return rec.Code, rec.Header().Get("ETag"), rec.Body.String()
	}

	code1, etag1, body1 := get()
	code2, etag2, body2 := get()

	if code1 != http.StatusOK || code2 != http.StatusOK {
		t.Fatalf("got %d then %d, want 200 twice", code1, code2)
	}
	if etag1 != etag2 {
		t.Errorf("ETag changed between requests: %s then %s", etag1, etag2)
	}
	// The cache-hit path does not consume the handle, so the body must
	// still be complete on the second request.
	if body1 != body2 || body2 != "body{color:red}" {
		t.Errorf("body differs across requests: %q then %q", body1, body2)
	}
}

// fs.ValidPath rejects any name that is not valid UTF-8, so os.DirFS and
// embed.FS answer fs.ErrInvalid, not fs.ErrNotExist, for a URL like /%ff.
// That is a malformed request, not a server fault: answering 500 let any
// client drive a 5xx with a two-character URL, and skipped SPA fallback.
func TestInvalidPathIsNotAServerError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("app shell"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		spa  bool
		want int
	}{
		{"plain handler 404s", false, http.StatusNotFound},
		{"SPA handler falls back to the index", true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler(Config{FS: os.DirFS(dir), SPA: tc.spa})
			for _, p := range []string{"/\xff", "/\xc3\x28"} {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.URL.Path = p
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != tc.want {
					t.Errorf("path %q: got %d, want %d", p, rec.Code, tc.want)
				}
			}
		})
	}
}
