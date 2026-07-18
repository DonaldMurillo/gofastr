package static

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdkdocs"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type staticTestRegistry struct{ entities []*entity.Entity }

func (f *staticTestRegistry) All() map[string]*entity.Entity {
	out := map[string]*entity.Entity{}
	for _, e := range f.entities {
		out[e.Config.Name] = e
	}
	return out
}
func (f *staticTestRegistry) AllSorted() []*entity.Entity { return f.entities }
func (f *staticTestRegistry) Get(name string) (*entity.Entity, error) {
	for _, e := range f.entities {
		if e.Config.Name == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("no entity %q", name)
}

// The SDK docs site exports like any other screens (dynamic entity pages
// expand via StaticPaths) and ExtraDirs ships the downloadable artifacts
// into the tree — without clobbering files the export already owns.
func TestBuildExportsSDKDocsAndExtraDirs(t *testing.T) {
	reg := &staticTestRegistry{entities: []*entity.Entity{
		entity.Define("posts", entity.EntityConfig{
			Public: true,
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}),
	}}

	a := coreapp.NewApp("SDK Export")
	a.Register("/", &homeScreen{}, nil)
	if err := sdkdocs.Mount(a, router.New(), sdkdocs.Config{Registry: reg}); err != nil {
		t.Fatal(err)
	}
	host := uihost.New(a)

	out := t.TempDir()
	b := &Builder{
		Host:   host,
		OutDir: out,
		ExtraDirs: map[string]fs.FS{
			"/docs/api/sdk": fstest.MapFS{
				"go.zip":        {Data: []byte("zip-bytes")},
				"manifest.json": {Data: []byte(`{"schemaVersion":1,"artifacts":{"go":{"file":"sdk-go.zip","sha256":"aa"}}}`)},
			},
		},
	}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}

	index := readOut(t, out, "docs/api/index.html")
	if !strings.Contains(index, "SDKs") {
		t.Error("docs index page missing from export")
	}
	entityPage := readOut(t, out, "docs/api/entities/posts/index.html")
	if !strings.Contains(entityPage, "title") {
		t.Error("entity reference page missing from export")
	}
	if got := readOut(t, out, "docs/api/sdk/go.zip"); got != "zip-bytes" {
		t.Errorf("artifact not copied: %q", got)
	}
}

func TestBuildExtraDirsNeverClobber(t *testing.T) {
	a := coreapp.NewApp("Clobber")
	a.Register("/", &homeScreen{}, nil)
	staticDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staticDir, "dl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "dl", "file.txt"), []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := uihost.New(a, uihost.WithStaticDir(staticDir))

	out := t.TempDir()
	b := &Builder{
		Host:   host,
		OutDir: out,
		ExtraDirs: map[string]fs.FS{
			"/dl": fstest.MapFS{"file.txt": {Data: []byte("extra-owned")}},
		},
	}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readOut(t, out, "dl/file.txt"); got != "user-owned" {
		t.Errorf("extra dir clobbered a user static file: %q", got)
	}
}
