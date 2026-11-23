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
	}, render.HTML(`<p id="body">Breadcrumbs, copy and cards.</p>`))
	body := `<div style="width: 480px; padding: 16px">` + string(card) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]string
	probe := `(() => {
		const at = el => { const r = el.getBoundingClientRect(); const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return hit ? (hit.id || hit.className || hit.tagName) : 'none'; };
		const share = document.getElementById('share');
		return {body: at(document.getElementById('body')), share: at(share)};
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
}
