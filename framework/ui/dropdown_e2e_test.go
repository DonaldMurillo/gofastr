package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A dropdown inside a GET form: its panel floats (it does not push the
// page down), a click outside closes it, and Enter in a field outside
// the closed panel still submits the panel's fields with the form.
func TestE2E_DropdownInsideAForm(t *testing.T) {
	dd := ui.Dropdown(ui.DropdownConfig{ID: "dd", Label: "Filters", Icon: "filter",
		Content: render.HTML(`<select name="status" id="status" aria-label="Status">` +
			`<option value="">Any</option><option value="paid" selected>Paid</option></select>`)})
	srv := menuTriggerAxeServer(t, `<form method="get" action="/">`+
		`<input name="q" id="q" aria-label="Search">`+string(dd)+`</form>`+
		`<p id="below">below</p>`)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	var before, after float64
	var open bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('below').getBoundingClientRect().top`, &before),
		chromedp.Click(`#dd > summary`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('below').getBoundingClientRect().top`, &after),
		chromedp.Evaluate(`document.getElementById('dd').open`, &open),
	); err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Fatal("the trigger did not open the dropdown")
	}
	if after != before {
		t.Errorf("the open panel moved the content below it from %v to %v; it must float", before, after)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#below`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('dd').open`, &open),
	); err != nil {
		t.Fatal(err)
	}
	if open {
		t.Fatal("a click outside left the dropdown open")
	}
	var search string
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`#q`, "acme"+kb.Enter, chromedp.ByQuery),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`location.search`, &search),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search, "q=acme") || !strings.Contains(search, "status=paid") {
		t.Errorf("Enter submitted %q; want the search and the closed panel's status", search)
	}
}
