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

// TestSearchInputActionVariantPixels reproduces #239's measurement: at a
// 1440px viewport the Action variant's label must be the styled flex box
// (roughly the form's content width), not the ~200px unstyled inline run
// the bug produced (form 992px, label 202px, input 181px).
func TestSearchInputActionVariantPixels(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp")
	}
	entry, ok := registry.Lookup("ui-search-input")
	if !ok {
		t.Fatal("ui-search-input style not registered")
	}
	// The form gets a fixed width so the assertions cannot pass by
	// shrink-wrapping: the label must FILL a constrained form, which is
	// exactly what the unstyled-label bug broke.
	page := `<!doctype html><html><head><style>` +
		entry.CSSFor(style.Theme{}) +
		`.ui-search-input__form { inline-size: 1000px; }` +
		`</style></head><body style="margin:0;width:1440px">` +
		string(ui.SearchInput(ui.SearchInputConfig{
			Name: "q", ID: "q", Action: "/search",
			Placeholder: "a placeholder long enough to clip",
		})) +
		`</body></html>`
	dir := t.TempDir()
	file := filepath.Join(dir, "page.html")
	if err := os.WriteFile(file, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	// Shared suite browser: per-test Chrome launches flake on CI (see
	// siteBrowserCtx), and this test needs only a tab on a file:// URL.
	ctx := siteBrowserCtx(t)

	var formW, labelW, inputW float64
	measure := `(() => {
		const r = s => document.querySelector(s).getBoundingClientRect().width;
		return [r("form"), r("label"), r("input")];
	})()`
	var widths []float64
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate("file://"+file),
		chromedp.Evaluate(measure, &widths),
	); err != nil {
		t.Fatal(err)
	}
	formW, labelW, inputW = widths[0], widths[1], widths[2]

	// The label is the flex box: it should span (almost) the form, and
	// the input should flex to fill most of the label. The bug's
	// signature was label ≈ 200px inside a 992px form.
	if labelW < formW*0.9 {
		t.Errorf("label (%vpx) does not span the form (%vpx): the box style is not on the label", labelW, formW)
	}
	if inputW < labelW*0.5 {
		t.Errorf("input (%vpx) is not flexing inside the label (%vpx)", inputW, labelW)
	}
	fmt.Printf("measured: form=%.0f label=%.0f input=%.0f\n", formW, labelW, inputW)
}
