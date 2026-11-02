package headless

// Escape in and around disclosures: one key, one level, and a reading
// section is never a popup.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Escape inside a combobox in an open popover disclosure closes the
// combobox's listbox (one level). The disclosure must stay open: the
// combobox handled the key (preventDefault), and one Escape is one
// level. A second Escape, with the listbox closed and the query empty,
// is the disclosure's.
func TestE2E_ComboboxEscapeInsideDisclosureClosesOneLevel(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "cq", Name: "q", Label: "Search", Options: []ComboboxOption{
		{Label: "Alpha"}, {Label: "Beta"},
	}}, nil)
	d := Disclosure(DisclosureProps{ID: "pop", Summary: render.Text("Filter"), Open: true, Dismiss: true, Content: box}, nil)
	b := startBehaviorServer(t, string(d))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) || !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("modules never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#cq`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('cq').getAttribute('aria-expanded') === 'true'`) {
		t.Fatal("listbox never opened")
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\u001b")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	state := `JSON.stringify([document.getElementById('cq').getAttribute('aria-expanded'), document.getElementById('pop').open])`
	if got := evalString(t, ctx, state); got != `["false",true]` {
		t.Fatalf("one Escape in the combobox left [listbox expanded, disclosure open] = %s, want [\"false\",true]", got)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\u001b")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pop').open`) {
		t.Fatal("a second Escape, with nothing left for the combobox to close, left the popover open")
	}
}

// An open accordion section (no Dismiss, so not a popup) must not close
// because the reader pressed Escape in an unrelated field elsewhere on
// the page. An open popup (Dismiss) still does: Escape with focus in no
// disclosure is light dismiss.
func TestE2E_EscapeElsewhereClosesPopupsNotSections(t *testing.T) {
	faq := Disclosure(DisclosureProps{ID: "faq", Summary: render.Text("Shipping"), Open: true, Content: render.Text("We ship.")}, nil)
	pop := Disclosure(DisclosureProps{ID: "pop", Summary: render.Text("Filter"), Open: true, Dismiss: true, Content: render.Text("Filters")}, nil)
	b := startBehaviorServer(t, `<input id="other" type="text">`+string(faq)+string(pop))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#other`, chromedp.ByQuery),
		chromedp.KeyEvent("\u001b"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pop').open`) {
		t.Fatal("Escape with focus outside every disclosure left the open popup open")
	}
	if got := evalString(t, ctx, `String(document.getElementById('faq').open)`); got != "true" {
		t.Fatal("Escape in an unrelated input closed an open accordion section")
	}
}
