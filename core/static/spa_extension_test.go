package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// TestSPAFallbackOnlyForExtensionlessPaths: the SPA fallback answers a
// client route with the index, and used to answer every miss that way,
// so a missing /app.3f2a.js (a fingerprint a newer replica links
// mid-deploy, a stale bundle name) came back 200 text/html. The browser
// then ran HTML as a script or a stylesheet and reported a syntax error
// far from the cause, and a CDN stored the shell under the asset's URL.
// A path whose last segment has a file extension now gets a 404; one
// without (a client route, dots in earlier segments included) still gets
// the index.
func TestSPAFallbackOnlyForExtensionlessPaths(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<p>shell</p>")},
		"app.abc123.js": {Data: []byte("js")},
	}
	for name, h := range map[string]http.Handler{
		"root":   Handler(Config{FS: fsys, SPA: true}),
		"prefix": Handler(Config{FS: fsys, SPA: true, Prefix: "/app"}),
	} {
		t.Run(name, func(t *testing.T) {
			pre := ""
			if name == "prefix" {
				pre = "/app"
			}
			for _, p := range []string{"/app.3f2a.js", "/x.css", "/img/x.png", "/deep/path/font.woff2", "/a.json"} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pre+p, nil))
				if rec.Code != http.StatusNotFound {
					t.Errorf("GET %s%s (missing file) = %d %q, want 404", pre, p, rec.Code, rec.Body.String())
				}
			}
			for _, p := range []string{"/dashboard/settings", "/users/42", "/v1.2/release", "/"} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pre+p, nil))
				if rec.Code != http.StatusOK || rec.Body.String() != "<p>shell</p>" {
					t.Errorf("GET %s%s (client route) = %d %q, want the shell", pre, p, rec.Code, rec.Body.String())
				}
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pre+"/app.abc123.js", nil))
			if rec.Code != http.StatusOK || rec.Body.String() != "js" {
				t.Errorf("GET an existing asset = %d %q", rec.Code, rec.Body.String())
			}
		})
	}
}
