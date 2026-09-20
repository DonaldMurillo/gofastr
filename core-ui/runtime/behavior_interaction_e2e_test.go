package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Browser coverage for the interaction bridge's registered-descriptor
// path (docs/spec-behavior-registry.md, "Dependencies and readiness":
// the bridge used to iterate the kernel's own _moduleMarkers table
// only, so a registered behaviour had no interaction trigger). The
// probes here prove the fourth load path honours a descriptor the way
// the marker scan, idle queue and hover prefetch already do: a click
// or keydown that lands while the module is still fetching is
// prevented synchronously, retained, and replayed through the
// module's own handler once it registers.

// probeInteractionJS is a registered behaviour keeping the module
// contract: loadedModules flag FIRST, then the document-level
// listeners whose calls are the replay evidence — the click handler
// records calls on [data-ia-go] nodes, the keydown handler records
// e.key for events from inside [data-ia-scope].
const probeInteractionJS = `(function () {
  'use strict';
  var NAME = 'probe-ia';
  var NS = window.__gofastr = window.__gofastr || {};
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;
  document.addEventListener('click', function (e) {
    var t = e.target && e.target.closest ? e.target.closest('[data-ia-go]') : null;
    if (!t) return;
    window.__iaClicks = (window.__iaClicks || 0) + 1;
    window.__iaClicked = t.id;
  });
  document.addEventListener('keydown', function (e) {
    var t = e.target && e.target.closest ? e.target.closest('[data-ia-scope]') : null;
    if (!t) return;
    window.__iaKeys = window.__iaKeys || [];
    window.__iaKeys.push(e.key);
  });
})();`

// registerInteractionProbe isolates the registry and registers
// probe-ia with the given options beside its marker.
func registerInteractionProbe(t *testing.T, opts ...registry.BehaviorOption) {
	t.Helper()
	registry.IsolateForTest(t)
	all := append([]registry.BehaviorOption{registry.Markers("[data-ia]")}, opts...)
	registry.RegisterBehavior("probe-ia", probeInteractionJS, all...)
}

// iaServer serves the composed runtime, the probe module behind a
// gate (the cold-cache window the bridge exists for), every other
// embedded module, and one page. The module response is held until
// release, so a held dynamic <script> would stall the load event:
// navigate with location.href and wait for a node, the pattern
// lightbox_bridge_e2e_test.go uses for the same reason. tail lands
// AFTER the runtime script: an observer installed there has its
// document listeners registered after the bridge's, so it sees the
// default the bridge prevented.
type iaServer struct {
	srv        *httptest.Server
	hits       atomic.Int32
	requested  chan struct{}
	gate       chan struct{}
	releaseOne sync.Once
}

func (s *iaServer) release() { s.releaseOne.Do(func() { close(s.gate) }) }

func startIAServer(t *testing.T, head, body, tail string) *iaServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("probe-ia")
	if !ok {
		t.Fatal("probe-ia not served by runtime.Module after registration")
	}
	s := &iaServer{requested: make(chan struct{}), gate: make(chan struct{})}
	requestedOnce := sync.Once{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/probe-ia.js", func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		requestedOnce.Do(func() { close(s.requested) })
		<-s.gate
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(mod))
	})
	// Other embedded modules the kernel decides to load must get
	// JavaScript, not the page HTML.
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			_, _ = w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>ia</title>%s</head><body>
  <main role="main"><span id="ready">ready</span>%s</main>
  <script src="/__gofastr/runtime.js"></script>
  <script>%s</script>
</body></html>`, head, body, tail)
	})
	s.srv = httptest.NewServer(mux)
	// LIFO: the gate opens before the server closes, so Close never
	// waits on a held response.
	t.Cleanup(s.srv.Close)
	t.Cleanup(s.release)
	return s
}

// iaGoto navigates without waiting for the load event the held module
// would stall.
func iaGoto(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf("location.href = %q", url), nil),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp setup: %v", err)
	}
}

// iaWaitRequested blocks until the kernel's marker scan has fetched
// the (held) probe module, so the interaction that follows lands
// inside the cold-cache window — and, because the scan runs after
// DOMContentLoaded, until the tail observer script has run too.
func iaWaitRequested(t *testing.T, s *iaServer) {
	t.Helper()
	select {
	case <-s.requested:
	case <-time.After(10 * time.Second):
		t.Fatal("the marker scan never fetched the probe module")
	}
}

const iaLoadedExpr = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['probe-ia'])`

// The tail observers: record, for the control under test, whether the
// event arrived already prevented — a listener registered after the
// runtime's bridge sees the default the bridge prevented
// synchronously, the same evidence pattern the lightbox cold-load
// regression uses.
const iaClickObserver = `
  window.__clickPrevented = null;
  document.addEventListener('click', function (e) {
    if (e.target && e.target.closest && e.target.closest('#go')) {
      window.__clickPrevented = e.defaultPrevented;
    }
  });`

const iaKeyObserver = `
  window.__keyPrevented = null;
  document.addEventListener('keydown', function (e) {
    if (e.target && e.target.id === 'in') window.__keyPrevented = e.defaultPrevented;
  });`

// A click on a registered behaviour's control during its module's
// cold-cache fetch is prevented synchronously, retained, and replayed
// through the module's own handler after it registers — and the replay
// is not doubled: a second real click produces exactly one more call.
func TestBehaviorInteractionClickRetainedAndReplayed(t *testing.T) {
	registerInteractionProbe(t, registry.Interactions(
		registry.Interaction{Event: "click", Selector: "[data-ia-go]"},
	))
	s := startIAServer(t, inlineBehaviorsBlock(t), `
<p data-ia>probe target</p>
<button id="go" type="button" data-ia-go="first">Go</button>`, iaClickObserver)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	iaGoto(t, ctx, s.srv.URL+"/")
	iaWaitRequested(t, s)

	var prevented bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Evaluate(`window.__clickPrevented`, &prevented),
	); err != nil {
		t.Fatalf("chromedp cold-cache click: %v", err)
	}
	if !prevented {
		t.Fatal("the click was not prevented synchronously while the registered module was still fetching")
	}

	s.release()
	if !pollTrue(ctx, iaLoadedExpr) {
		t.Fatal("the registered module never loaded after the gate opened")
	}
	if !pollTrue(ctx, `window.__iaClicks === 1`) {
		t.Fatal("the retained click was not replayed through the module's own handler")
	}
	var clicked string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__iaClicked`, &clicked)); err != nil || clicked != "go" {
		t.Fatalf("the replay missed the original node: __iaClicked = %q (err %v)", clicked, err)
	}
	// Not doubled: one more real click, exactly one more call.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('go').click()`, nil)); err != nil {
		t.Fatalf("chromedp second click: %v", err)
	}
	if !pollTrue(ctx, `window.__iaClicks === 2`) {
		t.Fatal("a real click after load was replayed or counted twice")
	}
}

// A keydown on a registered behaviour's key is retained the same way,
// gated by the scope selector: armed while the scope element exists
// (the key is prevented and replayed), inert while it does not (the
// key is neither prevented nor replayed, even after the module loads).
func TestBehaviorInteractionKeydownScopeGatesRetention(t *testing.T) {
	registerInteractionProbe(t, registry.Interactions(
		registry.Interaction{Event: "keydown", Keys: []string{"Enter"}, Scope: "[data-ia-scope]"},
	))
	head := inlineBehaviorsBlock(t)

	t.Run("armed while the scope exists", func(t *testing.T) {
		s := startIAServer(t, head, `
<p data-ia>probe target</p>
<div data-ia-scope><input id="in" type="text"></div>`, iaKeyObserver)
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		iaGoto(t, ctx, s.srv.URL+"/")
		iaWaitRequested(t, s)

		var prevented bool
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.getElementById('in').focus(); document.getElementById('in').dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true, cancelable: true}))`, nil),
			chromedp.Evaluate(`window.__keyPrevented`, &prevented),
		); err != nil {
			t.Fatalf("chromedp cold-cache keydown: %v", err)
		}
		if !prevented {
			t.Fatal("the keydown was not prevented while the scope matched and the module was fetching")
		}
		s.release()
		if !pollTrue(ctx, iaLoadedExpr) {
			t.Fatal("the registered module never loaded after the gate opened")
		}
		if !pollTrue(ctx, `window.__iaKeys && window.__iaKeys.join(',') === 'Enter'`) {
			t.Fatal("the retained keydown was not replayed through the module's own handler")
		}
	})

	t.Run("inert while the scope does not", func(t *testing.T) {
		s := startIAServer(t, head, `
<p data-ia>probe target</p>
<input id="in" type="text">`, iaKeyObserver)
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		iaGoto(t, ctx, s.srv.URL+"/")
		iaWaitRequested(t, s)

		var prevented bool
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.getElementById('in').focus(); document.getElementById('in').dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true, cancelable: true}))`, nil),
			chromedp.Evaluate(`window.__keyPrevented`, &prevented),
		); err != nil {
			t.Fatalf("chromedp scopeless keydown: %v", err)
		}
		if prevented {
			t.Fatal("the keydown was prevented although the scope selector matched nothing")
		}
		s.release()
		if !pollTrue(ctx, iaLoadedExpr) {
			t.Fatal("the registered module never loaded after the gate opened")
		}
		// Nothing was retained, so nothing replays: the module's own
		// keydown handler never fires.
		if pollTrue(ctx, `window.__iaKeys !== undefined`) {
			t.Fatal("a keydown the scope refused was replayed anyway")
		}
	})
}

// The bridge costs a behaviour that declares no interactions nothing:
// during the same held fetch, a click on the same control is neither
// prevented nor retained — the module registers after release and the
// click never reaches it.
func TestBehaviorWithoutInteractionsGetsNoBridgeListener(t *testing.T) {
	registerInteractionProbe(t) // markers only, no Interactions
	s := startIAServer(t, inlineBehaviorsBlock(t), `
<p data-ia>probe target</p>
<button id="go" type="button" data-ia-go="first">Go</button>`, iaClickObserver)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	iaGoto(t, ctx, s.srv.URL+"/")
	iaWaitRequested(t, s)

	var prevented bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Evaluate(`window.__clickPrevented`, &prevented),
	); err != nil {
		t.Fatalf("chromedp cold-cache click: %v", err)
	}
	if prevented {
		t.Fatal("the bridge prevented a click for a behaviour that declares no interactions")
	}
	if n := s.hits.Load(); n != 1 {
		t.Fatalf("module fetched %d times during the held window, want the single marker-scan fetch", n)
	}
	s.release()
	if !pollTrue(ctx, iaLoadedExpr) {
		t.Fatal("the registered module never loaded after the gate opened")
	}
	if pollTrue(ctx, `window.__iaClicks !== undefined`) {
		t.Fatal("a click that was never retained reached the module after load")
	}
}

// A malformed interactions field must not take the page down. The x
// array feeds the bridge's install loop at boot, outside the
// _registered reader's try, so the reader's filter is what stands
// between a non-array x and a dead boot: the throw stays inside the
// try, the registry is dropped, the kernel boots on its own table.
func TestBehaviorMalformedInteractionsLeaveKernelStanding(t *testing.T) {
	registerInteractionProbe(t, registry.Interactions(
		registry.Interaction{Event: "click", Selector: "[data-ia-go]"},
	))
	head := `<script>window.__gofastr_behaviors = {"probe-ia": {"s": ["[data-ia]"], "x": "garbage"}};</script>`
	s := startIAServer(t, head, `
<p data-ia>probe target</p>
<p data-fui-reveal="fade-up" id="rev">reveal marker</p>`, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	iaGoto(t, ctx, s.srv.URL+"/")

	// The kernel booted far enough to run its own module table.
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.reveal)`) {
		t.Fatal("the kernel's own module table stopped loading after a malformed interactions field")
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("the malformed interactions field still loaded the probe module: %d fetches, want 0", n)
	}
}

// A well-shaped entry whose selector the browser refuses is the case
// the reader's filter cannot catch: the shape is fine, so the entry
// reaches the install loop, and the throw happens later still —
// inside an async listener, on every matching event, as an unhandled
// rejection nothing surfaces. A hand-written behaviours block is the
// way to get one, and the block is a documented global. The guard in
// _interactionNode is what keeps the page working: the selector does
// not resolve a node, which is the same answer as not matching one,
// and the kernel's own interactions still retain.
func TestBehaviorUnresolvableSelectorLeavesThePageWorking(t *testing.T) {
	registerInteractionProbe(t, registry.Interactions(
		registry.Interaction{Event: "click", Selector: "[data-ia-go]"},
	))
	// Two entries the Go registry would have refused: a click whose
	// selector querySelector throws on, and a keydown with no scope at
	// all, where the kernel resolves querySelector('') — also a throw.
	head := `<script>window.__gofastr_behaviors = {"probe-ia": {"s": ["[data-ia]"], "x": [` +
		`{"event": "click", "selector": "!!!"},` +
		`{"event": "keydown", "keys": ["Enter"]}` +
		`]}};</script>`
	s := startIAServer(t, head, `
<p data-ia>probe target</p>
<button id="go" data-ia-go>go</button>
<input id="k">
<p data-fui-reveal="fade-up" id="rev">reveal marker</p>`, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	iaGoto(t, ctx, s.srv.URL+"/")

	// The kernel boots and its own table still works.
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.reveal)`) {
		t.Fatal("the kernel stopped loading its own modules after an unresolvable selector")
	}
	// Record anything the listeners throw, then drive both events.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		window.__iaErrors = [];
		window.addEventListener('error', (e) => window.__iaErrors.push(String(e.message)));
		window.addEventListener('unhandledrejection', (e) => window.__iaErrors.push(String(e.reason)));
		true;`, nil)); err != nil {
		t.Fatalf("install the error sink: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click("#go", chromedp.ByID),
		chromedp.SendKeys("#k", "\r", chromedp.ByID),
	); err != nil {
		t.Fatalf("drive the events: %v", err)
	}
	// The bridge's listener is async, so a throw inside it is a rejected
	// promise, and unhandledrejection is reported a turn later than the
	// click. Reading the sink immediately would pass whether the guard
	// is there or not.
	var errs []string
	for i := 0; i < 40; i++ {
		if err := chromedp.Run(ctx, chromedp.Sleep(50*time.Millisecond),
			chromedp.Evaluate(`window.__iaErrors`, &errs)); err != nil {
			t.Fatalf("read the error sink: %v", err)
		}
		if len(errs) != 0 {
			break
		}
	}
	if len(errs) != 0 {
		t.Fatalf("an unresolvable selector threw in the bridge: %v", errs)
	}
	// And the page is still live: the kernel's own reveal module
	// answers a rescan, which a dead boot pass could not.
	if !pollTrue(ctx, `typeof window.__gofastr.loadModule === 'function'`) {
		t.Fatal("the kernel's loader is gone after an unresolvable selector")
	}
}
