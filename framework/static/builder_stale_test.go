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

// A page exported while public and gated later must leave a rebuild
// into the same OutDir: the old file kept the private page published.
// A file the builder never wrote stays.
func TestRebuildPrunesGatedPage(t *testing.T) {
	gated := false
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.RegisterScreen(coreapp.NewScreen("/private", &homeScreen{}).
		WithPolicy(coreapp.PolicyFunc(func(context.Context) coreapp.Decision {
			if gated {
				return coreapp.Decision{Kind: coreapp.DecisionBlock, Status: 403}
			}
			return coreapp.Decision{Kind: coreapp.DecisionAllow}
		})), nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	page := filepath.Join(out, "private", "index.html")
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("premise: first build wrote no /private: %v", err)
	}
	foreign := filepath.Join(out, "private", "notes.txt")
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	gated = true
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if _, err := os.Stat(page); err == nil {
		t.Errorf("rebuild kept the gated page %s", page)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("rebuild lost the public page: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("rebuild removed a file it never wrote: %v", err)
	}
}

// shrinkingProducts lists alpha and beta until drop is set, then alpha.
type shrinkingProducts struct {
	productScreen
	drop *bool
}

func (p *shrinkingProducts) StaticPaths(context.Context) []map[string]string {
	if *p.drop {
		return []map[string]string{{"slug": "alpha"}}
	}
	return []map[string]string{{"slug": "alpha"}, {"slug": "beta"}}
}

// A page the rebuild no longer lists goes with its llm.md, and a file
// ExtraDirs now supplies at a pruned page's path stays.
func TestRebuildPrunesDroppedPageAndDoc(t *testing.T) {
	drop := false
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &shrinkingProducts{drop: &drop}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	beta := filepath.Join(out, "products", "beta")
	for _, f := range []string{"index.html", "llm.md"} {
		if _, err := os.Stat(filepath.Join(beta, f)); err != nil {
			t.Fatalf("premise: first build wrote no beta %s: %v", f, err)
		}
	}

	drop = true
	b.ExtraDirs = map[string]fs.FS{"/products/beta": fstest.MapFS{"index.html": {Data: []byte("ARCHIVED")}}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(beta, "llm.md")); err == nil {
		t.Error("rebuild kept the dropped page's llm.md")
	}
	if got, err := os.ReadFile(filepath.Join(beta, "index.html")); err != nil || string(got) != "ARCHIVED" {
		t.Errorf("ExtraDirs file at the dropped path = %q, %v; want ARCHIVED", got, err)
	}
}
