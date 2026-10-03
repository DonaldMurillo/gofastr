package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/examples/site/sitefooter"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// TestSiteFooterLinkTargetsAt390 pins #257: at a phone viewport, every
// footer link's rendered hit area must reach the design system's 24px
// AA floor for dense clusters (the bug was inline 13px/1.6 anchors at
// ~17px, where vertical padding can't extend an inline box).
func TestSiteFooterLinkTargetsAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp")
	}
	entry, ok := registry.Lookup("docsite-footer")
	if !ok {
		t.Fatal("docsite-footer style not registered")
	}
	// The site's theme carries the spacing values the sheet reads; the
	// tokens files complete it with the colophon's own step. Owned
	// sheets read tokens without fallbacks, so the fixture page must
	// carry the theme's :root block the way the running app's app.css
	// does.
	th := theme.Default().Extend(sitefooter.Tokens)
	var root strings.Builder
	root.WriteString(":root{")
	tokens := style.ThemeToTokens(th)
	for _, k := range slices.Sorted(maps.Keys(tokens)) {
		if strings.Contains(k, ".") {
			continue // dark/component entries are not custom-property names
		}
		root.WriteString("--" + k + ":" + tokens[k] + ";")
	}
	root.WriteString("}")
	page := `<!doctype html><html><head><style>` +
		root.String() +
		entry.CSSFor(th) +
		`</style></head><body style="margin:0">` +
		string(sitefooter.Render(sitefooter.Config{
			Version: "dev",
			Tagline: "hit-area fixture",
			Columns: []sitefooter.Column{
				{Title: "Product", Links: []sitefooter.Link{
					{Label: "Pricing", Href: "/pricing"},
					{Label: "About", Href: "/about"},
				}},
				{Title: "Legal", Links: []sitefooter.Link{
					{Label: "Terms", Href: "/terms"},
					{Label: "Privacy", Href: "/privacy"},
				}},
			},
		})) +
		`</body></html>`
	dir := t.TempDir()
	file := filepath.Join(dir, "footer.html")
	if err := os.WriteFile(file, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := siteBrowserCtx(t)
	var heights []float64
	measure := `Array.from(document.querySelectorAll("li a")).map(a => a.getBoundingClientRect().height)`
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate("file://"+file),
		chromedp.Evaluate(measure, &heights),
	); err != nil {
		t.Fatal(err)
	}
	if len(heights) != 4 {
		t.Fatalf("expected 4 links, measured %d", len(heights))
	}
	for i, h := range heights {
		if h < 24 {
			t.Errorf("link %d hit area %.1fpx, below the 24px AA floor", i, h)
		}
	}
	fmt.Printf("footer link heights at 390px: %v\n", heights)
}
