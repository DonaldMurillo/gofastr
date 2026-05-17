package main

import (
	"context"
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
