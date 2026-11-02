package static

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The export publishes what the UI host serves from the static dir, and
// the UI host reads that dir through os.Root: a symlink pointing out of
// the tree answers 404 there. copyDir followed it with os.Open and
// published the target's bytes into the public tree, so
// static/config.json -> ../config/prod.json (or a key, an .env under a
// harmless name) went out with the site. A symlink that stays in the
// tree is served live and still ships. So does a symlinked directory
// that stays in the tree (static/images -> img): the live host follows
// it through os.Root, and an export that dropped it 404'd on every
// asset under it. A directory link that escapes is refused like a file
// link, and a link back up the tree (loop -> .) is not walked forever.
func TestExportRefusesSymlinkOutOfStaticDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "static")
	outside := filepath.Join(base, "private")
	for _, d := range []string{dir, outside, filepath.Join(dir, "img")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(outside, "prod.json"), `{"db":"postgres://admin:pw@db/prod"}`)
	write(filepath.Join(dir, "app.css"), "body{}")
	write(filepath.Join(dir, "img", "logo.svg"), "<svg/>")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	link(filepath.Join("..", "private", "prod.json"), "config.json") // escapes
	link(filepath.Join(outside, "prod.json"), "abs.json")            // escapes, absolute
	link("app.css", "theme.css")                                     // stays in the tree
	link("img", "images")                                            // a directory in the tree
	link(outside, "secrets")                                         // a directory out of it
	link(".", "loop")                                                // a cycle

	for name, host := range map[string]func() *uihost.UIHost{
		"dir": func() *uihost.UIHost { return uihost.New(newSymlinkApp(), uihost.WithStaticDir(dir)) },
		"fs": func() *uihost.UIHost {
			h := uihost.New(newSymlinkApp())
			h.SetStaticFS(os.DirFS(dir))
			return h
		},
	} {
		t.Run(name, func(t *testing.T) {
			// The parity this test holds the export to: the live host
			// refuses the escaping links and serves the in-tree one.
			live := host()
			for u, want := range map[string]int{
				"/config.json": http.StatusNotFound, "/abs.json": http.StatusNotFound, "/secrets/prod.json": http.StatusNotFound,
				"/theme.css": http.StatusOK, "/images/logo.svg": http.StatusOK,
			} {
				rec := httptest.NewRecorder()
				live.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
				if rec.Code != want {
					t.Fatalf("live GET %s = %d, want %d", u, rec.Code, want)
				}
			}
			out := t.TempDir()
			if _, err := (&Builder{Host: host(), OutDir: out}).Build(context.Background()); err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, rel := range []string{"config.json", "abs.json", filepath.Join("secrets", "prod.json")} {
				if b, err := os.ReadFile(filepath.Join(out, rel)); err == nil {
					t.Errorf("SECURITY: [static] the export published %s, a symlink out of the static dir (%q); the live host answers 404 for it", rel, b)
				}
			}
			if b, err := os.ReadFile(filepath.Join(out, "theme.css")); err != nil || string(b) != "body{}" {
				t.Errorf("theme.css (an in-tree symlink) = %q, %v; want body{}", b, err)
			}
			if fi, err := os.Lstat(filepath.Join(out, "images")); err == nil && !fi.IsDir() {
				t.Errorf("the export wrote images as a %v file; a symlinked directory is not a file", fi.Mode())
			}
			if b, err := os.ReadFile(filepath.Join(out, "images", "logo.svg")); err != nil || string(b) != "<svg/>" {
				t.Errorf("images/logo.svg (under an in-tree directory symlink the live host serves) = %q, %v; want <svg/>", b, err)
			}
			// The cycle is cut at its first link: without the cut the
			// walk recursed until the kernel's symlink limit and shipped
			// loop/app.css, loop/loop/app.css, ... dozens deep.
			if _, err := os.Lstat(filepath.Join(out, "loop")); err == nil {
				t.Errorf("the export walked loop -> . (a link back up the tree) and published copies under loop/")
			}
		})
	}
}

func newSymlinkApp() *coreapp.App {
	app := coreapp.NewApp("Symlink Export")
	app.Register("/home", &homeScreen{}, nil)
	return app
}
