package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPartsReplayKeepsInlineRegions(t *testing.T) {
	site := newPartsSite(t, 0)
	ctx := chromedptest.Context(t)
	var regions []struct {
		ID      string
		Text    string
		Loading bool
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.envelope`, nil),
		chromedp.Click(`#goC`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('main').textContent.includes('C-PRIMARY')`, nil),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#doc-a`, chromedp.ByID),
		chromedp.Poll(`!document.documentElement.hasAttribute('aria-busy')`, nil),
		chromedp.Evaluate(`['aside','rail'].map(id => {
			const region = document.getElementById(id);
			return {ID:id, Text:region.textContent, Loading:region.hasAttribute('data-cui-loadstate')};
		})`, &regions),
	); err != nil {
		t.Fatal(err)
	}
	if site.totalParts() != 0 {
		t.Fatal("inline replay fetched deferred parts")
	}
	for _, region := range regions {
		want := map[string]string{"aside": "OLD-ASIDEb", "rail": "OLD-RAIL"}[region.ID]
		if region.Text != want {
			t.Errorf("%s replayed text %q, want %q", region.ID, region.Text, want)
		}
		if region.Loading {
			t.Errorf("%s inline replay acquired loading state", region.ID)
		}
	}
}
