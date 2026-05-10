// widgets-demo is a tiny example app that mounts a FloatingPanel
// and a Modal using core-ui/widget. Exists to prove the widget API
// end-to-end and host the chromedp browser tests.
//
//	go run ./examples/widgets-demo
//	open http://localhost:8088/
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core-ui/widget"
	"github.com/gofastr/gofastr/core-ui/widget/preset"
)

// counter is a simple shared signal source.
var counter int64

type htmlComp struct{ html string }

func (h htmlComp) Render() render.HTML { return render.HTML(h.html) }

func main() {
	r := router.New()

	// FloatingPanel — corner widget with header + body slot, an SSE-less
	// signal-driven counter, and an RPC button.
	panel := preset.FloatingPanel("demo-panel").
		Slot("header", htmlComp{`<strong class="demo-header">Demo Panel</strong>`}).
		Slot("body", htmlComp{
			`<div class="demo-body">` +
				`<p>Counter: <span data-fui-signal="counter">0</span></p>` +
				`<button type="button" data-fui-rpc="/api/inc" data-fui-rpc-signal="counter">Increment</button>` +
				`</div>`,
		}).
		Signal("counter", widget.SignalFunc(func() (any, error) {
			return atomic.LoadInt64(&counter), nil
		})).
		RPCWithSignal("POST", "/api/inc", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := atomic.AddInt64(&counter, 1)
			_ = json.NewEncoder(w).Encode(n)
		}), "counter").
		Build()

	// Modal — center-mounted dialog with a Close action.
	modal := preset.Modal("demo-modal").
		Slot("body", htmlComp{
			`<div class="demo-modal-card">` +
				`<h2>Hello from Modal</h2>` +
				`<p>This widget is positioned center with a backdrop. ESC or click-outside dismisses.</p>` +
				`<button type="button" data-fui-action="close">Close</button>` +
				`</div>`,
		}).
		Build()

	widget.Mount(r, &panel)
	widget.Mount(r, &modal)
	widget.MountRuntime(r)

	r.Get("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>widgets-demo</title></head><body>
<h1>widgets-demo</h1>
<p>The floating panel and modal below are mounted via core-ui/widget. The
panel's Increment button POSTs to /api/inc and the response value flows
back into the counter signal — no page reload.</p>
` + widget.RuntimeTag() + `
</body></html>`))
	}))

	addr := ":8088"
	log.Printf("widgets-demo on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}
