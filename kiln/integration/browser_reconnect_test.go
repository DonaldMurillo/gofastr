package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// captureEventSources keeps every EventSource the page opens on window,
// so the test can drop the reload client's connection the way a network
// blip does: the client's own onerror runs, closes, and reconnects.
const captureEventSources = `(() => {
  const ES = window.EventSource;
  window.__kilnES = [];
  window.EventSource = function (u, o) { const es = new ES(u, o); window.__kilnES.push(es); return es; };
  window.EventSource.prototype = ES.prototype;
})();`

// A shell edit (here the theme) made while the reload client was
// disconnected reaches the page once it reconnects. The bus has no
// replay, so the client cannot tell which events it missed, and the
// reconnect used to run a same-route SPA navigation, which keeps the head:
// the page kept the old stylesheet until a manual reload.
func TestBrowser_ReconnectReloadsShell(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx := chromedptest.Context(t)

	if res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/dashboard", Title: "Dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Dashboard"}},
		}},
	}}); !res.OK {
		t.Fatalf("add_page: %+v", res)
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(captureEventSources).Do(ctx)
			return err
		}),
		chromedp.Navigate(urlBase+"/dashboard"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitJS(t, ctx, `document.body.classList.contains('cui-sse-up')`, "reload client never connected")

	const drop = `(() => {
	  const es = window.__kilnES.filter(e => e.url.includes('/.kiln/events') && e.readyState !== 2).pop();
	  if (!es) return false;
	  es.onerror();
	  return true;
	})()`
	var dropped bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(drop, &dropped)); err != nil || !dropped {
		t.Fatalf("drop the reload client's connection: dropped=%v err=%v", dropped, err)
	}
	waitJS(t, ctx, `document.body.classList.contains('cui-sse-down')`, "reload client did not mark the drop")
	if res := tools.SetTheme(t.Context(), protocol.SetThemeArgs{Theme: map[string]string{"primary": "#123456"}}); !res.OK {
		t.Fatalf("set_theme: %+v", res)
	}

	const themed = `[...document.querySelectorAll('style')].some(s => s.textContent.includes('--color-primary:#123456'))`
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(themed, &ok)); err == nil && ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("theme edit made while disconnected never reached the page after the reconnect")
}
