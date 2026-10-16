package headless

import (
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
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

// A touch release outside a popup dismisses it with no click at all:
// iOS Safari sends none for a tap on content with no click handler of
// its own, so a page-text tap reaches the module only as pointerup.
// The text sits in a <main tabindex="-1">, as on every gofastr page: a
// focus target the skip link moves to is not a control.
func TestE2E_DisclosureDismissOnTouchRelease(t *testing.T) {
	pop := Disclosure(DisclosureProps{Summary: render.Text("Filters"), Dismiss: true, Open: true, ID: "pop",
		Content: render.Text("panel")}, nil)
	b := startBehaviorServer(t, string(pop)+`<main tabindex="-1"><p id="outside">outside</p></main>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	const release = `document.getElementById(%q).dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerType: %q })); true`
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(release, "outside", "mouse"), nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('pop').open`) {
		t.Fatal("a mouse release closed the popup; a mouse dismisses on its click")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(release, "pop", "touch"), nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('pop').open`) {
		t.Fatal("a touch release inside the popup closed it")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(release, "outside", "touch"), nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pop').open`) {
		t.Fatal("a touch release outside the popup left it open")
	}
}

// A tap on a button below an open in-flow popup reaches that button.
// Closing on the touch release would collapse the panel before the
// browser hit-tests the tap's click, moving the button out from under
// the finger; a control closes the popup through its click instead.
func TestE2E_TapBelowPopupHitsTarget(t *testing.T) {
	pop := Disclosure(DisclosureProps{Summary: render.Text("Filters"), Dismiss: true, Open: true, ID: "pop",
		Content: render.HTML(`<div style="height:300px">panel</div>`)}, nil)
	b := startBehaviorServer(t, string(pop)+`<button id="below" style="height:40px">below</button><div style="height:2000px"></div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	var at []float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.hits = 0; document.getElementById('below').addEventListener('click', () => window.hits++); true`, nil),
		chromedp.Evaluate(`(() => { const r = document.getElementById('below').getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`, &at),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		emulation.SetTouchEmulationEnabled(true),
		input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: at[0], Y: at[1]}}),
		input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pop').open`) {
		t.Fatal("a tap on a button outside the popup left it open")
	}
	if !pollTrue(ctx, `window.hits === 1`) {
		t.Fatal("the tap never clicked the button under it: the popup closed on the release and moved the button")
	}
}
