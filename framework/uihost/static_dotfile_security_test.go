package uihost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// The app's static directory and static FS refuse what core/static
// refuses, a dot segment or a server config file, and still serve
// /.well-known/, the public namespace RFC 8615 reserves. serveOrRender
// opened whatever path.Clean left, so GET /.env returned the file.
func TestStaticRefusesDotfiles(t *testing.T) {
	files := map[string]string{
		".env":                     "SECRET=1", // not-a-secret: fixture body
		".git/config":              "[core]",
		"assets/.htpasswd":         "admin:x",
		"Web.Config":               "<configuration/>",
		".well-known/.hidden":      "hidden",
		".well-known/security.txt": "Contact: mailto:security@example.com",
		"nav.js":                   "console.log('nav')",
	}
	refused := []string{"/.env", "/.git/config", "/assets/.htpasswd", "/Web.Config", "/.well-known/.hidden"}
	served := []string{"/.well-known/security.txt", "/nav.js"}

	dir := t.TempDir()
	mapFS := fstest.MapFS{}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		mapFS[name] = &fstest.MapFile{Data: []byte(body)}
	}
	hosts := map[string]func() *UIHost{
		"dir": func() *UIHost {
			a := app.NewApp("dotfile-test")
			a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
			return New(a, WithStaticDir(dir))
		},
		"fs": func() *UIHost {
			a := app.NewApp("dotfile-test")
			a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
			ds := New(a)
			ds.SetStaticFS(mapFS)
			return ds
		},
	}
	for name, build := range hosts {
		t.Run(name, func(t *testing.T) {
			ds := build()
			for _, p := range refused {
				rec := httptest.NewRecorder()
				ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
				if rec.Code == http.StatusOK {
					t.Errorf("SECURITY: [static] GET %s = 200 %q", p, rec.Body.String())
				}
				if ds.resolvesStaticOrScreen(httptest.NewRequest(http.MethodGet, p, nil)) {
					t.Errorf("resolvesStaticOrScreen(%s) = true, serveOrRender refuses it", p)
				}
			}
			for _, p := range served {
				rec := httptest.NewRecorder()
				ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
				if rec.Code != http.StatusOK {
					t.Errorf("GET %s = %d, want 200", p, rec.Code)
				}
			}
		})
	}
}
