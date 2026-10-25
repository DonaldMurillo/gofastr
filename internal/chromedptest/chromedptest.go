// Package chromedptest boots the headless Chrome used by the repo's
// chromedp-driven tests.
//
// It replaces the per-package copies of the browser-boot helper that had
// drifted across the tree (core-ui/runtime's newSeedBrowserCtx,
// newPollBrowserCtx, newComputeBrowserCtx, newFormBrowserCtx,
// navPrefixBrowser, chromeCtxBrowser, rtcBrowserCtx, and boundaryBrowserCtx;
// core-ui/widget's newBareBrowserCtx; framework/headless's
// behaviorBrowserCtx; framework/uihost's
// newEmbedBrowserCtx, newE2EChromeForUIHost, and the PWA Chrome boot;
// cmd/gofastr's devE2EBrowserCtx; examples/meridian's e2eBrowser;
// examples/backoffice's backofficeBrowser; framework/uie2e's newE2EChrome;
// framework/static's PWA Chrome boot; framework/ui's image-placeholder and
// srcset browser boots; kiln/integration's newChrome; and
// battery/print/chromepdf's host-resolver browser boot), each of which
// repeated the same allocator flags, lazy startup watchdog, and per-test
// deadline. It depends only on chromedp, cdproto, and the standard library
// so core-ui packages (which must not import framework) can use it.
//
// The boot contract: allocator options are chromedp's defaults plus
// headless, disable-gpu, no-sandbox, force-prefers-no-reduced-motion, a 90s
// DevTools websocket-URL deadline, and an explicit window size. A test
// binary starts one Chrome per distinct allocator option set, lazily, on
// the first Context call that needs it, and every later call with the
// same options reuses it: a Chrome launch costs about 360 ms, an isolated
// window in a running one about 110 ms. Each Context call gets its own
// BrowserContext (its own cookies, storage, and HTTP cache, like an
// incognito profile) and its own window at the requested size, so tests
// stay isolated from each other. Chrome is started against a background
// context (not the deadline context) under a watchdog so a hung cold launch
// fails the test instead of the suite; a shared Chrome that died or failed
// leaves the pool at once, so the next call starts a fresh one, and closes
// when the last window still open on it is done. Sharing needs an sh to run
// the reaper that kills Chrome when the test binary exits; on Windows, or
// with no sh, or for an option set that installs a func or writer (see
// AllocatorOptions), every call gets a private Chrome that closes with its
// window, as before sharing. The returned context carries the per-test
// deadline. Cleanup registration order is BrowserContext disposal and
// release of the Chrome, window close, deadline cancel — teardown runs
// deadline-first.
package chromedptest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
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
	// disposeTimeout bounds the cleanup that drops a test's
	// BrowserContext. A shared Chrome that cannot answer in that long is
	// wedged and gets replaced.
	disposeTimeout = 10 * time.Second
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
// DevTools websocket-URL read deadline and the watchdogs on starting Chrome
// and opening the test's window. It replaces each former helper's
// duplicated 90s startup literal.
func StartTimeout(d time.Duration) Option {
	return func(c *config) { c.startTimeout = d }
}

// AllocatorOptions appends extra chromedp allocator options after the standard
// flag set. It replaces the custom allocator additions in former helpers that
// needed an explicit browser path or test-specific flags. Calls with the same
// extra options share one Chrome; calls with different ones never do. An
// option that installs a func, writer, or other reference (ModifyCmdFunc,
// CombinedOutput) cannot be compared, so a call carrying one always gets a
// private Chrome that closes with its window.
func AllocatorOptions(opts ...chromedp.ExecAllocatorOption) Option {
	return func(c *config) { c.extra = append(c.extra, opts...) }
}

// newConfig resolves opts over the package defaults.
func newConfig(opts ...Option) config {
	cfg := config{
		windowW:      defaultWindowW,
		windowH:      defaultWindowH,
		timeout:      defaultTimeout,
		startTimeout: defaultStartTimeout,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// Context opens an isolated window in the test binary's headless Chrome for a
// chromedp-driven test and returns a context carrying the per-test deadline.
// It replaces the duplicated browser boot helpers listed in this package's
// documentation. Chrome starts on first use under the startup watchdog; the
// window lives in its own BrowserContext, and all cancels are registered on t.
// A tab opened from the returned context with chromedp.NewContext lands in
// the same BrowserContext, so it shares the test's cookies and storage.
func Context(t testing.TB, opts ...Option) context.Context {
	t.Helper()
	cfg := newConfig(opts...)
	// A shared Chrome can die between tests, or wedge without the
	// connection dropping. The window open is what finds out, so a failed
	// open retires that Chrome and retries once on a fresh one.
	var lastErr error
	for range 2 {
		b, err := acquire(cfg)
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
		ctx, err := openWindow(t, b, cfg)
		if err == nil {
			return ctx
		}
		lastErr = err
		b.retire()
		b.release()
	}
	t.Fatalf("chrome did not open a window: %v", lastErr)
	return nil
}

// openWindowFault, when set, fails openWindow before it touches b. It is
// the tests' stand-in for a Chrome that wedged with its connection still
// up, the failure only a window open finds.
var openWindowFault func(b *browser) error

// openWindow creates a BrowserContext and a window in it on b, and returns
// the window's context with the per-test deadline. Its cleanups, which end
// with releasing b, are registered on t only once the window is open; on
// error it has already disposed of what it made, and b is still the
// caller's to release.
func openWindow(t testing.TB, b *browser, cfg config) (context.Context, error) {
	if openWindowFault != nil {
		if err := openWindowFault(b); err != nil {
			return nil, err
		}
	}
	opCtx, opCancel := b.command(context.Background(), cfg.startTimeout)
	defer opCancel()
	bcID, err := target.CreateBrowserContext().Do(opCtx)
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	dispose := func() error {
		ctx, cancel := b.command(context.Background(), disposeTimeout)
		defer cancel()
		return target.DisposeBrowserContext(bcID).Do(ctx)
	}
	// A new window, not a tab: a background tab stops running
	// requestAnimationFrame and throttles timers, and its own window is
	// also where the per-test size goes.
	targetID, err := target.CreateTarget("about:blank").
		WithBrowserContextID(bcID).
		WithNewWindow(true).
		WithWidth(int64(cfg.windowW)).
		WithHeight(int64(cfg.windowH)).
		Do(opCtx)
	if err != nil {
		_ = dispose()
		return nil, fmt.Errorf("create window: %w", err)
	}
	tab, tabCancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	// chromedp copies a parent's BrowserContextID into child contexts
	// that do not name a target, so a tab the test opens from this
	// context stays in this BrowserContext. NewContext refuses the ID
	// next to WithTargetID, hence the assignment after it.
	chromedp.FromContext(tab).BrowserContextID = bcID
	// The first Run attaches the window and ties the session to the
	// context it is given, so it runs on tab itself under a watchdog,
	// never on a timeout child.
	attached := make(chan error, 1)
	go func() { attached <- chromedp.Run(tab) }()
	select {
	case err = <-attached:
	case <-b.lost():
		err = errors.New("chrome lost its DevTools connection")
	case <-time.After(cfg.startTimeout):
		err = fmt.Errorf("window did not attach within %v", cfg.startTimeout)
	}
	if err != nil {
		tabCancel()
		_ = dispose()
		return nil, fmt.Errorf("attach window: %w", err)
	}
	t.Cleanup(func() {
		// Disposing closes every tab left in the BrowserContext and
		// drops its cookies, storage, and cache. A Chrome that cannot
		// do that is not fit for the next test: it leaves the pool now
		// and closes once the other windows on it are done.
		if err := dispose(); err != nil {
			if b.alive() {
				t.Logf("chromedptest: dispose browser context: %v; replacing the shared Chrome", err)
			}
			b.retire()
		}
		b.release()
	})
	t.Cleanup(tabCancel)
	ctx, cancel := context.WithTimeout(tab, cfg.timeout)
	t.Cleanup(cancel)
	return ctx, nil
}

// command returns a context for one browser-level command on b (the Target
// domain runs against Chrome rather than a page). It ends at the timeout, or
// as soon as Chrome's connection drops: chromedp would otherwise wait out the
// whole timeout for a reply from a dead Chrome.
func (b *browser) command(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, d)
	go func() {
		select {
		case <-b.lost():
			cancel()
		case <-ctx.Done():
		}
	}()
	return cdp.WithExecutor(ctx, chromedp.FromContext(b.ctx).Browser), cancel
}
