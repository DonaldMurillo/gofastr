package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A card's Action sits at the header's end, level with the heading, and
// the heading and its description keep the rest of the row.
func TestCardActionSitsBesideHeading(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	card := ui.Card(ui.CardConfig{
		Heading:     "Recent activity",
		Description: "The newest changes across every entity.",
		Action:      ui.Link(ui.LinkConfig{Href: "/audit", Text: "Audit log"}),
	}, render.Text("Ada created INV-1."))
	body := `<div style="width: 480px; padding: 16px">` + string(card) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]float64
	probe := `(() => {
		const r = s => document.querySelector(s).getBoundingClientRect();
		const h = r(".fui-card__heading"), a = r(".fui-card__action"), d = r(".fui-card__description"), hd = r(".fui-card__header");
		return {hTop: h.top, aTop: a.top, aBottom: a.bottom, hBottom: h.bottom, hRight: h.right, aLeft: a.left, aRight: a.right, hdRight: hd.right, dTop: d.top};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["aTop"] >= got["hBottom"] || got["aBottom"] <= got["hTop"] {
		t.Errorf("the action is not level with the heading: %v", got)
	}
	if got["aLeft"] < got["hRight"] {
		t.Errorf("the action overlaps the heading: %v", got)
	}
	// The header pads 24px on each side; the action ends at that inset.
	if diff := got["hdRight"] - got["aRight"]; diff < 20 || diff > 28 {
		t.Errorf("the action does not end at the header's inset (%vpx from its edge): %v", diff, got)
	}
	if got["dTop"] < got["hBottom"] {
		t.Errorf("the description left the heading's column: %v", got)
	}
}

// Href makes the whole card one link, so an Action inside it would be a
// control nested in an anchor: the pair panics.
func TestCardActionWithHrefPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Card with Action and Href rendered")
		}
	}()
	ui.Card(ui.CardConfig{Heading: "Blog", Href: "/apps/blog", Action: ui.Link(ui.LinkConfig{Href: "/x", Text: "x"})})
}
