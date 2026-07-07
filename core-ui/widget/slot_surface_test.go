package widget_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// fetchWidgetCSS mounts a widget and returns its served stylesheet.
func fetchWidgetCSS(t *testing.T, def *widget.Definition) string {
	t.Helper()
	r := router.New()
	widget.Mount(r, def)
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + def.StylePath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("style status: %v code=%d", err, resp.StatusCode)
	}
	return readAll(t, resp)
}

// ruleBlock extracts the declaration block of the first rule whose
// selector contains sel. Empty string when no rule matches.
func ruleBlock(css, sel string) string {
	i := strings.Index(css, sel)
	if i < 0 {
		return ""
	}
	open := strings.Index(css[i:], "{")
	close := strings.Index(css[i:], "}")
	if open < 0 || close < 0 || close < open {
		return ""
	}
	return css[i+open : i+close]
}

// A centered widget using header/body/footer slots must read as ONE
// dialog: defaultSkeleton wraps all slots in a single .fui-panel and
// the CSS paints that panel — three separately-painted sibling cards
// is the defect this guards against.
func TestCenterMultiSlotOnePanel(t *testing.T) {
	def := preset.Modal("multi-slot-probe").
		Slot("header", stubComponent{`<h2 id="t">t</h2>`}).
		Slot("body", stubComponent{`<p>b</p>`}).
		Slot("footer", stubComponent{`<button>ok</button>`}).
		Build()

	chrome := widget.RenderChrome(&def)
	if got := strings.Count(chrome, `class="fui-panel"`); got != 1 {
		t.Fatalf("want exactly 1 .fui-panel wrapper, got %d:\n%s", got, chrome)
	}
	// All three slots live inside the panel (the panel div closes just
	// before the widget root's closing tag).
	panel := chrome[strings.Index(chrome, `class="fui-panel"`):]
	for _, slot := range []string{"fui-slot-header", "fui-slot-body", "fui-slot-footer"} {
		if !strings.Contains(panel, slot) {
			t.Errorf("slot %s not inside the panel:\n%s", slot, chrome)
		}
	}

	css := fetchWidgetCSS(t, &def)
	if ruleBlock(css, ".fui-pos-center > .fui-panel") == "" {
		t.Fatalf("no `.fui-pos-center > .fui-panel` paint rule:\n%s", css)
	}
}

// A plain preset.Modal must paint a panel around its slots: surface
// background, padding, radius, shadow, size caps + scroll. Without
// it, slot content floats bare on the dimmed backdrop.
func TestCenterPanelPaintsSurface(t *testing.T) {
	def := preset.Modal("slot-surface-probe").
		Slot("body", stubComponent{`<p>hello</p>`}).
		Build()
	css := fetchWidgetCSS(t, &def)

	block := ruleBlock(css, ".fui-pos-center > .fui-panel")
	if block == "" {
		t.Fatalf("no `.fui-pos-center > .fui-panel` rule in widget CSS:\n%s", css)
	}
	for _, want := range []string{
		"background: var(--color-surface)",
		"border-radius:",
		"padding:",
		"box-shadow:",
		"max-inline-size:",
		"max-block-size:",
		"overflow: auto",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("center slot panel rule missing %q:\n%s", want, block)
		}
	}
}

// A preset.BottomSheet's root must paint the sheet panel: surface
// background, shadow, rounded top corners, height cap + scroll —
// same disease as the centered modal, fixed on the container like
// drawers.
func TestBottomSheetPaintsPanel(t *testing.T) {
	def := preset.BottomSheet("sheet-surface-probe").
		Slot("body", stubComponent{`<p>hello</p>`}).
		Build()
	css := fetchWidgetCSS(t, &def)

	block := ruleBlock(css, ".fui-pos-bottom {")
	if !strings.Contains(block, "background: var(--color-surface)") {
		// The position loop emits a bare placement rule first; find
		// the surface rule among all .fui-pos-bottom blocks.
		rest := css
		found := false
		for {
			i := strings.Index(rest, ".fui-pos-bottom {")
			if i < 0 {
				break
			}
			b := ruleBlock(rest[i:], ".fui-pos-bottom {")
			if strings.Contains(b, "background: var(--color-surface)") {
				block = b
				found = true
				break
			}
			rest = rest[i+len(".fui-pos-bottom {"):]
		}
		if !found {
			t.Fatalf("no .fui-pos-bottom rule paints a surface:\n%s", css)
		}
	}
	for _, want := range []string{
		"background: var(--color-surface)",
		"box-shadow:",
		"border-radius:",
		"max-block-size:",
		"overflow: auto",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("bottom-sheet panel rule missing %q:\n%s", want, block)
		}
	}
}

// Full-bleed bodies opt out of the default panel: a slot root element
// carrying .fui-slot-bare, and Lightbox viewers ([data-fui-lightbox])
// which center bare media on the backdrop. The opt-out markers sit on
// the slot's root child, one level under the painted panel.
func TestCenterPanelBareOptOut(t *testing.T) {
	def := preset.Modal("slot-bare-probe").
		Slot("body", stubComponent{`<p>hello</p>`}).
		Build()
	css := fetchWidgetCSS(t, &def)

	i := strings.Index(css, ".fui-pos-center > .fui-panel:not(:has(")
	if i < 0 {
		t.Fatalf("center panel rule has no :not(:has(…)) opt-out:\n%s", css)
	}
	sel := css[i : strings.Index(css[i:], "{")+i]
	for _, want := range []string{"> .fui-slot > .fui-slot-bare", "> .fui-slot > [data-fui-lightbox]"} {
		if !strings.Contains(sel, want) {
			t.Errorf("panel opt-out selector missing %q: %s", want, sel)
		}
	}
}
