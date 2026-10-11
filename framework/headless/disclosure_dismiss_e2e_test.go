package headless

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A dismissable disclosure closes on a press outside it and stays open
// for a press inside its panel; an ordinary one ignores outside presses
// (an accordion section is not a popup). A menu is dismissable.
func TestE2E_DisclosureDismissOutside(t *testing.T) {
	pop := Disclosure(DisclosureProps{Summary: render.Text("Filters"), Dismiss: true, Open: true, ID: "pop",
		Content: render.HTML(`<input id="inside" aria-label="q">`)}, nil)
	plain := Disclosure(DisclosureProps{Summary: render.Text("Section"), Open: true, ID: "plain",
		Content: render.Text("body")}, nil)
	menu := Menu(MenuProps{ID: "m", Label: "Options", Items: []MenuItem{{Label: "One", Href: "/one"}}}, nil)
	b := startBehaviorServer(t, string(pop)+string(plain)+string(menu)+
		`<p id="outside">outside</p>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#inside`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('pop').open`) {
		t.Fatal("a press inside the panel closed the dismissable disclosure")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`details[data-hui-menu="m"] > summary`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="m"]').open && !document.getElementById('pop').open`) {
		t.Fatal("opening the menu did not dismiss the open popup beside it")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#outside`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="m"]').open`) {
		t.Fatal("a press outside the open menu left it open")
	}
	if !pollTrue(ctx, `document.getElementById('plain').open`) {
		t.Fatal("a press outside closed an ordinary disclosure; only dismissable ones close")
	}
}
