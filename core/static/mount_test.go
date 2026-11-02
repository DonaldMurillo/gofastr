package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestMountWithPrefixServes pins Mount under a non-root prefix, the
// spelling docs/porting.md shows. Mount used to register both
// "GET /static/" and "GET /static/{path...}", which ServeMux treats as the
// same pattern, so every prefixed Mount panicked at boot with a "route
// conflict". A trailing slash on the prefix ("/static/") panicked too, on
// an unclean "/static//" pattern.
func TestMountWithPrefixServes(t *testing.T) {
	fsys := fstest.MapFS{
		"app.js":     {Data: []byte("js")},
		"index.html": {Data: []byte("<p>index</p>")},
	}
	for _, prefix := range []string{"/static", "static", "/static/"} {
		t.Run(prefix, func(t *testing.T) {
			r := router.New()
			func() {
				defer func() {
					if v := recover(); v != nil {
						t.Fatalf("Mount(Prefix %q) panicked: %v", prefix, v)
					}
				}()
				Mount(r, Config{FS: fsys, Prefix: prefix})
			}()
			for _, tc := range []struct {
				url, body string
			}{
				{"/static/app.js", "js"},
				{"/static/", "<p>index</p>"},
			} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
				if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
					t.Errorf("GET %s = %d %q, want 200 %q", tc.url, rec.Code, rec.Body.String(), tc.body)
				}
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static", nil))
			if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/static/" {
				t.Errorf("GET /static = %d Location %q, want 307 to /static/", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

// TestHandlerStripsPrefixOnSegmentBoundary pins Prefix as a path-segment
// boundary. The handler used to strip it with a bare TrimPrefix, so with
// Prefix "/static" a request for "/staticsecret.txt" served secret.txt, a
// file the URL never named. (A path with no prefix at all is still served
// from the root, so the handler works behind http.StripPrefix.)
func TestHandlerStripsPrefixOnSegmentBoundary(t *testing.T) {
	fsys := fstest.MapFS{
		"secret.txt": {Data: []byte("s")},
		"app.js":     {Data: []byte("js")},
	}
	h := Handler(Config{FS: fsys, Prefix: "/static"})
	for _, u := range []string{"/staticsecret.txt", "/static-secret.txt", "/other/app.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d %q, want 404: no such file once the prefix is read on a segment boundary", u, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "js" {
		t.Errorf("GET /static/app.js = %d %q, want 200 js", rec.Code, rec.Body.String())
	}
}
