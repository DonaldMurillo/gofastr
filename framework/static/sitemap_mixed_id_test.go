package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// mixedIDs lists one ID per spelling: ASCII with a percent, Unicode,
// and both.
type mixedIDs struct{ productScreen }

func (*mixedIDs) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"slug": "a%"}, {"slug": "café"}, {"slug": "café%"}}
}

// An exported page stays in the exported sitemap whatever its ID holds:
// "café%" matched neither the escaped loc nor its plain unescape, so the
// sitemap dropped a page the export wrote.
func TestSitemapKeepsMixedIDPage(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &mixedIDs{}, nil)
	host := uihost.New(a, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://x.test"}))
	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 4 {
		t.Fatalf("premise: exported %v, want 4 pages", res.Pages)
	}
	sm := readOut(t, out, "sitemap.xml")
	for _, want := range []string{"/products/a%25/", "/products/caf%C3%A9/", "/products/caf%C3%A9%25/"} {
		if !strings.Contains(sm, "<loc>https://x.test"+want+"</loc>") {
			t.Errorf("sitemap lacks %s:\n%s", want, sm)
		}
	}
}
