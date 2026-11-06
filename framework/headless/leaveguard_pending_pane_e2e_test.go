package headless

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestPaneDeclineDropsPendingReply(t *testing.T) {
	for _, action := range []string{"close", "return"} {
		for _, reply := range []string{"layer", "query"} {
			t.Run(action+"/"+reply, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var startedOnce, releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				body := `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}},{"path":"/rel/:id","intercept":{"from":"/rec/:id","as":"drawer"}}]</script><a id="open" href="/rec/1">Open</a>` + leaveDialogTemplate
				server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
					mux.HandleFunc("/rec/1", func(w http.ResponseWriter, request *http.Request) {
						if request.URL.RawQuery != "" {
							startedOnce.Do(func() { close(started) })
							select {
							case <-release:
							case <-request.Context().Done():
								return
							}
							w.Header().Set("X-Gofastr-Overlay", "drawer")
							fmt.Fprint(w, `<p id="late-pane">Late query</p>`)
							return
						}
						w.Header().Set("X-Gofastr-Overlay", "drawer")
						fmt.Fprint(w, `<div id="guarded-pane">`+leaveDialogForm+`<a id="pending" href="`+map[string]string{"layer": "/rel/1", "query": "/rec/1?tab=slow"}[reply]+`">Pending</a><button id="close" data-cui-intercept-close>Close</button></div>`)
					})
					mux.HandleFunc("/rel/1", func(w http.ResponseWriter, request *http.Request) {
						startedOnce.Do(func() { close(started) })
						select {
						case <-release:
						case <-request.Context().Done():
							return
						}
						w.Header().Set("X-Gofastr-Overlay", "drawer")
						fmt.Fprint(w, `<p id="late-pane">Late layer</p>`)
					})
				})
				t.Cleanup(unblock)
				ctx := behaviorPage(t, server)
				if err := chromedp.Run(ctx,
					chromedp.Poll(`!!window.__gofastr?.loadedModules?.intercept`, nil),
					chromedp.Click(`#open`, chromedp.ByID),
					chromedp.Poll(`!!document.getElementById('gf-name') && !!window.__gofastr.loadedModules['headless-leaveguard']`, nil),
					chromedp.Evaluate(`const cssReady = window.__gofastr._cssReady.bind(window.__gofastr); window.__gofastr._cssReady = async html => { await cssReady(html); if (html.includes('late-pane')) window.replySeen = true; };`, nil),
					chromedp.Click(`#pending`, chromedp.ByID),
				); err != nil {
					t.Fatal(err)
				}
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("pending pane request did not start")
				}
				gesture := `document.getElementById('close').click()`
				if action == "return" {
					gesture = `window.__gofastr._interceptReturn('/', document.getElementById('gf-name'))`
				}
				if err := chromedp.Run(ctx,
					leaveGuardEdit(`#gf-name`, "unsaved"),
					chromedp.Evaluate(gesture, nil),
					chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
				); err != nil {
					t.Fatal(err)
				}
				unblock()
				if err := chromedp.Run(ctx, chromedp.Poll(`window.replySeen === true`, nil)); err != nil {
					t.Fatal(err)
				}
				var intact bool
				if err := chromedp.Run(ctx, chromedp.Evaluate(`!document.getElementById('late-pane') && !!document.getElementById('gf-name') && document.getElementById('gf-name').value === 'unsaved'`, &intact)); err != nil {
					t.Fatal(err)
				}
				if !intact {
					t.Error("pending reply replaced or covered the guarded pane while its dialog was open")
				}
				if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
					t.Fatal(err)
				}
				if !pollTrue(ctx, `location.pathname === '/' && !document.getElementById('cui-intercept')`) {
					t.Error("Discard did not close the captured pane and return to the page")
				}
			})
		}
	}
}

func TestPaneRetryKeepsNewLayer(t *testing.T) {
	body := `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}}]</script>` +
		leaveDialogTemplate + `<a id="open" href="/rec/1">Open</a><a id="next" href="/rec/2">Next</a>`
	server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/rec/1", func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			fmt.Fprint(w, leaveDialogForm+`<button id="close" data-cui-intercept-close>Close</button>`)
		})
		mux.HandleFunc("/rec/2", func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			fmt.Fprint(w, `<p id="new-pane">New pane</p>`)
		})
	})
	ctx := behaviorPage(t, server)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.intercept`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		leaveGuardEdit(`#gf-name`, "unsaved"),
		chromedp.Evaluate(`document.getElementById('close').click()`, nil),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Evaluate(`history.replaceState(null, '', '/'); window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { root: document } })); document.getElementById('next').click();`, nil),
		chromedp.Poll(`!!document.getElementById('new-pane')`, nil),
		leaveDialogClick("accept-danger"),
		chromedp.Poll(`!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Evaluate(`requestAnimationFrame(() => requestAnimationFrame(() => window.retrySettled = true))`, nil),
		chromedp.Poll(`window.retrySettled === true`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var intact bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.getElementById('new-pane') && location.pathname === '/rec/2'`, &intact)); err != nil {
		t.Fatal(err)
	}
	if !intact {
		t.Fatal("Discard closed a replacement instead of its captured pane")
	}
}
