package ui_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestLongListsSkipOffscreenRows: a Long EntryList or Timeline lets the
// browser skip rows far below the viewport, while Tab still walks into
// them. A list without Long renders every row as before.
func TestLongListsSkipOffscreenRows(t *testing.T) {
	const n = 300
	entries := make([]ui.Entry, n)
	events := make([]ui.TimelineEvent, n)
	for i := range n {
		entries[i] = ui.Entry{Title: fmt.Sprintf("Entry %d", i), Href: fmt.Sprintf("/e/%d", i), ID: fmt.Sprintf("e%d", i)}
		events[i] = ui.TimelineEvent{Title: fmt.Sprintf("Event %d", i), Body: render.HTML(fmt.Sprintf(`<a id="t%d" href="/t/%d">open</a>`, i, i))}
	}
	cases := []struct {
		name, body, first, last string
		long                    bool
	}{
		{"entry list", string(ui.EntryList(ui.EntryListConfig{Label: "Feed", Entries: entries, Long: true})), "#e0 a", "#e299 a", true},
		{"ranked entry list", string(ui.EntryList(ui.EntryListConfig{Label: "Feed", Entries: entries, Ranked: true, Long: true})), "#e0 a", "#e299 a", true},
		{"timeline", string(ui.Timeline(ui.TimelineConfig{Events: events, Long: true})), "#t0", "#t299", true},
		{"plain entry list", string(ui.EntryList(ui.EntryListConfig{Label: "Feed", Entries: entries})), "#e0 a", "#e299 a", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := menuTriggerAxeServer(t, c.body)
			ctx := chromedptest.Context(t)
			row := func(sel string) string {
				return `(document.querySelector('` + sel + `').closest('li'))`
			}
			var skipped, tabbed bool
			var cv string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.Poll(`getComputedStyle(`+row(c.last)+`).listStyleType !== '' && document.querySelector('[data-cui-comp] li') && getComputedStyle(document.querySelector('[data-cui-comp] li')).minWidth === '0px'`, nil,
					chromedp.WithPollingInterval(50*time.Millisecond)),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`getComputedStyle(`+row(c.last)+`).contentVisibility`, &cv),
				// The row's own box is drawn; its contents are what is skipped.
				chromedp.Evaluate(`!document.querySelector('`+c.last+`').checkVisibility({ contentVisibilityAuto: true })`, &skipped),
				// The link before the last one, then Tab: focus lands on the last.
				chromedp.Evaluate(`(() => { const all = [...document.querySelectorAll('[data-cui-comp] a')]; all[all.length - 2].focus(); })()`, nil),
				// Focus renders the row it lands in; let that frame paint.
				chromedp.Sleep(300*time.Millisecond),
				chromedp.KeyEvent(kb.Tab),
				chromedp.Evaluate(`document.activeElement === document.querySelector('`+c.last+`')`, &tabbed),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if c.long && (cv != "auto" || !skipped) {
				t.Errorf("a far row of a Long list was rendered: content-visibility=%q skipped=%v", cv, skipped)
			}
			if !c.long && (cv == "auto" || skipped) {
				t.Errorf("a list without Long skipped its rows: content-visibility=%q", cv)
			}
			if !tabbed {
				t.Errorf("Tab did not reach the last row's link")
			}
		})
	}
}
