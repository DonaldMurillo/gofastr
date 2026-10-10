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

// A floating bar counts the checked rows (not the select-all box) as the
// headless behaviour writes them,
// sits under the rows held to the bottom of the screen, and its clear
// button unchecks every row, which hides it again.
func TestE2E_FloatingSelectionCountsAndClears(t *testing.T) {
	sel := ui.Selection(ui.SelectionConfig{ID: "s", Floating: true, Form: "bulk",
		Bar: render.HTML(`<form id="bulk"><button>Apply</button></form>`),
		Body: render.HTML(`<div data-hui-table><label><input type="checkbox" id="all" data-hui-table-select-all="ids"> all</label>` +
			`<label><input type="checkbox" name="ids" form="bulk" id="r1"> one</label>` +
			`<label><input type="checkbox" name="ids" form="bulk" id="r2"> two</label></div>`),
	})
	srv := menuTriggerAxeServer(t, string(sel))
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var two, pos string
	var shownAfter bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('style[data-cui-style="ui-selection"], link[data-cui-style="ui-selection"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		// The select-all box checks both rows; their changes recount
		// with it checked too, which must not add it.
		chromedp.Click(`#all`, chromedp.ByID),
		// The module's scan (what the kernel runs over inserted markup
		// and after a navigation) recounts with the select-all box
		// checked beside both rows.
		chromedp.Evaluate(`window.__gofastr._moduleScanners.headless(document)`, nil),
		chromedp.Evaluate(`document.querySelector('.fui-selection__count').textContent.trim()`, &two),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-selection__bar')).position`, &pos),
		chromedp.Click(`.fui-selection__clear`, chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.fui-selection__bar').offsetParent !== null`, &shownAfter),
	); err != nil {
		t.Fatal(err)
	}
	if two != "2 selected" {
		t.Errorf("the count reads %q, want \"2 selected\" (two rows, the select-all box not counted)", two)
	}
	if pos != "sticky" {
		t.Errorf("the bar is %s, not held to the screen", pos)
	}
	if shownAfter {
		t.Error("the bar stayed after Clear unchecked the rows")
	}
}

// A checkbox in a form of the body's own (a cell's inline editor for a
// yes/no field) is a value, not a row: checked, it neither shows the
// bar nor counts.
func TestE2E_SelectionIgnoresABodyFormsCheckbox(t *testing.T) {
	sel := ui.Selection(ui.SelectionConfig{ID: "s", Floating: true, Form: "bulk",
		Bar: render.HTML(`<form id="bulk"><button>Apply</button></form>`),
		Body: render.HTML(`<label><input type="checkbox" name="ids" form="bulk" id="r1"> one</label>` +
			`<form id="edit"><label><input type="checkbox" name="active" checked> Active</label></form>`),
	})
	srv := menuTriggerAxeServer(t, string(sel))
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var count string
	var shown, shownWithRow bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('style[data-cui-style="ui-selection"], link[data-cui-style="ui-selection"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__gofastr._moduleScanners.headless(document)`, nil),
		chromedp.Evaluate(`document.querySelector('.fui-selection__count').textContent.trim()`, &count),
		chromedp.Evaluate(`document.querySelector('.fui-selection__bar').offsetParent !== null`, &shown),
		chromedp.Click(`#r1`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('.fui-selection__bar').offsetParent !== null`, &shownWithRow),
	); err != nil {
		t.Fatal(err)
	}
	if shown {
		t.Error("the bar shows for a checked box in the body's own form")
	}
	if count != "0 selected" {
		t.Errorf("the count reads %q, want \"0 selected\"", count)
	}
	if !shownWithRow {
		t.Error("the bar stayed hidden with a row checked")
	}
}
