package headless

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestGuardStopsPendingPaneMount(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(fmt.Sprint(accept), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			body := `<script type="application/json" id="gofastr-routes">` +
				`[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}},` +
				`{"path":"/new","intercept":{"from":"/rec/:id","as":"drawer"}}]</script>` +
				leaveDialogTemplate + `<a id="open" href="/rec/1">open</a>`
			server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
				mux.HandleFunc("/rec/1", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("X-Gofastr-Overlay", "drawer")
					if r.URL.RawQuery != "" {
						fmt.Fprint(w, `<div id="next-page">Page two</div>`)
						return
					}
					fmt.Fprint(w, `<div id="record-pane">`+leaveDialogForm+`<a id="new" href="/new">new</a><a id="page" href="/rec/1?page=2">two</a></div>`)
				})
				mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("X-Gofastr-Overlay", "drawer")
					fmt.Fprint(w, `<div id="pending-pane">New pane</div>`)
				})
			})
			t.Cleanup(finish)
			ctx := behaviorPage(t, server)
			if !pollTrue(ctx, `!!window.__gofastr.loadedModules.intercept`) {
				t.Fatal("intercept module did not load")
			}
			if err := chromedp.Run(ctx, chromedp.Click(`#open`, chromedp.ByID), leaveGuardEdit(`#gf-name`, "edited"), chromedp.Evaluate(`
				document.getElementById('gf-name').dispatchEvent(new Event('input',{bubbles:true}));
				const original = window.__gofastr.fetch;
				window.__gofastr.fetch = async (...args) => { const response = await original(...args); if(args[0] === '/new') window.__overlayAnswered = true; return response; };
				document.getElementById('new').click();`, nil)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(4 * time.Second):
				t.Fatal("pending pane request never started")
			}
			if err := chromedp.Run(ctx, chromedp.Click(`#page`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]')`) {
				t.Fatal("query move did not open the discard dialog")
			}
			finish()
			if !pollTrue(ctx, `!!window.__overlayAnswered`) {
				t.Fatal("pending pane response never arrived")
			}
			var mounted bool
			if err := chromedp.Run(ctx, chromedp.Sleep(150*time.Millisecond), chromedp.Evaluate(`!!document.getElementById('pending-pane')`, &mounted)); err != nil {
				t.Fatal(err)
			}
			if mounted {
				t.Fatal("pending overlay mounted behind the discard dialog")
			}
			part, want := "cancel", `location.pathname === '/rec/1' && !location.search && document.getElementById('gf').hasAttribute('data-hui-dirty')`
			if accept {
				part, want = "accept-danger", `location.search === '?page=2' && !!document.getElementById('next-page') && document.querySelectorAll('#cui-intercept > *').length === 1`
			}
			if err := chromedp.Run(ctx, leaveDialogClick(part)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, want) {
				t.Fatal("dialog replay lost its original pane scope")
			}
		})
	}
}
