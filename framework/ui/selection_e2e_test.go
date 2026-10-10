package ui_test

import (
	"context"
	"strings"
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

// Copy CSV fetches the export route with one _id per checked row (not
// the select-all box, not a box in a cell's own form), writes the answer
// to the clipboard and toasts the count. A Copy URL on another origin
// fetches nothing, so injected markup cannot fill the clipboard.
func TestE2E_SelectionCopiesCheckedRows(t *testing.T) {
	sel := ui.Selection(ui.SelectionConfig{ID: "s", Floating: true, Form: "bulk", Copy: "/api/apps/_export.csv",
		Bar: render.HTML(`<form id="bulk"><button>Apply</button></form>`),
		Body: render.HTML(`<div data-hui-table><label><input type="checkbox" id="all" data-hui-table-select-all="ids"> all</label>` +
			`<label><input type="checkbox" name="ids" value="r1" form="bulk" id="r1"> one</label>` +
			`<label><input type="checkbox" name="ids" value="r2" form="bulk" id="r2"> two</label>` +
			`<form><input type="checkbox" name="active" value="edit" checked></form></div>`),
	})
	srv := menuTriggerAxeServer(t, string(sel))
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTabContext(browser, 90*time.Second)
	defer cancel()
	// Open the tab on the tab's own context: chromedp attaches the target
	// on the first Run, and a step's shorter context must not own it.
	if err := chromedp.Run(ctx); err != nil {
		t.Fatal(err)
	}
	stub := `(() => {
		window.__fetched = []; window.__clip = null; window.__toasts = [];
		window.fetch = (u) => { window.__fetched.push(String(u)); return Promise.resolve(new Response('id,name\nr2,two\n', {status: 200})); };
		Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {
			write: (items) => items[0].getType('text/plain').then((b) => b.text()).then((t) => { window.__clip = t; }),
			writeText: (t) => { window.__clip = t; return Promise.resolve(); },
		}});
		window.__gofastr.toast = (cfg) => { window.__toasts.push(cfg); return 't'; };
	})()`
	var fetched []string
	var clip, toast string
	var crossFetched int
	// Each step runs on its own and names itself on failure: a bare
	// "context deadline exceeded" from one Run cannot say which action
	// waited out the tab's budget.
	// Each step gets its own budget under the tab's, so a step that
	// stalls still leaves the tab alive to report the page, and each
	// logs how long it took.
	step := func(name string, actions ...chromedp.Action) {
		t.Helper()
		began := time.Now()
		sctx, scancel := context.WithTimeout(ctx, 15*time.Second)
		defer scancel()
		err := chromedp.Run(sctx, actions...)
		t.Logf("step %s: %v", name, time.Since(began).Round(time.Millisecond))
		if err != nil {
			var state string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({mods: Object.keys((window.__gofastr||{}).loadedModules||{}), bar: !!document.querySelector('.fui-selection__bar') && document.querySelector('.fui-selection__bar').offsetParent !== null, copy: (document.querySelector('[data-hui-selection-copy]')||{}).outerHTML, fetched: window.__fetched, toasts: window.__toasts})`, &state))
			t.Fatalf("%s: %v\npage: %s", name, err, state)
		}
	}
	step("load",
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('style[data-cui-style="ui-selection"], link[data-cui-style="ui-selection"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)))
	// Both behaviours must be bound before the stub replaces toast and
	// before a click: the count module shows the floating bar, and the
	// copy module owns the click. A fixed sleep raced them on a slow CI
	// runner.
	step("modules bound",
		chromedp.Poll(`!!(window.__gofastr.loadedModules || {})['headless'] && !!(window.__gofastr.loadedModules || {})['headless-selection-copy']`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(stub, nil))
	step("check a row",
		chromedp.Evaluate(`document.getElementById('r2').click()`, nil),
		chromedp.Poll(`document.querySelector('[data-hui-selection-copy]').offsetParent !== null`, nil,
			chromedp.WithPollingTimeout(5*time.Second)))
	step("copy",
		chromedp.Evaluate(`document.querySelector('[data-hui-selection-copy]').click()`, nil),
		chromedp.Poll(`window.__toasts.length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__fetched`, &fetched),
		chromedp.Evaluate(`window.__clip`, &clip),
		chromedp.Evaluate(`window.__toasts[0].title`, &toast))
	step("copy from another origin",
		chromedp.Evaluate(`document.querySelector('[data-hui-selection-copy]').setAttribute('data-hui-selection-copy', 'https://evil.example/x.csv'); window.__fetched = []`, nil),
		chromedp.Evaluate(`document.querySelector('[data-hui-selection-copy]').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__fetched.length`, &crossFetched))
	if len(fetched) != 1 || !strings.HasSuffix(fetched[0], "/api/apps/_export.csv?_id=r2") {
		t.Errorf("fetched %v, want the export route with _id=r2 alone", fetched)
	}
	if clip != "id,name\nr2,two\n" {
		t.Errorf("the clipboard holds %q", clip)
	}
	if toast != "Copied 1 rows as CSV" {
		t.Errorf("toast %q", toast)
	}
	if crossFetched != 0 {
		t.Error("SECURITY: a Copy URL on another origin was fetched")
	}
}
