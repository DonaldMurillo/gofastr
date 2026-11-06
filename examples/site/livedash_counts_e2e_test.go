package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestIncidentCountsStayLive(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	var bound bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard"),
		pageReady(),
		chromedp.Poll(`typeof (window.__gofastr || {}).setSignal === 'function'`, nil),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-signal="dash.incidentsOpen"]')`, &bound),
	); err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Fatal("open incident count has no live signal binding")
	}
	for _, step := range []struct {
		button string
		open   string
		ackd   string
	}{
		{"dash.incidentsOpen:1", "1", "0"},
		{"dash.incidentsOpen:1", "2", "0"},
		{"dash.incidentsOpen:-1", "1", "0"},
		{"dash.incidentsAckd:1", "1", "1"},
	} {
		var open, ackd string
		if err := chromedp.Run(ctx,
			chromedp.Click(`button[data-cui-signal-inc="`+step.button+`"]`, chromedp.ByQuery),
			chromedp.Text(`[data-cui-signal="dash.incidentsOpen"]`, &open, chromedp.ByQuery),
			chromedp.Text(`[data-cui-signal="dash.incidentsAckd"]`, &ackd, chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
		if open != step.open || ackd != step.ackd {
			t.Fatalf("after %s: open=%q acknowledged=%q, want %s/%s", step.button, open, ackd, step.open, step.ackd)
		}
	}
}
