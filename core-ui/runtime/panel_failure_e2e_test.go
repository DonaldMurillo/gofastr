package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPanelFetchFailureOpensPage(t *testing.T) {
	server := startInterceptStackServer(t)
	server.overlayMiss.Store("drop")
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.srv.URL+"/rec/a"), chromedp.WaitVisible(`#to-panel`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!window.__gofastr?.loadedModules?.intercept`) {
		t.Fatal("intercept did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#to-panel`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `location.pathname === '/rec/a' && !document.getElementById('cui-intercept') && !!document.querySelector('#full-main, #plain-screen')`) {
		t.Fatal("failed panel fetch left the user on the list")
	}
}
