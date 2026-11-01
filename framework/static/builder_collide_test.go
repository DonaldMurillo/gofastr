package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// slashProducts exports one record whose key holds a slash: it links as
// /products/a%2Fb and lands on products/a/b/index.html.
type slashProducts struct{ productScreen }

func (*slashProducts) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"slug": "a/b"}}
}

// Two routes whose pages map to one file refuse the export instead of
// the later page silently replacing the earlier one.
func TestExportRefusesSharedPageFile(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/products/:slug", &slashProducts{}, nil)
	a.Register("/products/a/b", &homeScreen{}, nil)
	b := &Builder{Host: uihost.New(a), OutDir: t.TempDir()}
	_, err := b.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "products/a/b/index.html") {
		t.Fatalf("Build error = %v, want a refusal naming products/a/b/index.html", err)
	}
}

// caseProducts exports two records whose keys differ only in case.
type caseProducts struct{ productScreen }

func (*caseProducts) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"slug": "A"}, {"slug": "a"}}
}

// Two pages whose files differ only in case are one file on a
// case-insensitive filesystem (APFS, NTFS by default): the later one
// replaced the earlier, so the export refuses them too.
func TestExportRefusesCaseFoldedFile(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/products/:slug", &caseProducts{}, nil)
	b := &Builder{Host: uihost.New(a), OutDir: t.TempDir()}
	_, err := b.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "both export to") {
		t.Fatalf("Build error = %v, want a refusal of /products/A beside /products/a", err)
	}
}

// A redirect whose file differs from a page's only in case would replace
// that page on a case-insensitive filesystem, and so would a second
// redirect: the export refuses both.
func TestExportRefusesCaseFoldedRedirect(t *testing.T) {
	for name, setup := range map[string]func(a *coreapp.App){
		"page and redirect": func(a *coreapp.App) {
			a.Register("/About", &homeScreen{}, nil)
			a.Redirect("/about", "/About")
		},
		"two redirects": func(a *coreapp.App) {
			a.Register("/next", &homeScreen{}, nil)
			a.Redirect("/old", "/next")
			a.Redirect("/OLD", "/next")
		},
	} {
		a := coreapp.NewApp("t")
		setup(a)
		b := &Builder{Host: uihost.New(a), OutDir: t.TempDir()}
		if _, err := b.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "both export to") {
			t.Errorf("%s: Build error = %v, want a refusal of the case-folded pair", name, err)
		}
	}
}
