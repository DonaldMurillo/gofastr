// Package chromedptest boots the headless Chrome used by the repo's
// chromedp-driven tests.
//
// It replaces the per-package copies of the browser-boot helper that had
// drifted across the tree (core-ui/runtime's newSeedBrowserCtx,
// newPollBrowserCtx, newComputeBrowserCtx, newFormBrowserCtx,
// navPrefixBrowser, chromeCtxBrowser, and rtcBrowserCtx;
// core-ui/widget's newBareBrowserCtx; framework/uihost's
// newEmbedBrowserCtx, newE2EChromeForUIHost, and the PWA Chrome boot;
// cmd/gofastr's devE2EBrowserCtx; examples/meridian's e2eBrowser;
// examples/backoffice's backofficeBrowser; framework/uie2e's newE2EChrome;
// framework/static's PWA Chrome boot; framework/ui's image-placeholder and
// srcset browser boots; kiln/integration's newChrome; and
// battery/print/chromepdf's host-resolver browser boot), each of which
// repeated the same allocator flags, lazy startup watchdog, and per-test
// deadline. It depends only on chromedp and the standard library so core-ui
// packages (which must not import framework) can use it.
//
// The boot contract, carried over verbatim from those copies: allocator
// options are chromedp's defaults plus headless, disable-gpu, no-sandbox,
// a 90s DevTools websocket-URL deadline, and an explicit window size;
// Chrome is started eagerly against the browser context (not the
// deadline context) under a watchdog so a hung cold launch fails the
// test instead of the suite; the returned context carries the per-test
// deadline. Cleanup registration order is allocator cancel, browser
// cancel, deadline cancel — teardown runs deadline-first.
package chromedptest

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Defaults for Context: the window size and per-test deadline the former
// helper copies used most often, and the 90s startup patience every copy
// shared.
const (
	defaultWindowW = 1024
	defaultWindowH = 768
	defaultTimeout = 60 * time.Second
	// defaultStartTimeout bounds both chromedp's DevTools websocket-URL
	// wait and the startup watchdog below. A cold Chrome start on a
	// shared CI runner can exceed chromedp's 20s default; a generous
	// deadline turns that from a flaky suite failure into a few slow
	// seconds.
	defaultStartTimeout = 90 * time.Second
)

// config holds the resolved Option values for one Context call.
type config struct {
	windowW      int
	windowH      int
	timeout      time.Duration
	startTimeout time.Duration
	extra        []chromedp.ExecAllocatorOption
}

// Option customizes Context's browser allocation and deadline. It replaces the
// per-package configuration closures in the former browser helpers listed
// above.
type Option func(*config)

// WindowSize overrides the default 1024x768 window. It replaces the explicit
// window-size constants in those former browser helpers.
func WindowSize(w, h int) Option {
	return func(c *config) { c.windowW, c.windowH = w, h }
}

// Timeout overrides the default 60s deadline carried by the returned context.
// It replaces each former helper's local context.WithTimeout call.
func Timeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// StartTimeout overrides the default 90s startup patience for both the
// DevTools websocket-URL read deadline and the initial chromedp.Run watchdog.
// It replaces each former helper's duplicated 90s startup literal.
func StartTimeout(d time.Duration) Option {
	return func(c *config) { c.startTimeout = d }
}

// AllocatorOptions appends extra chromedp allocator options after the standard
// flag set. It replaces the custom allocator additions in former helpers that
// needed an explicit browser path or test-specific flags.
func AllocatorOptions(opts ...chromedp.ExecAllocatorOption) Option {
	return func(c *config) { c.extra = append(c.extra, opts...) }
}

// Context boots one headless Chrome for a chromedp-driven test and returns a
// context carrying the per-test deadline. It replaces the duplicated browser
// boot helpers listed in this package's documentation. Chrome starts eagerly
// under the startup watchdog, and all cancels are registered on t.
func Context(t testing.TB, opts ...Option) context.Context {
	t.Helper()
	cfg := config{
		windowW:      defaultWindowW,
		windowH:      defaultWindowH,
		timeout:      defaultTimeout,
		startTimeout: defaultStartTimeout,
	}
	for _, o := range opts {
		o(&cfg)
	}
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(cfg.startTimeout),
		chromedp.WindowSize(cfg.windowW, cfg.windowH),
	)
	allocOpts = append(allocOpts, cfg.extra...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run: allocate against
	// the browser context so the browser's lifetime is the browser
	// context's, passing a timeout context here would make the browser
	// die when that deadline passed. The watchdog bounds only the
	// startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(cfg.startTimeout):
		t.Fatalf("chrome did not start within %v", cfg.startTimeout)
	}

	ctx, cancel := context.WithTimeout(browserCtx, cfg.timeout)
	t.Cleanup(cancel)
	return ctx
}
