package static

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// seoScreen declares SEO, which the live /llm.md writes as front matter.
type seoScreen struct{}

func (seoScreen) Render() render.HTML { return render.Text("catalog") }
func (seoScreen) ScreenSEO(context.Context) uihost.SEO {
	return uihost.SEO{Description: "Browse the catalog", Canonical: "https://example.com/products"}
}

// The exported llm.md carries the SEO front matter the live one does:
// the export wrote the bare doc, so a static host served agents a page
// with no description or canonical (#614).
func TestExportLLMMDKeepsSEOFrontMatter(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.RegisterScreen(coreapp.NewScreen("/products", seoScreen{}).WithTitle("Products"), nil)
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	srv := httptest.NewServer(uihost.New(a, uihost.WithPublicLLMMD()))
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL + "/products/llm.md")
	if err != nil {
		t.Fatal(err)
	}
	live, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(live), `description: "Browse the catalog"`) {
		t.Fatalf("live llm.md has no SEO front matter:\n%s", live)
	}
	if got := readOut(t, out, "products/llm.md"); got != string(live) {
		t.Errorf("exported llm.md differs from the live one.\nexported:\n%s\nlive:\n%s", got, live)
	}
}
