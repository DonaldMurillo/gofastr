package runtime

// Widget lifecycle e2e (#409): app-wide widget roots across SPA layer
// swaps, and the fui:widget-open / fui:widget-close contract.
//
// The fixture reproduces the host-layout shape from the issue: the SSR
// page inlines the panel widget's chrome INSIDE the layout shell
// element (the framework's own layouts inline before </body>, outside
// the shell; hosts that build their own layout wrapper have wrapped
// the chrome inside it). Cross-chain navigation replaces that shell
// wholesale, which is what tore the drawer out on the docs site.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type widgetLifecycleSite struct {
	srv        *httptest.Server
	chromeHits *atomic.Int32
}

// startWidgetLifecycleServer wires two disjoint single-layout chains
// (/one/* under l:one, /two/* under l:two; cross-chain nav swaps the
// whole shell, in-chain nav swaps the layout slot) plus two app-wide
// widgets with no page scoping:
//
//	appwide-panel  non-hidden, SSR-inlined inside the shell on /one pages
//	appwide-drawer hidden + backdrop, opened via data-fui-open, chrome
//	               fetched lazily (root lands on <body>)
//
// An inline script installed before runtime.js records every
// fui:widget-open / fui:widget-close detail for later assertions.
func startWidgetLifecycleServer(t *testing.T) *widgetLifecycleSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var chromeHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
{"hidden":false,"cfg":{"name":"appwide-panel","chromePath":"/chrome/panel","stylePath":"/style.css"}},
{"hidden":true,"cfg":{"name":"appwide-drawer","chromePath":"/chrome/drawer","stylePath":"/style.css","backdrop":true,"closeOnEscape":true,"closeOnClick":true}}
]`)
	})
	mux.HandleFunc("/chrome/panel", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div data-fui-widget="appwide-panel" id="panel-fetched">Fetched panel</div>`)
	})
	mux.HandleFunc("/chrome/drawer", func(w http.ResponseWriter, _ *http.Request) {
		chromeHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div data-fui-widget="appwide-drawer" id="drawer-root"><button data-fui-action="close">×</button>Drawer</div>`)
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, "[hidden]{display:none}")
	})

	routes := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/one/a","layouts":["l:one"]},{"path":"/one/b","layouts":["l:one"]},` +
		`{"path":"/two","layouts":["l:two"]},{"path":"/two/b","layouts":["l:two"]}]` +
		`</script>`
	header := `<header><a id="to-b" href="/one/b">B</a><a id="to-one-a" href="/one/a">OneA</a>` +
		`<a id="to-two" href="/two">Two</a><a id="to-two-b" href="/two/b">TwoB</a>` +
		`<button id="open-drawer" data-fui-open="appwide-drawer">Drawer</button></header>`
	events := `<script>
window.__wopens=[];window.__wcloses=[];
document.addEventListener('fui:widget-open',function(e){window.__wopens.push(e.detail);});
document.addEventListener('fui:widget-close',function(e){window.__wcloses.push(e.detail);});
</script>`
	onePage := func(screen string) string {
		return `<!doctype html><html><head><title>t</title>` + routes + `</head><body>` + events +
			`<div data-fui-layout="one" data-fui-layout-key="l:one">` + header +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:one"><h1 id="` + screen + `">` + screen + `</h1></main>` +
			`<div data-fui-widget="appwide-panel" id="panel-root">Panel SSR chrome</div>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	twoPage := func(screen string) string {
		return `<!doctype html><html><head><title>t</title>` + routes + `</head><body>` + events +
			`<div data-fui-layout="two" data-fui-layout-key="l:two">` + header +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:two"><h1 id="` + screen + `">` + screen + `</h1></main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	partial := func(w http.ResponseWriter, swap, screen string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", screen)
		w.Header().Set("X-Gofastr-Swap", swap)
		fmt.Fprint(w, `<h1 id="`+screen+`">`+screen+`</h1>`)
	}
	for path, screen := range map[string]string{"/one/a": "screen-one-a", "/one/b": "screen-one-b"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				partial(w, "l:one", screen)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, onePage(screen))
		})
	}
	for path, screen := range map[string]string{"/two": "screen-two", "/two/b": "screen-two-b"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				partial(w, "l:two", screen)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, twoPage(screen))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &widgetLifecycleSite{srv: srv, chromeHits: &chromeHits}
}

var widgetPollOpts = chromedp.WithPollingTimeout(10 * time.Second)

// TestAppwideWidgetRootsSurviveShellSwap pins the DOM lifetime contract
// for app-wide widgets (#409):
//
//   - a registered root torn out by a full-shell swap is re-appended to
//     <body> and announced with fui:widget-open {reinserted: true};
//   - an SSR-hydrated root riding above the swap slot is untouched by
//     in-chain navigation (no event, same parent);
//   - an open modal drawer is closed by the navigate listener (root
//     dropped, fui:widget-close fired), and reopening re-fetches its
//     chrome and announces fui:widget-open again;
//   - fui:widget-open detail.root is the mounted [data-fui-widget]
//     element on both the fetched and the SSR-hydrated path.
func TestAppwideWidgetRootsSurviveShellSwap(t *testing.T) {
	site := startWidgetLifecycleServer(t)
	ctx := newSeedBrowserCtx(t)

	var panelHydrated map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/one/a"),
		chromedp.WaitVisible(`#screen-one-a`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr._widgets&&window.__gofastr._widgets['appwide-panel'])`, nil, widgetPollOpts),
		// The panel hydrated the SSR-inlined node in place (inside the
		// shell), and the open event names that exact element.
		chromedp.Evaluate(`(function(){
var o=window.__wopens.filter(function(d){return d.name==='appwide-panel';});
var root=document.getElementById('panel-root');
return o.length===1&&o[0].root===root&&o[0].hydrated===true&&o[0].reinserted===false&&
  root.isConnected&&!!root.closest('[data-fui-layout-key]')?
  {ok:true,rootIsBodyChild:false}:o;})()`, &panelHydrated),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ok, _ := panelHydrated["ok"].(bool); !ok {
		t.Errorf("panel hydrate/open event wrong on /one/a: %+v", panelHydrated)
	}

	// Open the drawer: fetched chrome, root on <body>.
	var drawerOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#open-drawer`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('[data-fui-widget="appwide-drawer"]')`, nil, widgetPollOpts),
		chromedp.Evaluate(`(function(){
var o=window.__wopens.filter(function(d){return d.name==='appwide-drawer';});
var root=document.querySelector('[data-fui-widget="appwide-drawer"]');
return o.length===1&&o[0].root===root&&o[0].hydrated===false&&o[0].reinserted===false&&
  root.parentElement===document.body&&
  !!document.querySelector('[data-fui-backdrop="appwide-drawer"]');})()`, &drawerOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !drawerOpen {
		t.Error("drawer open event must carry the fetched [data-fui-widget] root appended to <body>")
	}
	if got := site.chromeHits.Load(); got != 1 {
		t.Errorf("drawer chrome fetches after first open = %d, want 1", got)
	}

	// In-chain navigation: the open drawer (modal) is closed and its
	// root dropped; the panel above the swap slot is untouched.
	var inChain map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-one-b`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(function(){
var c=window.__wcloses.filter(function(d){return d.name==='appwide-drawer';});
var panelEvents=window.__wopens.filter(function(d){return d.name==='appwide-panel';});
var G=window.__gofastr;
return {
 drawerClosedEvent:c.length===1&&!!c[0].root,
 drawerGone:!document.querySelector('[data-fui-widget="appwide-drawer"]')&&
   !G._widgets['appwide-drawer'],
 backdropGone:!document.querySelector('[data-fui-backdrop="appwide-drawer"]'),
 panelUntouched:panelEvents.length===1&&document.getElementById('panel-root').isConnected&&
   !!document.getElementById('panel-root').closest('[data-fui-layout-key]'),
};})()`, &inChain),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for k, v := range inChain {
		if ok, _ := v.(bool); !ok {
			t.Errorf("in-chain nav /one/a -> /one/b: %s failed (state %+v)", k, inChain)
		}
	}

	// Reopen on /one/b: the navigate cleared the chrome cache, so this
	// re-fetches (the documented drawer lifetime) and fires open again.
	var reopened bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#open-drawer`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('[data-fui-widget="appwide-drawer"]')`, nil, widgetPollOpts),
		chromedp.Evaluate(`window.__wopens.filter(function(d){return d.name==='appwide-drawer';}).length===2`, &reopened),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !reopened {
		t.Error("reopening the drawer after close must announce fui:widget-open again")
	}
	if got := site.chromeHits.Load(); got != 2 {
		t.Errorf("drawer chrome fetches after reopen = %d, want 2 (cache cleared on navigate)", got)
	}

	// Cross-chain navigation: whole-shell swap. The drawer (open, modal)
	// closes again; the panel root, torn out with the old shell, must be
	// re-appended to <body> and re-announced with reinserted: true.
	var crossChain map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Click(`#to-two`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-two`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(function(){
var re=window.__wopens.filter(function(d){return d.name==='appwide-panel'&&d.reinserted===true;});
var root=document.getElementById('panel-root');
return {
 panelAlive:root&&root.isConnected&&root.parentElement===document.body,
 panelReinserted:re.length===1&&re[0].root===root&&re[0].hydrated===true,
 drawerClosed:window.__wcloses.filter(function(d){return d.name==='appwide-drawer';}).length===2,
 drawerGone:!document.querySelector('[data-fui-widget="appwide-drawer"]'),
};})()`, &crossChain),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for k, v := range crossChain {
		if ok, _ := v.(bool); !ok {
			t.Errorf("cross-chain nav /one/b -> /two: %s failed (state %+v)", k, crossChain)
		}
	}

	// In-chain on the new chain: the reattached root sits on <body>,
	// outside the swapped slot, so it stays put with no new event.
	var settled bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#to-two-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-two-b`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(function(){
var n=window.__wopens.filter(function(d){return d.name==='appwide-panel';}).length;
var root=document.getElementById('panel-root');
return n===2&&root.isConnected&&root.parentElement===document.body;})()`, &settled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !settled {
		t.Error("reattached panel root must stay on <body> across in-chain swaps with no new open event")
	}
}

// TestSwappedSSRPanelMovesToFreshInline pins the other half of the
// lifetime contract: when the destination page SSR-inlines its OWN copy
// of the widget inside the new shell (hosts that nest chrome in the
// shell inline it on every page), the stale runtime-inserted/hydrated
// instance is dismissed (fui:widget-close, root lifted out) and the
// catalog pass hydrates the fresh node (fui:widget-open, hydrated).
// Two live roots for one registration must never coexist.
func TestSwappedSSRPanelMovesToFreshInline(t *testing.T) {
	site := startWidgetLifecycleServer(t)
	ctx := newSeedBrowserCtx(t)

	var done map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/one/a"),
		chromedp.WaitVisible(`#screen-one-a`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr._widgets&&window.__gofastr._widgets['appwide-panel'])`, nil, widgetPollOpts),
		// Stamp the live root so the two panel nodes stay identifiable
		// once the second one arrives with the swapped-in shell.
		chromedp.Evaluate(`document.getElementById('panel-root').dataset.stamp='old'`, nil),
		chromedp.Click(`#to-two`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-two`, chromedp.ByID),
		// /two does not inline the panel: the old root is reattached.
		chromedp.Poll(`!!document.querySelector('[data-fui-widget="appwide-panel"][data-stamp="old"]')&&document.querySelector('[data-fui-widget="appwide-panel"][data-stamp="old"]').parentElement===document.body`, nil, widgetPollOpts),
		chromedp.Click(`#to-one-a`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-one-a`, chromedp.ByID),
		// The fresh /one/a shell inlines a new panel node; the runtime
		// must trade the stale reattached instance for it.
		chromedp.Poll(`(function(){
var G=window.__gofastr;
var reg=G._widgets&&G._widgets['appwide-panel'];
if(!reg||!reg.root||reg.root.dataset.stamp==='old')return false;
return reg.root.isConnected&&!!reg.root.closest('[data-fui-layout-key]')&&
  !document.querySelector('[data-fui-widget="appwide-panel"][data-stamp="old"]');})()`, nil, widgetPollOpts),
		chromedp.Evaluate(`(function(){
var old=document.querySelector('[data-fui-widget="appwide-panel"][data-stamp="old"]');
var G=window.__gofastr;
var reg=G._widgets['appwide-panel'];
var cl=window.__wcloses.filter(function(d){return d.name==='appwide-panel'&&d.root&&d.root.dataset.stamp==='old';});
var op=window.__wopens.filter(function(d){return d.name==='appwide-panel'&&d.root===reg.root;});
return {oldGone:!old, registrationMoved:reg.root.id==='panel-root',
 closeAnnounced:cl.length===1, openAnnounced:op.length===1&&op[0].hydrated===true&&op[0].reinserted===false};})()`, &done),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for k, v := range done {
		if ok, _ := v.(bool); !ok {
			t.Errorf("return nav /two -> /one/a: %s failed (state %+v)", k, done)
		}
	}
}
