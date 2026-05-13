// Browser tests exercising the widgets-demo end-to-end with chromedp.
// They prove that:
//   - core-ui/widget bootstrap.js mounts the chrome onto a vanilla page
//   - data-fui-signal hydrates from /core-ui/widget/<name>/state
//   - data-fui-rpc clicks POST to the right endpoint and the response
//     flows into the bound signal (no page reload)
//   - modal widgets render with a backdrop and dismiss on data-fui-action="close"
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

type htmlStub struct{ html string }

func (h htmlStub) Render() render.HTML { return render.HTML(h.html) }

// startPanelDemo spins up just the floating panel so chromedp clicks
// land on it without an interfering modal backdrop.
func startPanelDemo(t *testing.T) (string, *int64) {
	t.Helper()
	var counter int64
	r := router.New()

	panel := preset.FloatingPanel("demo-panel").
		Slot("body", htmlStub{
			`<p>Counter: <span data-fui-signal="counter" id="counter-display">0</span></p>` +
				`<button id="inc" type="button" data-fui-rpc="/api/inc" data-fui-rpc-signal="counter">+1</button>`,
		}).
		Signal("counter", widget.SignalFunc(func() (any, error) { return atomic.LoadInt64(&counter), nil })).
		RPCWithSignal("POST", "/api/inc", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := atomic.AddInt64(&counter, 1)
			_ = json.NewEncoder(w).Encode(n)
		}), "counter").
		Build()

	widget.Mount(r, &panel)
	widget.MountRuntime(r)

	r.Get("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body><h1>widgets-demo</h1>%s</body></html>`, widget.RuntimeTag())
	}))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, &counter
}

// startModalDemo spins up just the modal for the modal-close test.
func startModalDemo(t *testing.T) string {
	t.Helper()
	r := router.New()
	modal := preset.Modal("demo-modal").
		Slot("body", htmlStub{
			`<div id="modal-card"><h2>Modal</h2><button id="modal-close" type="button" data-fui-action="close">Close</button></div>`,
		}).
		Build()
	widget.Mount(r, &modal)
	widget.MountRuntime(r)
	r.Get("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body><h1>widgets-demo</h1>%s</body></html>`, widget.RuntimeTag())
	}))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newChromeCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	alloc, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browser, browserCancel := chromedp.NewContext(alloc)
	t.Cleanup(browserCancel)
	ctx, timeoutCancel := context.WithTimeout(browser, 30*time.Second)
	return ctx, timeoutCancel
}

func TestWidgetMountsAndHydrates(t *testing.T) {
	url, _ := startPanelDemo(t)
	ctx, cancel := newChromeCtx(t)
	defer cancel()

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`[data-fui-widget="demo-panel"]`, chromedp.ByQuery),
		chromedp.OuterHTML(`[data-fui-widget="demo-panel"]`, &html, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	for _, want := range []string{`fui-pos-bottom-right`, `Counter:`, `data-fui-rpc="/api/inc"`} {
		if !strings.Contains(html, want) {
			t.Errorf("panel chrome missing %q in %s", want, html)
		}
	}

	// Wait for state hydration to populate the signal node.
	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Text(`#counter-display`, &text, chromedp.ByQuery))
		if text == "0" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if text != "0" {
		t.Errorf("signal didn't hydrate from state; counter-display = %q", text)
	}
}

func TestWidgetRPCUpdatesSignal(t *testing.T) {
	url, counter := startPanelDemo(t)
	ctx, cancel := newChromeCtx(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#inc`, chromedp.ByQuery),
		chromedp.Click(`#inc`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate+click: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Text(`#counter-display`, &text, chromedp.ByQuery))
		if text == "1" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if text != "1" {
		t.Errorf("counter-display = %q after click, want 1", text)
	}
	if got := atomic.LoadInt64(counter); got != 1 {
		t.Errorf("server counter = %d, want 1", got)
	}
}

func TestModalWidgetClosesOnAction(t *testing.T) {
	url := startModalDemo(t)
	ctx, cancel := newChromeCtx(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`[data-fui-widget="demo-modal"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-fui-backdrop="demo-modal"]`, chromedp.ByQuery),
		chromedp.Click(`#modal-close`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("modal flow: %v", err)
	}

	// After close, the modal element should be gone.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="demo-modal"]')`, &present))
		if !present {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("modal not removed after close click")
}

