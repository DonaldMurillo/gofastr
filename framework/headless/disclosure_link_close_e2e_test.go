package headless

// A followed link closes the popup it sits in; a click that is not a
// link being followed, or a link in a reading section, leaves the
// disclosure as the reader left it.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A tab strip inside an open accordion: clicking a tab is in-page
// selection, not navigation, and the accordion stays open.
func TestE2E_TabClickInsideDisclosureKeepsItOpen(t *testing.T) {
	strip := tabsStrip("inner", "One", "Two")
	d := Disclosure(DisclosureProps{ID: "acc", Summary: render.Text("Details"), Open: true, Content: strip}, nil)
	b := startBehaviorServer(t, string(d))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) || !pollTrue(ctx, tabsLoaded) {
		t.Fatal("modules never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#acc [role="tab"][data-cui-tab-index="1"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `String(document.getElementById('acc').open)`); got != "true" {
		t.Fatal("clicking a tab inside an open disclosure collapsed the disclosure")
	}
}

// The close follows the link, not the click: in a popup (Dismiss), a
// click a handler already took (an in-page control spelled as a link),
// a modified click and a target=_blank link leave it open; a plain
// same-page link closes it. In a reading section (no Dismiss, no
// Trap) even the plain link leaves it open.
func TestE2E_DisclosureClosesOnlyForAFollowedLink(t *testing.T) {
	links := `<a id="taken" href="#taken-x">Taken</a>` +
		`<a id="blank" href="#blank-x" target="_blank">Blank</a>` +
		`<a id="plain" href="#plain-x">Plain</a>`
	pop := Disclosure(DisclosureProps{ID: "pop", Summary: render.Text("Menu"), Open: true, Dismiss: true,
		Content: render.Raw(links)}, nil)
	sec := Disclosure(DisclosureProps{ID: "sec", Summary: render.Text("Section"), Open: true,
		Content: render.Raw(`<a id="read" href="#read-x">Read on</a>`)}, nil)
	b := startBehaviorServer(t, string(pop)+string(sec))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("module never loaded")
	}
	// A handler on the link that takes its click, as an island anchor's
	// or a tab's does.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('taken').addEventListener('click', e => e.preventDefault())`, nil)); err != nil {
		t.Fatal(err)
	}
	open := `String(document.getElementById('pop').open)`
	for _, c := range []struct{ name, js string }{
		{"a click a handler took", `document.getElementById('taken').click()`},
		{"a ctrl-click", `document.getElementById('plain').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true}))`},
		{"a target=_blank link", `document.getElementById('blank').click()`},
		{"a middle click", `document.getElementById('plain').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 1}))`},
	} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(c.js, nil)); err != nil {
			t.Fatal(err)
		}
		if got := evalString(t, ctx, open); got != "true" {
			t.Fatalf("%s closed the popup", c.name)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('plain').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pop').open`) {
		t.Fatal("a plain followed link inside the popup left it open")
	}
	// With the popup closed (a click in the section would light-dismiss
	// it), the reading section's own link.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('read').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `String(document.getElementById('sec').open)`); got != "true" {
		t.Fatal("a followed link inside a reading section (no Dismiss) closed it")
	}
}

// A trapped drawer (a site header's phone menu) is a popup too: its
// same-page link closes it, or the page under it stays inert.
func TestE2E_DrawerClosesOnAFollowedLink(t *testing.T) {
	d := Disclosure(DisclosureProps{ID: "drawer", Summary: render.Text("Menu"), Trap: true,
		Content: render.Raw(`<a id="pricing" href="#pricing-x">Pricing</a>`)}, nil)
	b := startBehaviorServer(t, string(d))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#drawer > summary`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('drawer').open`) {
		t.Fatal("the drawer never opened")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#pricing`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('drawer').open && document.querySelectorAll('body > [inert]').length === 0`) {
		t.Fatal("a followed link inside the trapped drawer left it open (and the page inert)")
	}
}
