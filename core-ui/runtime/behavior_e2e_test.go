package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Browser coverage for the behaviour seam (docs/spec-behavior-registry.md,
// "What must be tested" → browser, in core-ui/runtime). A registered
// behaviour is a module from the host down: the kernel learns its markers
// from the inline #gofastr-behaviors block or the
// window.__gofastr_behaviors global, scans for them exactly as it scans
// its own table, loads the module once, and hands it swapped-in DOM.

// probeBehaviorJS is an IIFE keeping the module contract (spec section
// "The module contract"): binds only its own marker, sets
// loadedModules[name] when attached, registers an idempotent scanner on
// _moduleScanners[name] so the kernel can hand it inserted DOM and the
// post-navigation document.
const probeBehaviorJS = `(function () {
  'use strict';
  var NAME = 'probe-beh';
  var NS = window.__gofastr = window.__gofastr || {};
  function wire(el) {
    if (el.getAttribute('data-probe-attached')) return;
    el.setAttribute('data-probe-attached', '1');
  }
  function scan(root) {
    var scope = root && root.querySelectorAll ? root : document;
    if (scope.matches && scope.matches('[data-probe]')) wire(scope);
    var nodes = scope.querySelectorAll('[data-probe]');
    for (var i = 0; i < nodes.length; i++) wire(nodes[i]);
  }
  scan(document);
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();`

// registerProbe isolates the registry and registers probe-beh with the
// given extra options (Markers("[data-probe]") is always applied).
func registerProbe(t *testing.T, opts ...registry.BehaviorOption) {
	t.Helper()
	registry.IsolateForTest(t)
	all := append([]registry.BehaviorOption{registry.Markers("[data-probe]")}, opts...)
	registry.RegisterBehavior("probe-beh", probeBehaviorJS, all...)
}

// probeServer serves runtime.js, the probe module (500 when fail is set)
// and one page built from head and body. It counts module fetches and
// records the last fetched URL.
type probeServer struct {
	srv     *httptest.Server
	hits    atomic.Int32
	lastURL atomic.Value // string
}

func startProbeServer(t *testing.T, head, body string, fail bool) *probeServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("probe-beh")
	if !ok {
		t.Fatal("probe-beh not served by runtime.Module after registration")
	}
	p := &probeServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/probe-beh.js", func(w http.ResponseWriter, r *http.Request) {
		p.hits.Add(1)
		p.lastURL.Store(r.URL.String())
		w.Header().Set("Content-Type", "application/javascript")
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(mod))
	})
	// Other embedded modules the kernel decides to load (activelink and
	// friends) must get JavaScript, not the page HTML: an HTML body in a
	// <script> is an uncaught SyntaxError that has nothing to do with the
	// behaviour under test.
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>probe</title>%s</head><body>
  <main role="main"><span id="ready">ready</span>%s</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, head, body)
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// inlineBehaviorsBlock builds the #gofastr-behaviors block from
// BehaviorsJSON, the shape widget.BehaviorsManifestScript emits (built
// here directly: importing core-ui/widget from core-ui/runtime would be
// an import cycle).
func inlineBehaviorsBlock(t *testing.T) string {
	t.Helper()
	buf := BehaviorsJSON()
	if buf == nil {
		t.Fatal("BehaviorsJSON returned nil for a registered behaviour")
	}
	return `<script type="application/json" id="gofastr-behaviors">` + string(buf) + `</script>`
}

// pollTrue evaluates js (a boolean expression) until it is true or the
// bounded budget runs out. Polls instead of sleeping a fixed settle.
func pollTrue(ctx context.Context, js string) bool {
	for range 40 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

const attachedExpr = `!!(document.querySelector('[data-probe]') && document.querySelector('[data-probe]').getAttribute('data-probe-attached'))`

// A page whose DOM carries the marker fetches the registered module
// exactly once and the module attaches (writes its probe attribute).
func TestBehaviorLoadsOnMarker(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), `<p data-probe>probe target</p>`, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("marker element never got data-probe-attached after module load")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times, want exactly 1", n)
	}
}

// A page without the marker never fetches the module.
func TestBehaviorNoMarkerNoFetch(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), ``, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n := p.hits.Load(); n != 0 {
		t.Fatalf("module fetched %d times without any marker, want 0", n)
	}
}

// A marker inserted after boot (island swap, widget mount) loads the
// module through the insertion scan and attaches.
func TestBehaviorLoadsOnInsertedMarker(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), ``, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            const el = document.createElement('p');
            el.setAttribute('data-probe', '1');
            el.id = 'inserted';
            el.textContent = 'late marker';
            document.body.appendChild(el);
        })()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("inserted marker never attached — MutationObserver scan missed the registered behaviour")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times for inserted marker, want 1", n)
	}
}

// After a client navigation the module's registered scanner runs over
// the new document: the module is already loaded (no second fetch) and
// fresh markers in the swapped content attach.
func TestBehaviorScannerRunsAfterNavigate(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), `<p data-probe>first page marker</p>`, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("module never attached on the first page")
	}
	// Swap in a fresh document body (what a SPA nav does to <main>),
	// then fire the navigate event the runtime dispatches after a swap.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
            const main = document.querySelector('main');
            main.innerHTML = '<span id="ready">ready</span><p data-probe>new document marker</p>';
            window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: '/next' } }));
        })()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("scanner never ran over the post-navigation document")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times across a client navigation, want 1 (scanner reuse, no refetch)", n)
	}
}

// LoadIdle defers the load through requestIdleCallback and the module
// still attaches.
func TestBehaviorLoadIdleAttaches(t *testing.T) {
	registerProbe(t, registry.LoadIdle())
	p := startProbeServer(t, inlineBehaviorsBlock(t), `<p data-probe>idle target</p>`, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// rIC (or the setTimeout fallback) fires within the poll budget.
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("idle behaviour never attached after the idle callback ran")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times, want 1", n)
	}
}

// data-fui-prefetch="<name>" on hover fetches the registered module
// before any click, the warm-the-cache path.
func TestBehaviorHoverPrefetch(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), ``, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            const btn = document.createElement('button');
            btn.setAttribute('data-fui-prefetch', 'probe-beh');
            btn.textContent = 'prefetch probe';
            document.body.appendChild(btn);
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
        })()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['probe-beh']`) {
		t.Fatal("hover on data-fui-prefetch never loaded the registered module")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times on hover prefetch, want 1", n)
	}
}

// A failing fetch (500 on the module URL) leaves the page usable: the
// marker element stays, loadedModules stays falsy, and no uncaught
// error escapes. The failed load surfaces on the console through the
// browser's network error entry, never as an unhandled rejection.
func TestBehaviorFailedFetchLeavesPageUsable(t *testing.T) {
	registerProbe(t)
	p := startProbeServer(t, inlineBehaviorsBlock(t), `<p data-probe>doomed</p>`, true)

	var exceptions atomic.Int32
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	chromedp.ListenTarget(ctx, func(ev any) {
		if _, ok := ev.(*cdruntime.EventExceptionThrown); ok {
			exceptions.Add(1)
		}
	})

	var stillThere, loaded, alive bool
	if err := chromedp.Run(ctx,
		cdruntime.Enable(),
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-probe]')`, &stillThere),
		chromedp.Evaluate(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules['probe-beh'])`, &loaded),
		chromedp.Evaluate(`1 + 1 === 2`, &alive),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if p.hits.Load() == 0 {
		t.Fatal("module was never requested — failure path not exercised")
	}
	if !stillThere {
		t.Error("marker element disappeared after a failed module fetch")
	}
	if loaded {
		t.Error("loadedModules['probe-beh'] is truthy after a failed fetch")
	}
	if !alive {
		t.Error("page stopped evaluating scripts after a failed module fetch")
	}
	if n := exceptions.Load(); n != 0 {
		t.Fatalf("%d uncaught exceptions after a failed module fetch", n)
	}
}

// The global path: window.__gofastr_behaviors assigned by a classic
// script BEFORE runtime.js is honoured when no inline block exists.
func TestBehaviorGlobalManifestPath(t *testing.T) {
	registerProbe(t)
	head := `<script>window.__gofastr_behaviors = {"probe-beh":{"s":["[data-probe]"]}};</script>`
	p := startProbeServer(t, head, `<p data-probe>global path</p>`, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("behaviour declared only through window.__gofastr_behaviors never attached")
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times, want 1", n)
	}
}

// The module is fetched with the ?v=<hash> the module manifest carries:
// the fetched URL must carry the hash runtime.ModuleHash computed.
func TestBehaviorFetchCarriesManifestHash(t *testing.T) {
	registerProbe(t)
	hash := ModuleHash("probe-beh")
	if hash == "" {
		t.Fatal("ModuleHash returned empty for a registered behaviour")
	}
	head := inlineBehaviorsBlock(t) +
		`<script type="application/json" id="gofastr-runtime-modules">{"probe-beh":"` + hash + `"}</script>`
	p := startProbeServer(t, head, `<p data-probe>versioned</p>`, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, attachedExpr) {
		t.Fatal("module never attached; hash-bearing manifest test setup broken")
	}
	got, _ := p.lastURL.Load().(string)
	want := "/__gofastr/runtime/probe-beh.js?v=" + hash
	if got != want {
		t.Fatalf("fetched module URL = %q, want %q", got, want)
	}
}

// depBehaviorJS records, at its own evaluation time, whether its
// requirement had already registered. The requirement is probe-dep,
// whose own script is tiny and sets only its flag: what matters is
// the ORDER, which the dependent observes rather than the test
// inferring from network timing.
const depBehaviorJS = `(function () {
  'use strict';
  var NS = window.__gofastr = window.__gofastr || {};
  var lm = NS.loadedModules = NS.loadedModules || {};
  window.__depReadyAtEval = !!(Object.prototype.hasOwnProperty.call(lm, 'probe-dep') && lm['probe-dep']);
  lm['probe-beh'] = true;
})();`

const depRequirementJS = `(function () {
  'use strict';
  var NS = window.__gofastr = window.__gofastr || {};
  (NS.loadedModules = NS.loadedModules || {})['probe-dep'] = true;
})();`

// depProbeServer counts fetches per module name, delays the
// requirement's script by 300ms (so a dependent that does not wait
// evaluates against a requirement still in flight), and can serve the
// dependent a body that throws before registering.
type depProbeServer struct {
	srv    *httptest.Server
	hits   map[string]*atomic.Int32
	broken atomic.Bool
}

func startDepProbeServer(t *testing.T, head, body string) *depProbeServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	p := &depProbeServer{hits: map[string]*atomic.Int32{"probe-dep": {}, "probe-beh": {}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/probe-dep.js", func(w http.ResponseWriter, r *http.Request) {
		p.hits["probe-dep"].Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(depRequirementJS))
	})
	mux.HandleFunc("/__gofastr/runtime/probe-beh.js", func(w http.ResponseWriter, r *http.Request) {
		p.hits["probe-beh"].Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		if p.broken.Load() {
			w.Write([]byte(`throw new Error('probe exploded before registering');`))
			return
		}
		w.Write([]byte(depBehaviorJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>deps</title>%s</head><body>
  <main role="main"><span id="ready">ready</span>%s</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, head, body)
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// A behaviour with Requires loads its requirement first: the
// dependent's script evaluates only after the requirement registered,
// however long the requirement's fetch took, and both are fetched
// exactly once. The requirement's own marker is nowhere on the page,
// so the requirement is reachable only through the declaration.
func TestBehaviorRequiresLoadsTheRequirementFirst(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-dep", depRequirementJS, registry.Markers("[data-probe-dep]"))
	registry.RegisterBehavior("probe-beh", depBehaviorJS,
		registry.Markers("[data-probe]"), registry.Requires("probe-dep"))

	p := startDepProbeServer(t, inlineBehaviorsBlock(t), `<p data-probe>dependent</p>`)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__depReadyAtEval === true`) {
		t.Fatal("the dependent evaluated before its requirement had registered")
	}
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules['probe-beh'])`) {
		// This fixture's dependent sets no probe attribute; its
		// registration flag is the attachment signal.
		t.Fatal("the dependent never registered")
	}
	if n := p.hits["probe-dep"].Load(); n != 1 {
		t.Fatalf("requirement fetched %d times, want 1", n)
	}
	if n := p.hits["probe-beh"].Load(); n != 1 {
		t.Fatalf("dependent fetched %d times, want 1", n)
	}
}

// A module whose script runs and never registers is not loaded: the
// loader rejects with 'module failed to register', drops the cached
// promise, and a retry after the server serves a good body fetches
// again (hit count 2) and resolves.
func TestBehaviorThatNeverRegistersRejectsAndRetries(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-beh", depBehaviorJS, registry.Markers("[data-probe]"))

	// No marker on the page: the only loads are the two the test makes,
	// so the fetch counts say exactly what the loader did.
	p := startDepProbeServer(t, inlineBehaviorsBlock(t), `<p>no marker</p>`)
	p.broken.Store(true)
	var armed string
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            window.__gofastr.loadModule('probe-beh')
              .then(() => { window.__retry = 'resolved'; },
                    (e) => { window.__retry = 'rejected:' + e.message; });
            return 'armed';
        })()`, &armed),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__retry === 'rejected:module failed to register'`) {
		t.Fatal("a script that ran and never registered did not reject with 'module failed to register'")
	}
	if n := p.hits["probe-beh"].Load(); n != 1 {
		t.Fatalf("module fetched %d times before the retry, want 1", n)
	}
	// Serve the good body and load again: the dropped cached promise
	// means a fresh fetch, not a fulfilled promise for the missing
	// module.
	p.broken.Store(false)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
            window.__retry = '';
            window.__gofastr.loadModule('probe-beh')
              .then(() => { window.__retry = 'resolved'; },
                    (e) => { window.__retry = 'rejected:' + e.message; });
            return 'armed';
        })()`, &armed),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__retry === 'resolved'`) {
		t.Fatal("the retry after the server recovered did not resolve")
	}
	if n := p.hits["probe-beh"].Load(); n != 2 {
		t.Fatalf("module fetched %d times after the retry, want 2", n)
	}
}

// data-fui-prefetch on the dependent warms the requirement too,
// because prefetch goes through loadModule and loadModule is where
// dependencies live.
func TestBehaviorPrefetchWarmsRequirements(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-dep", depRequirementJS, registry.Markers("[data-probe-dep]"))
	registry.RegisterBehavior("probe-beh", depBehaviorJS,
		registry.Markers("[data-probe]"), registry.Requires("probe-dep"))

	p := startDepProbeServer(t, inlineBehaviorsBlock(t), ``)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(p.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            const btn = document.createElement('button');
            btn.setAttribute('data-fui-prefetch', 'probe-beh');
            btn.textContent = 'prefetch';
            document.body.appendChild(btn);
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
        })()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules['probe-beh'])`) {
		t.Fatal("hover prefetch never loaded the dependent")
	}
	if n := p.hits["probe-dep"].Load(); n != 1 {
		t.Fatalf("requirement fetched %d times on hover prefetch, want 1", n)
	}
	if n := p.hits["probe-beh"].Load(); n != 1 {
		t.Fatalf("dependent fetched %d times on hover prefetch, want 1", n)
	}
}

// A malformed behaviours manifest must not take the page down: the
// kernel's _registered reader catches the bad block, registers no
// behaviours, and boots on its own module table. Both delivery shapes
// are covered — a window.__gofastr_behaviors global that is not a
// descriptor map, and an inline #gofastr-behaviors block that is not
// JSON. The probe behaviour's marker IS on the page in both cases, so
// a regression that lets the throw escape (or lets a garbage entry
// load) is visible: the kernel's own reveal module must still load,
// the probe module must not.
func TestBehaviorMalformedManifestsLeaveTheKernelStanding(t *testing.T) {
	cases := []struct {
		name string
		head string
	}{
		{
			name: "global is not a descriptor map",
			head: `<script>window.__gofastr_behaviors = 'certainly not json';</script>`,
		},
		{
			name: "inline block is not JSON",
			head: `<script type="application/json" id="gofastr-behaviors">{"probe-beh": oops</script>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registerProbe(t)
			p := startProbeServer(t, tc.head, `<p data-probe>probe marker</p><p data-fui-reveal="fade-up" id="rev">reveal marker</p>`, false)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(p.srv.URL+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			// The kernel booted far enough to run its own module
			// table: the reveal marker drives a real module load.
			if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.reveal)`) {
				t.Fatal("the kernel's own module table stopped loading after a malformed behaviours manifest")
			}
			// And the broken registry loaded nothing.
			time.Sleep(300 * time.Millisecond)
			if n := p.hits.Load(); n != 0 {
				t.Fatalf("the malformed manifest still loaded the probe module: %d fetches, want 0", n)
			}
			if el := pollTrue(ctx, `!!document.querySelector('[data-probe]')`); !el {
				t.Fatal("the marker element left the page")
			}
		})
	}
}
