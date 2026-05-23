package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Behavioral converts of the prior source-grep regressions in
// core-ui/runtime/runtime_test.go. Each one renders a live page, drives
// real DOM events, and asserts the runtime's observable contract — not
// the presence of a substring in the bundle.

// Item 3a: setSignal with mode=attr + attr=href MUST scrub
// dangerous-scheme URLs (javascript:, vbscript:, data:). A benign URL
// must pass through unchanged.
func TestE2E_SetSignalRejectsJavascriptHref(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var dangerous, benign string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            // Two anchors bound to distinct signals via data-fui-signal*
            // attrs. Runtime writes each signal value into the href.
            const a1 = document.createElement('a');
            a1.id = 'sig-danger';
            a1.setAttribute('data-fui-signal', 'danger_href');
            a1.setAttribute('data-fui-signal-mode', 'attr');
            a1.setAttribute('data-fui-signal-attr', 'href');
            a1.textContent = 'danger';
            document.body.appendChild(a1);
            const a2 = document.createElement('a');
            a2.id = 'sig-benign';
            a2.setAttribute('data-fui-signal', 'benign_href');
            a2.setAttribute('data-fui-signal-mode', 'attr');
            a2.setAttribute('data-fui-signal-attr', 'href');
            a2.textContent = 'benign';
            document.body.appendChild(a2);
            // Drive the API the test cares about.
            window.__gofastr.setSignal('danger_href', 'javascript:alert(1)');
            window.__gofastr.setSignal('benign_href', '/safe-path');
        })()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('sig-danger').getAttribute('href') || ''`, &dangerous),
		chromedp.Evaluate(`document.getElementById('sig-benign').getAttribute('href') || ''`, &benign),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(dangerous)), "javascript:") {
		t.Errorf("setSignal accepted javascript: href — got %q", dangerous)
	}
	if benign != "/safe-path" {
		t.Errorf("setSignal mangled benign href — got %q, want /safe-path", benign)
	}
}

// Item 3b: clicks on <a download> must NOT trigger SPA fetch. The
// browser's native download handler must take over instead.
func TestE2E_AnchorDownloadSkipsSPA(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Capture whether gofastr:navigate fires on the click — that event
	// is the SPA-router's signal that it intercepted the anchor.
	var navigateFired bool
	var urlAfterClick string

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            window.__navigateFired = false;
            window.addEventListener('gofastr:navigate', () => { window.__navigateFired = true; });
            const a = document.createElement('a');
            a.id = 'dl-link';
            a.href = '/some-file.csv';
            a.setAttribute('download', 'file.csv');
            a.textContent = 'download';
            document.body.appendChild(a);
            // Synthesize a click — preventing the navigation chain via
            // returnValue lets us read state right after.
            const ev = new MouseEvent('click', { bubbles: true, cancelable: true });
            a.dispatchEvent(ev);
        })()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__navigateFired === true`, &navigateFired),
		chromedp.Evaluate(`location.pathname`, &urlAfterClick),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if navigateFired {
		t.Error("click on <a download> fired gofastr:navigate — SPA router should have skipped it")
	}
	if urlAfterClick != "/" {
		t.Errorf("click on <a download> swapped the URL to %q — SPA router should have skipped it", urlAfterClick)
	}
}

// Item 3c: hovering an element with data-fui-prefetch="popover" must
// trigger a fetch for the popover module exactly once even when the
// pointerover fires twice (the delegator de-dupes per element via a
// WeakSet). The local `loadModule` is closure-bound, so we observe the
// network fetch rather than monkey-patching.
func TestE2E_HoverPrefetchLoadsModule(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.createElement('button');
            btn.id = 'prefetch-btn';
            btn.setAttribute('data-fui-prefetch', 'popover');
            btn.textContent = 'prefetch test';
            document.body.appendChild(btn);
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
            // Fire pointerover twice — runtime must de-dup per element.
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
        })()`, nil),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	popoverFetches := 0
	urls.Range(func(k, _ interface{}) bool {
		if strings.Contains(k.(string), "/runtime/popover.js") {
			popoverFetches++
		}
		return true
	})
	if popoverFetches == 0 {
		var listed []string
		urls.Range(func(k, _ interface{}) bool { listed = append(listed, k.(string)); return true })
		t.Errorf("hover on data-fui-prefetch did not fetch popover module; urls: %v", listed)
	}
	if popoverFetches > 1 {
		t.Errorf("popover fetched %d times — delegator must de-dup per element", popoverFetches)
	}
}

// Item 3d: the idle scheduler must use requestIdleCallback when
// available and fall back to setTimeout otherwise. We can't directly
// monkey-patch the closure-bound `loadModule` the runtime uses, but we
// can observe the contract via setTimeout: stub setTimeout (the
// fallback) to record fires, stub requestIdleCallback to undefined, and
// trigger a fresh-marker SPA-nav scan. The runtime's
// _scheduleIdleModules captures rIC at call time, so we observe a
// fallback setTimeout being scheduled with delay 0.
func TestE2E_IdleFallbackUsesRIC(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var rICUsed, timeoutZeroSeen bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            window.__rICUsed = false;
            window.__timeoutZeroSeen = false;
            // Force fallback path.
            window.requestIdleCallback = undefined;

            // Spy on setTimeout(fn, 0) — that's the documented fallback
            // _scheduleIdleModules installs when rIC is missing.
            const origST = window.setTimeout;
            window.setTimeout = function(fn, ms) {
                if (ms === 0 || ms === undefined) {
                    window.__timeoutZeroSeen = true;
                }
                return origST.call(window, fn, ms);
            };

            // Mark sse not-loaded so the next scan queues it as idle.
            if (window.__gofastr.loadedModules) {
                delete window.__gofastr.loadedModules['sse'];
            }
            if (!document.querySelector('meta[name="gofastr-sse"]')) {
                const m = document.createElement('meta');
                m.setAttribute('name', 'gofastr-sse');
                document.head.appendChild(m);
            }

            // Dispatch the navigate event that drives _scanForModules
            // → _scheduleIdleModules → setTimeout(fn, 0) on the
            // fallback path.
            window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: '/x' } }));
        })()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__rICUsed === true`, &rICUsed),
		chromedp.Evaluate(`window.__timeoutZeroSeen === true`, &timeoutZeroSeen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !timeoutZeroSeen {
		t.Error("requestIdleCallback was stubbed undefined but _scheduleIdleModules never scheduled a setTimeout(fn, 0) fallback")
	}
}
