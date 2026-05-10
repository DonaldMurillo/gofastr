package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// startE2EServer spins the website up against an httptest.NewServer
// and returns the base URL. Reused across the whole E2E suite.
func startE2EServer(t *testing.T) string {
	t.Helper()
	app, _ := setupServer()
	srv := httptest.NewServer(app.Router)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newE2EBrowserCtx allocates a headless Chrome with default flags and
// a 30s timeout. Use t.Cleanup-managed cancellation throughout.
func newE2EBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	ctx, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// consoleSink captures every console message; tests assert no error-
// level lines fired during their navigation.
type consoleSink struct {
	mu   sync.Mutex
	errs []string
}

func (c *consoleSink) hasErrors() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.errs))
	copy(out, c.errs)
	return out
}

// listenConsoleErrors subscribes to runtime console events and pushes
// `error` lines into the sink. Call once per browser context.
func listenConsoleErrors(browserCtx context.Context, sink *consoleSink) {
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		if e, ok := ev.(*runtime.EventConsoleAPICalled); ok && e.Type == "error" {
			parts := make([]string, 0, len(e.Args))
			for _, a := range e.Args {
				parts = append(parts, string(a.Value))
			}
			sink.mu.Lock()
			sink.errs = append(sink.errs, fmt.Sprint(parts))
			sink.mu.Unlock()
		}
	})
}

// pageReady is a small settling delay for CSS transitions to complete
// after navigation. Modern CSS animations (interpolate-size etc.) can
// take ~250ms to reach a steady state.
func pageReady() chromedp.Action { return chromedp.Sleep(400 * time.Millisecond) }
func settle() chromedp.Action    { return chromedp.Sleep(250 * time.Millisecond) }
