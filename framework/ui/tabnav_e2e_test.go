package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The strip's rules reach the <nav> in both shapes: as the root, and
// inside the End row, where the row carries the rule instead. A
// descendant-only selector never matched the root nav, which carries
// data-cui-comp itself.
func TestE2E_TabNavStyledInBothShapes(t *testing.T) {
	items := []ui.TabNavItem{{Text: "All", Href: "/", Current: true}, {Text: "Open", Href: "/?v=open"}}
	plain := ui.TabNav(ui.TabNavConfig{ID: "plain", Label: "Plain", Items: items})
	row := ui.TabNav(ui.TabNavConfig{ID: "inrow", Label: "Row", Items: items,
		End: render.HTML(`<a href="/save">Save</a>`)})
	srv := menuTriggerAxeServer(t, string(plain)+string(row))
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var got struct {
		Plain, InRow, Row []string
	}
	probe := `(() => {
		const s = el => { const c = getComputedStyle(el); return [c.display, c.borderBottomWidth]; };
		return {Plain: s(document.getElementById('plain')), InRow: s(document.getElementById('inrow')),
			Row: s(document.getElementById('inrow').parentElement)};
	})()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`getComputedStyle(document.getElementById('plain')).display === 'flex'`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(probe, &got),
	); err != nil {
		t.Fatalf("the root <nav> never took the strip's display: %v (got %+v)", err, got)
	}
	want := struct{ Plain, InRow, Row []string }{
		Plain: []string{"flex", "1px"},
		InRow: []string{"flex", "0px"},
		Row:   []string{"flex", "1px"},
	}
	for name, pair := range map[string][2][]string{
		"root nav":     {got.Plain, want.Plain},
		"nav in a row": {got.InRow, want.InRow},
		"the row":      {got.Row, want.Row},
	} {
		if len(pair[0]) != 2 || pair[0][0] != pair[1][0] || pair[0][1] != pair[1][1] {
			t.Errorf("%s: display and bottom border = %v, want %v", name, pair[0], pair[1])
		}
	}
}
