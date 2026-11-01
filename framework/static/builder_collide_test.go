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
