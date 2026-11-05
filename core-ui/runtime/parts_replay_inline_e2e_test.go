package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPartsReplayKeepsInlineRegions(t *testing.T) {
	site := newPartsSite(t, 0)
	ctx := chromedptest.Context(t)
	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.envelope`, nil),
		chromedp.Click(`#goC`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('main').textContent.includes('C-PRIMARY')`, nil),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#doc-a`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('aside').getAttribute('data-cui-loadstate') || ''`, &state),
	); err != nil {
		t.Fatal(err)
	}
	if site.totalParts() != 0 {
		t.Fatal("inline replay fetched deferred parts")
	}
	if state != "" {
		t.Fatalf("inline replay acquired loading state %q", state)
	}
}
