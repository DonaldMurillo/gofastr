package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestVisiblePreloadDropsDetachedLinks(t *testing.T) {
	server, _ := newPreloadSite(t)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
window.__observedLinks = new Set();
window.IntersectionObserver = class {
  observe(target) { window.__observedLinks.add(target); }
  unobserve(target) { window.__observedLinks.delete(target); }
  disconnect() { window.__observedLinks.clear(); }
};`).Do(ctx)
			return err
		}),
		chromedp.Navigate(server.URL+"/hub"),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.preload`, nil),
		chromedp.Evaluate(`(() => {
  window.__gofastr_routes.push({path:'/unseen', preload:'visible'});
  for (const name of ['first', 'second']) {
    const link = document.createElement('a');
    link.href = '/unseen'; link.id = name;
    document.querySelector('main').appendChild(link);
  }
  window.dispatchEvent(new CustomEvent('gofastr:navigate'));
})()`, nil),
		chromedp.Poll(`window.__observedLinks.size === 2`, nil),
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('detail-screen') !== null`, nil),
		chromedp.Poll(`window.__observedLinks.size === 0`, nil, chromedp.WithPollingTimeout(time.Second)),
	); err != nil {
		t.Fatalf("visible preloads retained links from the previous page: %v", err)
	}
}
