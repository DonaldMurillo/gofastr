package headless

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestLeaveGuardDeclineRestoresInflightNavigation(t *testing.T) {
	for _, envelope := range []bool{false, true} {
		for _, themed := range []bool{false, true} {
			t.Run(fmt.Sprintf("envelope=%t/dialog=%t", envelope, themed), func(t *testing.T) {
				release := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				t.Cleanup(unblock)
				layouts := ""
				if envelope {
					layouts = `,"layouts":["l:site"]`
				}
				body := fmt.Sprintf(`<script type="application/json" id="gofastr-routes">[{"path":"/"%s},{"path":"/slow"%s},{"path":"/next"%s}]</script>`, layouts, layouts, layouts) +
					`<script>const originalFetch=window.fetch;window.fetch=(...args)=>originalFetch(...args).then(response=>{
if(new URL(args[0],location.href).pathname==='/slow')response.clone().text().then(()=>setTimeout(()=>setTimeout(()=>window.__slowSettled=true,0),0));
return response;});</script>` + leaveDialogForm + `<a id="slow" href="/slow">Slow</a><a id="next" href="/next">Next</a>`
				if themed {
					body += leaveDialogTemplate
				}
				if envelope {
					body = `<div data-cui-layout-key="l:site"><section id="slot" data-cui-layout-slot="l:site">` + body + `</section><aside data-cui-outlet="l:site#aside">Inline aside</aside></div>`
				}
				server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
					for _, path := range []string{"/slow", "/next"} {
						mux.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
							if req.URL.Path == "/slow" {
								select {
								case <-release:
								case <-req.Context().Done():
									return
								}
							}
							w.Header().Set("Content-Type", "text/html")
							w.Header().Set("X-Gofastr-Partial", "true")
							if envelope {
								w.Header().Set("X-Gofastr-Swap", "l:site")
								w.Header().Set("X-Gofastr-Envelope", "2")
								fmt.Fprintf(w, `<template data-cui-fill="l:site"><p id="arrived">%s</p></template><template data-cui-fill="l:site#aside">Updated aside</template>`, req.URL.Path)
								return
							}
							fmt.Fprintf(w, `<p id="arrived">%s</p>`, req.URL.Path)
						})
					}
				})
				ctx := behaviorPage(t, server)
				if err := chromedp.Run(ctx,
					chromedp.Poll(`!!window.__gofastr?.loadedModules?.['headless-leaveguard']`, nil),
					chromedp.Evaluate(leaveGuardStub+`window.__confirmRet=false;`, nil),
					chromedp.Click("#slow", chromedp.ByID),
					chromedp.Poll(`window.__gofastr.currentPath==='/slow' && document.documentElement.getAttribute('aria-busy')==='true'`, nil),
					leaveGuardEdit("#gf-name", "typed"),
					chromedp.Click("#next", chromedp.ByID),
				); err != nil {
					unblock()
					t.Fatal(err)
				}
				if themed {
					if err := chromedp.Run(ctx, chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil), leaveDialogClick("cancel")); err != nil {
						unblock()
						t.Fatal(err)
					}
				}
				check := func(stage string) {
					t.Helper()
					var state struct {
						Path, Current, Value string
						Busy                 bool
					}
					if err := chromedp.Run(ctx, chromedp.Evaluate(`({Path:location.pathname,Current:window.__gofastr.currentPath,Value:document.getElementById('gf-name')?.value || '',Busy:!!document.querySelector('[aria-busy]')})`, &state)); err != nil {
						t.Fatal(err)
					}
					if state.Path != "/" || state.Current != "/" || state.Value != "typed" || state.Busy {
						t.Errorf("%s: cancelled navigation state = %+v, want prior URL, edits, and no busy flag", stage, state)
					}
				}
				check("declined")
				unblock()
				if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__slowSettled`, nil)); err != nil {
					t.Fatal(err)
				}
				check("request settled")
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`document.getElementById('gf').reset()`, nil),
					chromedp.Click("#next", chromedp.ByID),
					chromedp.Poll(`document.getElementById('arrived')?.textContent==='/next' && !document.querySelector('[aria-busy]')`, nil),
				); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
