package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestEarlyPartWaitsForSheet: a deferred part lands only once the
// component sheets it renders have loaded, so a part answering before
// its page commits joins that commit styled. Waiting on the page's own
// markup alone flushed the buffered part unstyled.
func TestEarlyPartWaitsForSheet(t *testing.T) {
	sheets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(700 * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `#ppart{display:block;width:7px}`)
	}))
	t.Cleanup(sheets.Close)
	s := newPartsSite(t, 10000)
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<template data-cui-fill="l:site#aside"><div data-cui-comp="slow-part" id="ppart">PART</div></template>`)
	})
	pageGate := s.gatePage(t, "/a")

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr_catalog = {"slow-part": {stylePath: "`+sheets.URL+`/slow-part.css", version: "1"}};
			new MutationObserver(() => { const p = document.getElementById('ppart');
				if (p && window.__partW === undefined) window.__partW = p.getBoundingClientRect().width; })
			.observe(document.body, { childList: true, subtree: true }); true`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	deadline := time.Now().Add(10 * time.Second)
	for s.totalParts() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // the parts sit in the buffer
	pageGate()
	var width float64
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#ppart`, chromedp.ByID),
		chromedp.Evaluate(`window.__partW`, &width),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if width != 7 {
		t.Fatalf("the early part entered the DOM %vpx wide, want 7: it painted before its component sheet loaded", width)
	}
}
