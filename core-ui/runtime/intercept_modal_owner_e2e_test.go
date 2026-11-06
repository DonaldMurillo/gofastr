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

func TestPaneCloseDismissesOwnedModals(t *testing.T) {
	for _, action := range []string{"back", "direct", "return", "refresh", "pending", "pending-refresh", "pending-nested-refresh", "pending-links-refresh"} {
		t.Run(action, func(t *testing.T) {
			pending := action == "pending" || action == "pending-refresh" || action == "pending-nested-refresh" || action == "pending-links-refresh"
			pendingName := "owned"
			if action == "pending-nested-refresh" {
				pendingName = "nested"
			}
			base := startInterceptStackServer(t)
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/__gofastr/widgets":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"owned","chromePath":"/chrome/owned","backdrop":true,"deepLinkKey":"dialog","deepLinkValue":"owned","deepLinkParams":["record"]}},{"hidden":true,"cfg":{"name":"nested","chromePath":"/chrome/nested","backdrop":true}},{"hidden":true,"cfg":{"name":"unrelated","chromePath":"/chrome/unrelated","backdrop":true}}]`)
				case "/__gofastr/runtime/widgetlinks.js":
					if action == "pending-links-refresh" {
						time.Sleep(400 * time.Millisecond)
					}
					base.srv.Config.Handler.ServeHTTP(w, r)
				case "/chrome/owned", "/chrome/nested", "/chrome/unrelated":
					if pending && action != "pending-links-refresh" && r.URL.Path == "/chrome/"+pendingName {
						time.Sleep(400 * time.Millisecond)
					}
					name := r.URL.Path[len("/chrome/"):]
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprintf(w, `<div data-cui-widget="%s" role="dialog"><button id="%s-action">Action</button></div>`, name, name)
				default:
					base.srv.Config.Handler.ServeHTTP(w, r)
				}
			}))
			t.Cleanup(site.Close)
			ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(site.URL+"/list"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Evaluate(`document.querySelector('main').insertAdjacentHTML('beforeend','<button id="outside" data-cui-open="unrelated">Outside</button>'); true`, nil),
				chromedp.Poll(`!!window.__gofastr._widgetCatalog?.owned`, nil),
				chromedp.Click(`#to-a`, chromedp.ByID),
				chromedp.WaitVisible(`#rec-a`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			owner := "a-close"
			if action == "return" {
				returnClick(t, ctx, "a-to-rel", `!!document.getElementById('rel-r1')`)
				owner = "r-to-deep"
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.__gofastr.openWidget('unrelated', {btn: document.getElementById('outside')})`, nil, chromedp.EvalAsValue),
				chromedp.Evaluate(fmt.Sprintf(`window.__oldPane = document.getElementById('rec-a'); window.__gofastr.openWidget('owned', {btn: document.getElementById(%q), pushUrl: %t, params: {record: 'obsolete'}}); true`, owner, pending), nil),
			); err != nil {
				t.Fatal(err)
			}
			if !pending || pendingName == "nested" {
				// Here the owned open is not stale: it mounts, then loads
				// widgetlinks and writes its record. Wait for that write,
				// or the record set below can land before it and be
				// overwritten on a slow runner.
				if err := chromedp.Run(ctx,
					chromedp.Poll(`!!window.__gofastr._widgets.owned && window.__gofastr.getSignal('record') === 'obsolete'`, nil),
					chromedp.Evaluate(`window.__gofastr.openWidget('nested', {btn: document.getElementById('owned-action')})`, nil, chromedp.EvalAsValue),
				); err != nil {
					t.Fatal(err)
				}
				if !pending {
					if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr._widgets.nested`, nil)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if action == "pending-links-refresh" {
				if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr._widgets.owned`, nil)); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('record', 'current')`, nil)); err != nil {
				t.Fatal(err)
			}
			closeJS := `history.back(); true`
			closed := `!document.getElementById('cui-intercept')`
			if action == "direct" {
				closeJS = `document.getElementById('a-close').click(); true`
			} else if action == "return" {
				closeJS = `window.__gofastr._interceptReturn('/rec/a', document.getElementById('r-to-deep')); true`
				closed = `!document.getElementById('rel-r1')`
			} else if action == "refresh" || action == "pending-refresh" || action == "pending-nested-refresh" || action == "pending-links-refresh" {
				closeJS = `window.__gofastr._interceptReturn('/rec/a', document.getElementById('a-close')); true`
				closed = `document.getElementById('rec-a') !== window.__oldPane`
			}
			var open string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(closeJS, nil),
				chromedp.Poll(closed, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`window.__gofastr._modalStack.slice().sort().join(',')`, &open),
			); err != nil {
				t.Fatal(err)
			}
			if open != "unrelated" {
				t.Fatalf("modals after %s = %q, want unrelated only", action, open)
			}
			var backdrops int
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-cui-backdrop]').length`, &backdrops)); err != nil {
				t.Fatal(err)
			}
			if backdrops != 1 {
				t.Fatalf("backdrops = %d, want unrelated only", backdrops)
			}
			var query string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`location.search`, &query)); err != nil {
				t.Fatal(err)
			}
			if query != "" {
				t.Fatalf("closed layer wrote widget URL %q", query)
			}
			if pending {
				var record string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.getSignal('record')`, &record)); err != nil {
					t.Fatal(err)
				}
				if record != "current" {
					t.Fatalf("stale open applied deep-link record %q", record)
				}
			}
		})
	}
}
