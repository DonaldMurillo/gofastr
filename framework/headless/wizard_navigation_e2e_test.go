package headless

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestWizardNavDropsPendingFocus(t *testing.T) {
	for _, pending := range []bool{true, false} {
		t.Run(fmt.Sprint("pending=", pending), func(t *testing.T) {
			received := make(chan struct{}, 1)
			release := make(chan struct{})
			browser := startBehaviorServer(t, wizardIslandPage(t, 0, ""), func(mux *http.ServeMux) {
				mux.HandleFunc("/__hui/setup", func(writer http.ResponseWriter, request *http.Request) {
					received <- struct{}{}
					if pending {
						select {
						case <-release:
						case <-request.Context().Done():
						}
					}
					writer.WriteHeader(http.StatusServiceUnavailable)
				})
				destination := wizardIslandPage(t, 1, "")
				mux.HandleFunc("/other", func(writer http.ResponseWriter, request *http.Request) {
					writer.Header().Set("Content-Type", "text/html")
					fmt.Fprint(writer, `<main><div id="other">`+destination+`</div></main>`)
				})
			})
			t.Cleanup(func() { close(release) })
			ctx := behaviorPage(t, browser)
			if !pollTrue(ctx, controlsLoadedExpr(WizardBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr("rpc")) {
				t.Fatal("wizard or RPC module did not load")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`Promise.all(['rpc', 'headless-wizard'].map(name => window.__gofastr.loadModule(name)))`, nil, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`
				window.__answered = false;
				const send = window.__gofastr.fetch;
				window.__gofastr.fetch = (...args) => send(...args).finally(() => { window.__answered = true; });
				document.querySelector('[data-hui-step-wizard-action="next"]').click();`, nil)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-received:
			case <-time.After(5 * time.Second):
				t.Fatal("wizard did not submit")
			}
			if !pending && !pollTrue(ctx, `window.__answered`) {
				t.Fatal("failed request did not finish")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.__gofastr.navigate('/other')`, nil),
				chromedp.Poll(`location.pathname === '/other' && document.getElementById('other') !== null`, nil),
				chromedp.Evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`, nil, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) }),
			); err != nil {
				t.Fatal(err)
			}
			var focused bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement.matches('h2')`, &focused)); err != nil {
				t.Fatal(err)
			}
			if focused {
				t.Fatal("unrelated navigation focused the wizard heading")
			}
		})
	}
}
