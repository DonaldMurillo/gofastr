package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A directory in the static FS (SetStaticFS, the embed.FS path) is no
// static file. It was handed to http.ServeFileFS, which answers a
// directory with a 301 to "docs/"; the trailing slash then fails
// fs.ValidPath and lands on the 404 page, so a screen sharing the
// directory's name was unreachable. The static-dir tree (WithStaticDir)
// already skipped directories; the FS branch and its MethodNotAllowed
// mirror (resolvesStaticOrScreen) now do too.
func TestStaticFSDirectoryFallsThroughToScreen(t *testing.T) {
	a := app.NewApp("fsdir")
	a.Register("/", &testHomeComp{}, nil)
	a.RegisterScreen(app.NewScreen("/docs", &textComp{tag: "docs-screen"}).WithTitle("Docs"), nil)
	ds := New(a)
	ds.SetStaticFS(fstest.MapFS{
		"docs/diagram.png":    {Data: []byte("png")},
		"docs/internal.json":  {Data: []byte("{}")},
		"assets/app.css":      {Data: []byte("body{}")},
		"assets/nested/x.txt": {Data: []byte("x")},
	})

	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}

	if rec := get("/docs"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "docs-screen") {
		t.Errorf("GET /docs: want the registered screen (200), got %d Location=%q", rec.Code, rec.Header().Get("Location"))
	}
	// A directory with no screen is a plain 404, never a redirect or a
	// listing.
	for _, p := range []string{"/assets", "/assets/nested", "/docs/"} {
		rec := get(p)
		if rec.Code == http.StatusMovedPermanently || rec.Header().Get("Location") != "" {
			t.Errorf("GET %s: %d Location=%q; a static directory must not redirect", p, rec.Code, rec.Header().Get("Location"))
		}
		if body := rec.Body.String(); strings.Contains(body, "internal.json") || strings.Contains(body, "app.css") {
			t.Errorf("GET %s lists the static directory", p)
		}
	}
	// Files inside the directory still serve.
	if rec := get("/docs/diagram.png"); rec.Code != http.StatusOK || rec.Body.String() != "png" {
		t.Errorf("GET /docs/diagram.png: got %d %q", rec.Code, rec.Body.String())
	}

	// The MethodNotAllowed fall-through predicate agrees.
	for p, want := range map[string]bool{
		"/docs":             true, // the screen
		"/assets":           false,
		"/assets/app.css":   true,
		"/docs/diagram.png": true,
	} {
		if got := ds.resolvesStaticOrScreen(httptest.NewRequest(http.MethodGet, p, nil)); got != want {
			t.Errorf("resolvesStaticOrScreen(%s) = %v, want %v", p, got, want)
		}
	}
}
