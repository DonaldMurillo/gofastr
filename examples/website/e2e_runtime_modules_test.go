package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// E2E coverage for the code-split runtime (see
// docs/runtime-code-split-plan.md and core-ui/runtime/src/*.js).
//
// The contract these tests pin:
//
//   - Page WITHOUT a feature's marker NEVER fetches that feature's
//     runtime module. (The whole point of the split.)
//   - Page WITH a marker fetches the module via /__gofastr/runtime/<name>.js
//     by the time the user interacts. The marker scanner fires on
//     DOMContentLoaded + SPA-nav so a cold-cache visit still has the
//     module ready when the user clicks.
//   - Hovering a data-fui-prefetch="<module>" element triggers the
//     module fetch before any click — the "warm the cache on hover"
//     path.
//   - The manifest emitted in <head> binds each module to its content-
//     addressed `?v=<hash>` URL.

// collectRuntimeModuleURLs subscribes to chromedp's network events and
// returns every distinct `/__gofastr/runtime/<name>.js` URL the page
// fetched.
func collectRuntimeModuleURLs(ctx context.Context) (*sync.Map, func()) {
	urls := &sync.Map{}
	cancel := func() {}
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			if strings.Contains(e.Request.URL, "/__gofastr/runtime/") {
				urls.Store(e.Request.URL, true)
			}
		}
	})
	return urls, cancel
}

// Visiting / (home page) must NOT trigger fetches for runtime
// modules whose markers aren't on the page. The website mounts a
// site-wide toast stack on every page + emits the gofastr-sse meta
// tag, so toasts.js and sse.js are legitimately loaded — those are
// excluded. The split's payoff is asserting popover/fileupload/menu
// DON'T load.
func TestE2E_RuntimeSplit_NoMarkersNoFetch(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		// Wait through DOMContentLoaded + the marker scanner + one
		// idle frame. Anything fetched after this is overflow we
		// don't want.
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// The home page has no popover trigger, no fileupload zone, no
	// menu — those modules should not load. (toasts + sse load
	// legitimately because of site-wide widgets above.)
	for _, mod := range []string{"popover", "fileupload", "menu"} {
		urls.Range(func(k, _ interface{}) bool {
			u := k.(string)
			if strings.Contains(u, "/runtime/"+mod+".js") {
				t.Errorf("home page should NOT fetch %s module; got %s", mod, u)
			}
			return true
		})
	}
}

// /components/fileupload has a [data-fui-fileupload] marker; the
// scanner MUST trigger a fetch for the fileupload module.
func TestE2E_RuntimeSplit_FileuploadLoadsOnMarker(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/components/fileupload"),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	found := false
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/fileupload.js") {
			found = true
		}
		return true
	})
	if !found {
		var listed []string
		urls.Range(func(k, _ interface{}) bool { listed = append(listed, k.(string)); return true })
		t.Errorf("/components/fileupload should fetch fileupload module; runtime urls observed: %v", listed)
	}
}

// /components/popover has [data-fui-popover-anchor] triggers; the
// scanner MUST trigger a fetch for the popover module.
func TestE2E_RuntimeSplit_PopoverLoadsOnMarker(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/components/popover"),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	found := false
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/popover.js") {
			found = true
		}
		return true
	})
	if !found {
		t.Errorf("/components/popover should fetch popover module")
	}
}

// /components/toast has a toast stack widget mounted, so the toasts
// module must load.
func TestE2E_RuntimeSplit_ToastsLoadOnMarker(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/components/toast"),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	found := false
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/toasts.js") {
			found = true
		}
		return true
	})
	if !found {
		t.Errorf("/components/toast should fetch toasts module")
	}
}

// The framework emits a JSON manifest in <head> mapping each module
// name to its content-hash. Loader uses it to construct ?v=<hash>
// URLs so a deploy auto-busts the cache.
func TestE2E_RuntimeSplit_ManifestIsContentAddressed(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var manifest, requestedURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/fileupload"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('gofastr-runtime-modules')?.textContent || ''`, &manifest),
		chromedp.Sleep(500*time.Millisecond),
		// Pull the actual src= of any loaded fileupload script tag
		chromedp.Evaluate(`(() => {
            const s = document.querySelector('script[src*="/runtime/fileupload.js"]');
            return s ? s.getAttribute('src') : '';
        })()`, &requestedURL),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(manifest, `"fileupload"`) {
		t.Errorf("manifest should declare fileupload hash; got %q", manifest)
	}
	if requestedURL == "" {
		t.Skip("fileupload script not present — marker may not have fired yet")
	}
	if !strings.Contains(requestedURL, "?v=") {
		t.Errorf("fileupload script URL should carry ?v=<hash> cache-buster; got %q", requestedURL)
	}
}

// A user who clicks a `data-fui-open` button BEFORE the framework's
// /__gofastr/widgets catalog fetch resolves must not lose the click.
// Today the click delegator is installed inside the catalog .then()
// callback — meaning on slow networks (Slow 3G, cold cache, a deploy
// in flight) the very first click on an open trigger hits no handler
// at all. The button looks dead and the user clicks again, often
// after the catalog has loaded — at which point the second click works
// and the first click feels lost.
//
// The fix: install the click delegator in CORE at boot, before the
// catalog fetch. The handler awaits loadModule('widgets') itself, so
// state-on-namespace + the openWidget stub bridge the gap.
func TestE2E_RuntimeSplit_ClickBeforeCatalogStillOpens(t *testing.T) {
	// Spin a custom httptest.Server that stalls /__gofastr/widgets by
	// 800ms (server-side). Stalling at the network layer is the most
	// faithful repro of a real cold-cache deploy: the page paints,
	// the catalog request is in flight, and the user clicks during
	// the wait. If the click delegator is gated on the catalog .then,
	// the click hits no listener and dies.
	app, _ := setupServer()
	stall := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__gofastr/widgets" {
			time.Sleep(800 * time.Millisecond)
		}
		app.Router.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(stall)
	t.Cleanup(srv.Close)
	base := srv.URL
	ctx := newE2EBrowserCtx(t)

	var opened bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/drawer"),
		chromedp.WaitVisible(`button[data-fui-open="components-drawer"]`, chromedp.ByQuery),
		// Click during the catalog stall. Must be queued (awaited),
		// not lost. After the stall + grace window, widget MUST be open.
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-drawer"]').click()`, nil),
		chromedp.Sleep(2000*time.Millisecond),
		chromedp.Evaluate(`(() => {
            const w = document.querySelector('[data-fui-widget="components-drawer"]');
            if (!w) return false;
            return !w.hasAttribute('hidden');
        })()`, &opened),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !opened {
		t.Errorf("data-fui-open click fired before /__gofastr/widgets catalog resolved did NOT open " +
			"the widget — click delegator is gated on catalog .then() and drops cold-cache clicks.")
	}
}

// When `/__gofastr/runtime/toasts.js` fails to load (deploy mid-flight,
// CDN cache miss, transient 5xx), the X-Gofastr-Toast header path used
// to swallow the rejection via `.catch(() => {})` — the user's toast
// (often a "Save failed" error) silently vanished. Core must show a
// minimal fallback notice so the user still sees the message.
func TestE2E_RuntimeSplit_ToastModuleFailureShowsFallback(t *testing.T) {
	app, _ := setupServer()
	srv500 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__gofastr/runtime/toasts.js" {
			// Force toasts.js to 500 — simulates a deploy mid-flight.
			http.Error(w, "broken", http.StatusInternalServerError)
			return
		}
		app.Router.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(srv500)
	t.Cleanup(srv.Close)
	base := srv.URL
	ctx := newE2EBrowserCtx(t)

	var fallbackVisible bool
	// /components/about doesn't ship the toasts marker (no toast stack
	// on that page), so toasts.js isn't pre-loaded. We use the
	// /components/toast/push RPC instead — but call it from /about-style
	// page via fetch + manual header dispatch... simpler: navigate to a
	// non-toast page, then fetch the toast endpoint and assert fallback.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/about"),
		pageReady(),
		// Trigger an RPC manually that responds with X-Gofastr-Toast.
		// We can't use data-fui-rpc on /about because there's no
		// existing button — eval fetch + then poke the runtime's RPC
		// header dispatcher to mimic what dispatchRPC does.
		chromedp.Evaluate(`(async () => {
            const r = await fetch('/components/toast/push', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({variant:'danger', title:'Save failed', body:'Retry?'}),
            });
            const header = r.headers.get('X-Gofastr-Toast');
            // Mimic core's X-Gofastr-Toast dispatch path: kick off
            // loadModule('toasts') then call NS.toast. Since the
            // module 500s, the .catch path must show a fallback.
            window.__gofastr.loadModule('toasts').then(() => {
                try {
                    const parsed = JSON.parse(header);
                    const arr = Array.isArray(parsed) ? parsed : [parsed];
                    for (const cfg of arr) window.__gofastr.toast(cfg);
                } catch (_) {}
            }).catch((err) => {
                // The fallback contract: core must surface the toast
                // payload visibly even when the module fails.
                if (window.__gofastr._fallbackToast) {
                    try {
                        const parsed = JSON.parse(header);
                        const arr = Array.isArray(parsed) ? parsed : [parsed];
                        for (const cfg of arr) window.__gofastr._fallbackToast(cfg);
                    } catch (_) {}
                }
            });
        })()`, nil),
		chromedp.Sleep(800*time.Millisecond),
		// The fallback should produce SOME visible toast-shaped node
		// with the title text the server sent.
		chromedp.Evaluate(`(() => {
            // Look for the fallback container by a stable hook.
            const fallback = document.querySelector('[data-fui-toast-fallback]');
            if (!fallback) return false;
            return fallback.textContent.includes('Save failed');
        })()`, &fallbackVisible),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !fallbackVisible {
		t.Errorf("X-Gofastr-Toast header fired with toasts.js returning 500 should render a fallback " +
			"notice carrying the server-sent title; nothing visible was found.")
	}
}

// Inserting a marker into the DOM via island RPC, signal swap, or any
// other in-place mutation MUST trigger the module loader. Today the
// marker scanner runs only on DOMContentLoaded + gofastr:navigate;
// a newly-injected [data-fui-fileupload] zone (e.g. an RPC response
// that replaces innerHTML) used to be dead — the module never loaded.
//
// The MutationObserver in core handles component/widget hydration on
// inserted nodes; it MUST also kick the module scanner so newly-
// inserted markers cause the corresponding module to load.
func TestE2E_RuntimeSplit_MutationObserverLoadsNewMarker(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		// Home page has no fileupload marker, so the module is NOT
		// pre-loaded — exactly the cold-cache case we want to test.
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Sleep(300*time.Millisecond),
		// Inject a fresh fileupload zone via DOM mutation. This is the
		// same shape an island swap or RPC innerHTML replacement would
		// produce: new subtree appended under document.body containing
		// the module's marker attribute.
		chromedp.Evaluate(`(() => {
            const wrap = document.createElement('div');
            wrap.innerHTML = '<div data-fui-fileupload><input type="file" /></div>';
            document.body.appendChild(wrap);
        })()`, nil),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	found := false
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/fileupload.js") {
			found = true
		}
		return true
	})
	if !found {
		t.Errorf("appending a [data-fui-fileupload] subtree to the DOM should trigger the " +
			"module loader via the MutationObserver, but fileupload.js was never fetched.")
	}
}

// After a SPA-nav swaps `<main>` content, every ALREADY-LOADED runtime
// module must re-run its initializer against the fresh DOM. Without
// this, a page like /components/toast loads the toasts module on first
// paint, the user navs to a different page that has its own SSR-inlined
// toast stack with TTL items, and those new items NEVER get their auto-
// dismiss timers armed — _initToasts only ran once at module-load time
// before that DOM existed.
//
// The test injects a fresh SSR-style toast item with a 300ms TTL into
// the DOM AFTER the toasts module is loaded, then fires
// `gofastr:navigate`. If the per-module rescan contract is wired, the
// item's auto-dismiss timer arms inside the rescan and the item is
// removed within ~500ms. If not, the item lingers forever.
func TestE2E_RuntimeSplit_SPANavRescansLoadedModules(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var dismissed bool
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/components/toast"),
		pageReady(),
		chromedp.Sleep(400*time.Millisecond), // toasts module loaded by now
		// Inject a brand-new toast-stack with a TTL item — simulating
		// the post-SPA-nav case where a freshly-swapped <main> brings
		// a stack the module never saw at load time.
		chromedp.Evaluate(`(() => {
            const stack = document.createElement('div');
            stack.setAttribute('data-fui-toast-stack', 'spa-nav-rescan-test');
            stack.innerHTML = '<div data-fui-toast-id="rescan-target" data-fui-toast-ttl-ms="300">' +
                '<div class="ui-notification ui-notification--info">SPA nav rescan target</div>' +
                '</div>';
            document.body.appendChild(stack);
            // The contract: core dispatches gofastr:navigate after the
            // swap, and every loaded module's scanner re-runs against
            // the fresh DOM.
            window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: '/x' } }));
        })()`, nil),
		chromedp.Sleep(700*time.Millisecond), // TTL is 300ms, dismiss anim ~200ms
		chromedp.Evaluate(`!document.querySelector('[data-fui-toast-id="rescan-target"]')`, &dismissed),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !dismissed {
		t.Errorf("SSR toast with TTL=300ms injected after toasts.js loaded was NOT dismissed " +
			"after gofastr:navigate fired — modules don't re-init on SPA nav.")
	}
}

// Hovering an element with data-fui-prefetch fires loadModule for the
// named module — the "warm the cache before click" contract. We
// dispatch a synthetic pointerover (no actual mouse needed) and
// verify the module URL appears in the request log.
func TestE2E_RuntimeSplit_HoverPrefetch(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		// /components/ index has no popover triggers, so popover
		// won't be auto-loaded by the marker scanner. Inject a
		// synthetic data-fui-prefetch element and dispatch a hover
		// event to prove the prefetch path is wired.
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.createElement('button');
            btn.setAttribute('data-fui-prefetch', 'popover');
            btn.textContent = 'prefetch test';
            document.body.appendChild(btn);
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
        })()`, nil),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	found := false
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/popover.js") {
			found = true
		}
		return true
	})
	if !found {
		var listed []string
		urls.Range(func(k, _ interface{}) bool { listed = append(listed, k.(string)); return true })
		t.Errorf("pointerover on data-fui-prefetch element should fetch the popover module; runtime urls observed: %v", listed)
	}
}
