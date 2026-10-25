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

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
)

// startE2EServer boots site against an httptest server and returns the base
// URL, alias for the native siteE2EServer.
func startE2EServer(t *testing.T) string {
	t.Helper()
	return siteE2EServer(t)
}

// newE2EBrowserCtx returns a headless Chrome context, alias for siteBrowserCtx.
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
	chromedp.ListenTarget(browserCtx, func(ev any) {
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

// pageReady waits until the loaded page has settled (axetest.WaitReady):
// the load event fired, fonts loaded, the runtime booted, no client
// navigation is in flight and every runtime module the page requested has
// registered. It fails naming the unmet condition after 10s.
func pageReady() chromedp.Action { return axetest.WaitReady() }

// waitForTimeout bounds waitFor. Generous next to what any condition in
// this suite needs (tens of milliseconds), small next to the 45s tab
// deadline, so a broken condition fails with its own message.
const waitForTimeout = 10 * time.Second

// waitFor polls js, a boolean expression, until it is true, replacing a
// fixed sleep before an assertion with a wait on the state the assertion
// reads. An expression that throws (the element is not there yet) counts
// as false. After waitForTimeout it fails with the expression and the last
// evaluation error, never by hanging until the tab deadline. The loop runs
// in Go, so a throttled tab's timers cannot stretch it.
func waitFor(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(waitForTimeout)
		var lastErr error
		for {
			var ok bool
			err := chromedp.Evaluate(js, &ok).Do(ctx)
			if err == nil && ok {
				return nil
			}
			if err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("waitFor %s: %w", js, ctx.Err())
				}
				lastErr = err
			}
			if time.Now().After(deadline) {
				if lastErr != nil {
					return fmt.Errorf("waitFor: still false after %s: %s (last error: %v)", waitForTimeout, js, lastErr)
				}
				return fmt.Errorf("waitFor: still false after %s: %s", waitForTimeout, js)
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
}
