package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestWidgetLifecycleUpdatesTriggerARIA(t *testing.T) {
	site := startWidgetLifecycleServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/one/a"),
		chromedp.Click(`#open-drawer`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('open-drawer').getAttribute('aria-expanded') === 'true' && document.getElementById('open-drawer').getAttribute('aria-controls') === 'drawer-root'`, nil, widgetPollOpts),
		chromedp.Click(`[data-cui-widget="appwide-drawer"] [data-cui-action="close"]`),
		chromedp.Poll(`document.getElementById('open-drawer').getAttribute('aria-expanded') === 'false'`, nil, widgetPollOpts),
	); err != nil {
		t.Fatal(err)
	}
}
