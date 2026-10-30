package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// newTabEmbedComp leads with a new-tab link to a page on the app, then
// enough rows that the frame grows past the loader's first height.
type newTabEmbedComp struct{ href string }

func (c *newTabEmbedComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *newTabEmbedComp) RenderCtx(context.Context) render.HTML {
	html := `<a id="full" href="` + c.href + `" target="_blank"><h1>View the full report</h1></a>`
	for i := range 40 {
		html += fmt.Sprintf("<p>row %d</p>", i)
	}
	return render.HTML(html)
}

// A new-tab link inside an embed opens its tab and reports nothing. The
// frame opened it with window.open(href, '_blank', 'noopener') and read a
// null return as a refused popup, but with noopener window.open always
// returns null, so every allowed link also showed the blocked-navigation
// panel and posted an error to the host (2026-10-27 bug bash U43).
//
// chromedp cannot reach into the cross-origin frame, so the click is real
// input at the link's place in the parent's coordinates, which the browser
// routes into the frame.
func TestEmbedNewTabLinkReportsNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	var openedHits int32
	var appURL, nonce string
	customerMux := http.NewServeMux()
	customerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>customer</title></head><body>
<div id="reports"></div>
<script>
  window.__embedEvents = [];
  window.addEventListener('message', (e) => {
    if (e.data && e.data.proto === 'gofastr-embed/1') window.__embedEvents.push(e.data.type);
  });
</script>
<script src="%s/__gofastr/embed.js" data-surface="reports" data-token="%s" data-target="#reports"></script>
</body></html>`, appURL, nonce)
	})
	customer := httptest.NewServer(customerMux)
	t.Cleanup(customer.Close)

	comp := &newTabEmbedComp{}
	application := app.NewApp("Embed new tab")
	application.SetDefaultLayout(headerLayout("main", &testHeaderComp{}))
	reportsScreen := app.NewScreen("/reports", comp).WithTitle("Reports")
	application.RegisterScreen(reportsScreen, app.EmbedLayout())
	eh, err := fembed.New(fembed.Config{
		Surfaces:  []fembed.Surface{{Name: "reports", Screen: reportsScreen, Origins: []string{customer.URL}}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	ds := New(application, WithEmbed(eh))
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/opened" {
			atomic.AddInt32(&openedHits, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>opened</title><p>opened</p>"))
			return
		}
		ds.ServeHTTP(w, r)
	}))
	t.Cleanup(appSrv.Close)
	appURL = appSrv.URL
	comp.href = appSrv.URL + "/opened"
	if nonce, err = eh.MintNonce(context.Background(), "reports", "user-7", customer.URL, nil); err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	ctx := chromedptest.Context(t)
	var x, y float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(customer.URL+"/"),
		chromedp.Poll(`(() => {
			const f = document.querySelector('#reports iframe');
			return !!f && f.getAttribute('style').indexOf('height: 150px') < 0;
		})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		// The heading is the frame's first block: below its top margin
		// and inside its first word, a point lands on the link.
		chromedp.Evaluate(`document.querySelector('#reports iframe').getBoundingClientRect().left + 60`, &x),
		chromedp.Evaluate(`document.querySelector('#reports iframe').getBoundingClientRect().top + 45`, &y),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
		t.Fatalf("clicking the link: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&openedHits) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if atomic.LoadInt32(&openedHits) == 0 {
		var dbg []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.__embedEvents`, &dbg))
		t.Fatalf("the new-tab link never opened its page (click at %.0f,%.0f; events %v)", x, y, dbg)
	}
	// The error post, when it comes, follows the open in the same task;
	// a short wait covers its delivery to the parent.
	var events []string
	if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__embedEvents`, &events)); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e == "error" {
			t.Fatalf("an allowed new-tab link reported an error to the host; events = %v", events)
		}
	}
}
