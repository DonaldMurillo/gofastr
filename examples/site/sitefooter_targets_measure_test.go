package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestSiteFooterLinkTargetsAt390 pins #257: at a phone viewport, every
// footer link's rendered hit area must reach the design system's 24px
// AA floor for dense clusters (the bug was inline 13px/1.6 anchors at
// ~17px, where vertical padding can't extend an inline box).
func TestSiteFooterLinkTargetsAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp")
	}
	entry, ok := registry.Lookup("ui-site-footer")
	if !ok {
		t.Fatal("ui-site-footer style not registered")
	}
	page := `<!doctype html><html><head><style>` +
		entry.CSSFor(style.Theme{}) +
		`</style></head><body style="margin:0">` +
		string(ui.SiteFooter(ui.SiteFooterConfig{
			Columns: []ui.SiteFooterColumn{
				{Title: "Product", Links: []ui.SiteFooterLink{
					{Label: "Pricing", Href: "/pricing"},
					{Label: "About", Href: "/about"},
				}},
				{Title: "Legal", Links: []ui.SiteFooterLink{
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
