package main

// =============================================================================
// Compatibility shims for tests ported from examples/website. The website
// e2e suite used startE2EServer / newE2EBrowserCtx / consoleSink / pageReady /
// settle; site's native harness uses siteE2EServer / siteBrowserCtx. These
// thin shims let the ported test files run against site's harness with no
// edits to their bodies (only route paths were retargeted during the port).
// =============================================================================

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// startE2EServer boots site against an httptest server and returns the base
// URL — alias for the native siteE2EServer.
func startE2EServer(t *testing.T) string {
	t.Helper()
	return siteE2EServer(t)
}

// newE2EBrowserCtx returns a headless Chrome context — alias for siteBrowserCtx.
func newE2EBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	return siteBrowserCtx(t)
}

// consoleSink captures every console error fired during a navigation.
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

// listenConsoleErrors subscribes to runtime console events and records
// error-level lines. Call once per browser context.
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

// pageReady / settle are small settling delays for CSS transitions.
func pageReady() chromedp.Action { return chromedp.Sleep(400 * time.Millisecond) }
func settle() chromedp.Action    { return chromedp.Sleep(250 * time.Millisecond) }
