package headless

// The selection's count is a number in a sentence: it is written the
// page's way, on the server and in the browser, like every other count
// the kit fills.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestE2E_SelectionCountUsesPageDigits(t *testing.T) {
	sel := Selection(SelectionProps{Floating: true, Form: "bulk",
		Bar:  render.HTML(`<form id="bulk"></form>`),
		Body: render.HTML(`<input type="checkbox" id="r1" name="ids" value="1" form="bulk">`)}, nil)
	b := startBehaviorServer(t, string(sel))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.lang = 'ar-EG'`, nil), chromedp.Click(`#r1`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	got := evalString(t, ctx, `document.querySelector('[data-hui-selection-count]').textContent + '|' + (1).toLocaleString('ar-EG')`)
	if got != "١|١" {
		t.Fatalf("the selection count on an ar-EG page reads %q, want the page's digit", got)
	}
}

// The server's first "0" is the reader's zero too.
func TestSelectionInitialCountUsesTheReadersDigits(t *testing.T) {
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	w := (*Strings)(nil).ForRequest(ctx)
	html := string(Selection(SelectionProps{Floating: true, Strings: w,
		Bar: render.HTML(`<form id="bulk"></form>`), Body: render.HTML(`<input type="checkbox" form="bulk">`)}, nil))
	if i18n.Number(ctx, 0) == "0" {
		t.Skip("this build's number formatter writes ar-EG zero as 0")
	}
	want := `<span data-hui-selection-count="">` + i18n.Number(ctx, 0) + `</span>`
	if !strings.Contains(html, want) {
		t.Fatalf("the selection's first count is not the reader's zero %q:\n%s", i18n.Number(ctx, 0), html)
	}
}
