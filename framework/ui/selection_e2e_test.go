package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The bar is hidden until a row's checkbox is checked, shows while one
// is, and hides again when the last is cleared.
func TestE2E_SelectionBarFollowsTheChecks(t *testing.T) {
	sel := ui.Selection(ui.SelectionConfig{ID: "s",
		Bar: render.HTML(`<p id="bar">2 actions</p>`),
		Body: render.HTML(`<label><input type="checkbox" id="r1"> one</label>` +
			`<label><input type="checkbox" id="r2"> two</label>`),
	})
	srv := menuTriggerAxeServer(t, string(sel))
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	shown := `getComputedStyle(document.getElementById('bar')).display !== 'none' && document.getElementById('bar').offsetParent !== null`
	var atRest, oneChecked, twoChecked, oneCleared, allCleared bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('style[data-cui-style="ui-selection"], link[data-cui-style="ui-selection"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(shown, &atRest),
		chromedp.Click(`#r1`, chromedp.ByID),
		chromedp.Evaluate(shown, &oneChecked),
		chromedp.Click(`#r2`, chromedp.ByID),
		chromedp.Evaluate(shown, &twoChecked),
		chromedp.Click(`#r1`, chromedp.ByID),
		chromedp.Evaluate(shown, &oneCleared),
		chromedp.Click(`#r2`, chromedp.ByID),
		chromedp.Evaluate(shown, &allCleared),
	); err != nil {
		t.Fatal(err)
	}
	if atRest {
		t.Error("the bar shows with nothing checked")
	}
	if !oneChecked || !twoChecked || !oneCleared {
		t.Errorf("the bar hid while a row was checked: one %v, two %v, one cleared %v", oneChecked, twoChecked, oneCleared)
	}
	if allCleared {
		t.Error("the bar stayed after the last check was cleared")
	}
}
