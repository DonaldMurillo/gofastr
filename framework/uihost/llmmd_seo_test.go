package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// llmmdSEOComp is a screen that declares SEO via the bundle interface.
// Its values must appear both in the HTML <head> and in the per-screen
// llm.md front-matter (#108).
type llmmdSEOComp struct{}

func (llmmdSEOComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (llmmdSEOComp) ScreenSEO() SEO {
	return SEO{
		Description: "Browse the catalog",
		Canonical:   "https://example.com/products",
		Hreflangs: []HreflangLink{
			{Lang: "en", URL: "https://example.com/products"},
			{Lang: "fr", URL: "https://example.com/fr/produits"},
		},
		OG:      &OG{Title: "Products", Image: "https://example.com/og.png"},
		Twitter: &TwitterCard{Card: "summary", Title: "Products"},
		Schema:  []seo.Thing{seo.NewArticle()},
	}
}

// TestScreenLLMMD_InheritsSEOFrontMatter asserts that a screen's llm.md
// carries the SAME resolved SEO values its HTML head emits, proving
// parity between the two surfaces (issue #108).
func TestScreenLLMMD_InheritsSEOFrontMatter(t *testing.T) {
	a := app.NewApp("seo-llm")
	a.RegisterScreen(app.NewScreen("/products", &llmmdSEOComp{}).WithTitle("Products"), nil)
	ds := New(a, WithPublicLLMMD())
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	mdBody, mdResp := getBody(t, srv.URL+"/products/llm.md")
	if mdResp.StatusCode != 200 {
		t.Fatalf("/products/llm.md → %d, want 200", mdResp.StatusCode)
	}
	for _, want := range []string{
		`title: "Products"`,
		`description: "Browse the catalog"`,
		`canonical: "https://example.com/products"`,
		`og_title: "Products"`,
		`og_image: "https://example.com/og.png"`,
		`twitter_card: "summary"`,
		`twitter_title: "Products"`,
		`hreflang:`,
		`- "en"`,
		`- "fr"`,
		`schema_types:`,
		`- "Article"`,
	} {
		if !strings.Contains(mdBody, want) {
			t.Errorf("llm.md missing %q; got:\n%s", want, mdBody)
		}
	}

	// The SAME values must appear in the HTML head — that's the parity
	// contract.
	htmlBody, _ := getBody(t, srv.URL+"/products")
	for _, want := range []string{
		`<meta name="description" content="Browse the catalog">`,
		`<link rel="canonical" href="https://example.com/products">`,
		`<link rel="alternate" hreflang="en" href="https://example.com/products">`,
		`<link rel="alternate" hreflang="fr" href="https://example.com/fr/produits">`,
		`<meta property="og:title" content="Products">`,
		`<meta property="og:image" content="https://example.com/og.png">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta name="twitter:title" content="Products">`,
		`"@type":"Article"`,
	} {
		if !strings.Contains(htmlBody, want) {
			t.Errorf("HTML head missing %q", want)
		}
	}
}

// TestScreenLLMMD_NoSEOOmitsFrontMatter asserts that a screen with no
// SEO declarations produces no front-matter block — preserving the
// original ScreenLLMMD output verbatim.
func TestScreenLLMMD_NoSEOOmitsFrontMatter(t *testing.T) {
	a := app.NewApp("no-seo")
	a.RegisterScreen(app.NewScreen("/plain", &plainComp{}).WithTitle("Plain"), nil)
	ds := New(a, WithPublicLLMMD())
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	body, resp := getBody(t, srv.URL+"/plain/llm.md")
	if resp.StatusCode != 200 {
		t.Fatalf("/plain/llm.md → %d, want 200", resp.StatusCode)
	}
	if strings.HasPrefix(body, "---\n") {
		t.Errorf("screen with no SEO must not get front-matter; got:\n%s", body)
	}
	if !strings.HasPrefix(body, "# Plain\n") {
		t.Errorf("screen llm.md must still start with the title heading; got:\n%s", body[:80])
	}
}

// TestScreenSEOFrontMatter_NoValuesReturnsEmpty pins the empty-input
// contract: when nothing is set, no front-matter is emitted.
func TestScreenSEOFrontMatter_NoValuesReturnsEmpty(t *testing.T) {
	if got := screenSEOFrontMatter("", SEO{}); got != "" {
		t.Errorf("empty SEO must produce empty front-matter, got %q", got)
	}
	if got := screenSEOFrontMatter("Only Title", SEO{}); got != "" {
		t.Errorf("lone title with empty SEO must produce empty front-matter, got %q", got)
	}
}
