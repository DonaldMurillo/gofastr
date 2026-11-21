package static

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type sharedCatalog struct{ slugs []string }

type catalogScreen struct{ cat *sharedCatalog }

func (s *catalogScreen) SetParams(map[string]string) {}
func (s *catalogScreen) Render() render.HTML         { return "<p>doc</p>" }
func (s *catalogScreen) StaticPaths(context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(s.cat.slugs))
	for _, slug := range s.cat.slugs {
		out = append(out, map[string]string{"slug": slug})
	}
	return out
}

type catalogContributor struct{ cat *sharedCatalog }

func (c *catalogContributor) ExportStart(context.Context) error {
	c.cat.slugs = []string{"intro"}
	return nil
}
func (c *catalogContributor) ExportPage(string, string) error { return nil }
func (c *catalogContributor) ExportFiles(context.Context) (map[string][]byte, error) {
	return nil, nil
}

func TestExportStartRunsBeforeStaticPaths(t *testing.T) {
	cat := &sharedCatalog{}
	a := coreapp.NewApp("catalog")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/docs/:slug", &catalogScreen{cat: cat}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out, Contributors: []Contributor{&catalogContributor{cat: cat}}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "docs", "intro", "index.html")); err != nil {
		t.Errorf("page from the catalog ExportStart filled was not exported: %v", err)
	}
}
