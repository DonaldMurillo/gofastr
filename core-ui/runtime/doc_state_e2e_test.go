package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// startDocStateServer serves runtime.js plus a minimal page for
// exercising the __gofastr.doc global-document-state module. The page
// carries one SSR-provided singleton element (#fui-toast-fallback) so
// the "adopt existing element, don't re-create" path is covered.
func startDocStateServer(t *testing.T) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html>
<html>
<head><title>doc state e2e</title></head>
<body>
  <div id="fui-toast-fallback" data-ssr="1"></div>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body>
</html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestDocScrollLockRefcount proves the scroll lock is owner-refcounted:
// two lockers → unlock one keeps the viewport locked; unlock the last
// releases it; unlocking a never-registered owner is a harmless no-op.
func TestDocScrollLockRefcount(t *testing.T) {
	url := startDocStateServer(t)
	ctx := newSeedBrowserCtx(t)

	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const D = window.__gofastr.doc;
			const ov = () => document.documentElement.style.overflow;
			const out = [];
			D.lockScroll('a');
			D.lockScroll('b');
			out.push(ov());          // both held → hidden
			D.lockScroll('a');       // re-lock by same owner is idempotent
			D.unlockScroll('a');
			out.push(ov());          // b still holds → hidden
			D.unlockScroll('b');
			out.push(ov());          // last owner gone → released
			D.unlockScroll('ghost'); // unknown owner → no-op, no throw
			out.push(ov());
			out.push(String(D.scrollLocked()));
			return out.join('|');
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if want := "hidden|hidden|||false"; got != want {
		t.Errorf("scroll-lock refcount sequence = %q, want %q", got, want)
	}
}

// TestDocSingletonSemantics proves singleton(id, factory):
//   - creates once, appends to <body>, returns the same node afterwards
//     (factory runs exactly once);
//   - adopts an SSR-provided element with the same id without invoking
//     the factory;
//   - reattach() re-appends a created-but-detached singleton (the hook
//     the SPA full-shell swap calls).
func TestDocSingletonSemantics(t *testing.T) {
	url := startDocStateServer(t)
	ctx := newSeedBrowserCtx(t)

	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const D = window.__gofastr.doc;
			const out = [];
			let made = 0;
			const factory = () => { made++; const d = document.createElement('div'); return d; };
			const a = D.singleton('fui-nav-toast', factory);
			const b = D.singleton('fui-nav-toast', factory);
			out.push(String(a === b));                       // same node
			out.push(String(made));                          // factory ran once
			out.push(String(a.parentElement === document.body));
			out.push(String(document.querySelectorAll('#fui-nav-toast').length));

			// SSR-provided element is adopted, factory NOT called.
			let ssrMade = 0;
			const s = D.singleton('fui-toast-fallback', () => { ssrMade++; return document.createElement('div'); });
			out.push(String(s.getAttribute('data-ssr')));    // the SSR node
			out.push(String(ssrMade));

			// Detach + reattach (what the full-shell swap triggers).
			a.remove();
			out.push(String(a.isConnected));
			D.reattach();
			out.push(String(a.isConnected && a.parentElement === document.body));

			// singleton() on a detached created element also re-adopts it
			// instead of minting a duplicate.
			a.remove();
			const c = D.singleton('fui-nav-toast', factory);
			out.push(String(c === a && made === 1 && a.isConnected));
			return out.join('|');
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if want := "true|1|true|1|1|0|false|true|true"; got != want {
		t.Errorf("singleton sequence = %q, want %q", got, want)
	}
}

// TestDocManifestGuardWarns proves the dev-guard: writes outside the
// frozen MANIFEST still land (warn, don't break the page) but emit a
// console.warn naming the offender; manifest names stay silent.
func TestDocManifestGuardWarns(t *testing.T) {
	url := startDocStateServer(t)
	ctx := newSeedBrowserCtx(t)

	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const D = window.__gofastr.doc;
			const warns = [];
			const orig = console.warn;
			console.warn = (...a) => warns.push(a.join(' '));
			try {
				D.setHtmlAttr('data-fui-os', 'other');       // manifest → silent
				D.bodyClass('fui-sse-up', true);             // manifest → silent
				const before = warns.length;
				D.setHtmlAttr('data-fui-rogue', '1');        // NOT in manifest
				D.bodyClass('fui-rogue-class', true);        // NOT in manifest
				D.removeHtmlAttr('data-fui-rogue');
				return [
					String(before),
					String(warns.length),
					String(warns.every(w => w.indexOf('rogue') !== -1)),
					String(document.body.classList.contains('fui-rogue-class')), // write still landed
					String(Object.isFrozen(D.MANIFEST) && Object.isFrozen(D.MANIFEST.htmlAttrs)),
				].join('|');
			} finally {
				console.warn = orig;
			}
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if want := "0|3|true|true|true"; got != want {
		t.Errorf("manifest guard sequence = %q, want %q", got, want)
	}
}
