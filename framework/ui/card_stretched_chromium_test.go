package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// A stretched card's body is the heading link's hit area, while its
// footer control is still the control a click reaches.
func TestCardHeadingHrefCoversTheCardButNotItsControls(t *testing.T) {
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
		Heading:     "Release 0.98",
		Description: "Nov 23",
		HeadingHref: "/blog/0-98",
		Article:     true,
		Footer:      render.HTML(`<button id="share" type="button">Share</button>`),
	}, render.HTML(`<p id="body">Breadcrumbs, copy and cards.</p>`+
		`<p><span id="rb" role="button" tabindex="0">Pick</span> <span id="ce" contenteditable="true">Edit me</span> <span id="ti" tabindex="0">Focusable</span></p>`))
	body := `<div style="width: 480px; padding: 16px">` + string(card) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]string
	probe := `(() => {
		const at = el => { const r = el.getBoundingClientRect(); const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return hit ? (hit.id || hit.className || hit.tagName) : 'none'; };
		const share = document.getElementById('share');
		return {body: at(document.getElementById('body')), share: at(share),
			rb: at(document.getElementById('rb')), ce: at(document.getElementById('ce')), ti: at(document.getElementById('ti'))};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["body"] != "fui-card__heading-link" {
		t.Errorf("a click on the card's body reaches %q, not the heading's link", got["body"])
	}
	if got["share"] != "share" {
		t.Errorf("a click on the footer button reaches %q, not the button", got["share"])
	}
	// The body's own controls, whatever element they are built on, sit
	// above the stretched link.
	for _, id := range []string{"rb", "ce", "ti"} {
		if got[id] != id {
			t.Errorf("a click on the body's #%s reaches %q, not the control", id, got[id])
		}
	}
}

// A stretched card nested in another's body keeps its own link over its
// own surface: a click on the inner card's body reaches the inner link,
// and one on the outer card's body the outer link.
func TestCardHeadingHrefNestedCardKeepsItsOwnLink(t *testing.T) {
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
	inner := ui.Card(ui.CardConfig{Heading: "Inner", HeadingHref: "/inner"},
		render.HTML(`<p id="inner-body">Inner body text that is long enough to click.</p>`))
	outer := ui.Card(ui.CardConfig{Heading: "Outer", HeadingHref: "/outer"},
		render.HTML(`<p id="outer-body">Outer body.</p>`), inner)
	srv := themeTestPageWithHead(t, head, `<div style="width: 480px; padding: 16px">`+string(outer)+`</div>`)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const at = id => { const r = document.getElementById(id).getBoundingClientRect();
			const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
			const a = hit && hit.closest('a');
			return a ? a.getAttribute('href') : (hit ? hit.tagName : 'none'); };
		return {inner: at('inner-body'), outer: at('outer-body')};
	})()`, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["inner"] != "/inner" || got["outer"] != "/outer" {
		t.Errorf("clicks on the two bodies reach %v, want inner /inner and outer /outer", got)
	}
}
