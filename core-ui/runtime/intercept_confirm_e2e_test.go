package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestInterceptDefersToConfirmDialog(t *testing.T) {
	server := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.srv.URL+"/list"),
		chromedp.Click("#to-a"),
		chromedp.WaitVisible("#rec-a"),
		chromedp.Evaluate(fmt.Sprintf(`document.body.insertAdjacentHTML('beforeend', %q); window.__gofastr.loadModule('confirm').then(() => window.__gofastr.askWords({message:'Delete?'}));`, confirmTemplate), nil),
		chromedp.Poll(`!!document.querySelector('dialog:modal')`, nil),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Poll(`document.activeElement.getAttribute('data-cui-confirm-part') === 'accept'`, nil),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!document.querySelector('dialog:modal')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	if snapshot := readStack(ctx); len(snapshot.Layers) != 1 || snapshot.URL != "/rec/a" {
		t.Fatalf("confirm Escape closed its intercept pane: %+v", snapshot)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape), chromedp.Poll(`!document.getElementById('rec-a')`, nil)); err != nil {
		t.Fatal(err)
	}
}
