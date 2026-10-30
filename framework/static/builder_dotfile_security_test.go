package static

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The export publishes what the UI host serves: no dot segment and no
// server config file from the static directory, static FS or an extra
// dir, while /.well-known/ and the root .nojekyll still ship. copyDir
// walked every file into the public tree, so a .env beside the assets
// was published.
func TestExportSkipsDotfiles(t *testing.T) {
	files := map[string]string{
		".env":                     "SECRET=1", // not-a-secret: fixture body
		".git/config":              "[core]",
		"assets/.htpasswd":         "admin:x",
		"Web.Config":               "<configuration/>",
		".well-known/.hidden":      "hidden",
		".well-known/security.txt": "Contact: mailto:security@example.com",
		".nojekyll":                "",
		"logo.svg":                 "<svg/>",
	}
	refused := []string{".env", ".git/config", "assets/.htpasswd", "Web.Config", ".well-known/.hidden"}
	published := []string{".well-known/security.txt", ".nojekyll", "logo.svg"}

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
	newApp := func() *coreapp.App {
		a := coreapp.NewApp("Dotfile Export")
		a.Register("/home", &homeScreen{}, nil)
		return a
	}
	builders := map[string]func(out string) *Builder{
		"dir": func(out string) *Builder {
			return &Builder{Host: uihost.New(newApp(), uihost.WithStaticDir(dir)), OutDir: out}
		},
		"fs": func(out string) *Builder {
			host := uihost.New(newApp())
			host.SetStaticFS(mapFS)
			return &Builder{Host: host, OutDir: out}
		},
		"extra": func(out string) *Builder {
			return &Builder{Host: uihost.New(newApp()), OutDir: out, ExtraDirs: map[string]fs.FS{"/": mapFS}}
		},
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			if _, err := build(out).Build(context.Background()); err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, rel := range refused {
				if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
					t.Errorf("SECURITY: [static] the export published %s", rel)
				}
			}
			for _, rel := range published {
				if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
					t.Errorf("the export left out %s: %v", rel, err)
				}
			}
		})
	}
}
