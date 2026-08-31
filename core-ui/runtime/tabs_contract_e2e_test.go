package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// startTabsContractServer serves the runtime, the demand modules, and a page
// carrying a strip shaped exactly like framework/ui.Tabs SSR with the #320
// contract knobs on. The stash JSON is built with the same marshal + </ →
// <\/ pipeline the component uses, so the fixture cannot drift from what
// production emits.
//
// extra HTML is spliced in before the runtime script tag.
func startTabsContractServer(t *testing.T, body string) *httptest.Server {
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
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>tabs-contract</title></head><body>
%s
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runTabsContractBrowser(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chromedp start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chromedp start timeout")
	}
	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(func() {
		cancel()
		browserCancel()
		allocCancel()
	})
	return ctx, cancel
}

// vacateStripPage builds a two-tab strip with VacateHidden + StateAttrs on,
// mirroring the component's SSR: inactive panel empty, its content in the
// stash script, wrapper carrying both markers and the prefetch bridge.
func vacateStripPage(t *testing.T, panel1 string) string {
	t.Helper()
	stash := map[string]string{"1": panel1}
	buf, err := json.Marshal(stash)
	if err != nil {
		t.Fatal(err)
	}
	stashJSON := strings.ReplaceAll(string(buf), `</`, `<\/`)
	return `<div id="wrap" class="fui-tabs" data-active="0" data-fui-signal="vsig"
       data-fui-signal-mode="attr" data-fui-signal-attr="data-active"
       data-fui-tabs-state="true" data-fui-tabs-vacate="true" data-fui-prefetch="tabs">
  <nav role="tablist">
    <button id="t0" role="tab" aria-selected="true" data-state="active" data-fui-tab-index="0" data-fui-signal-set="vsig:0">A</button>
    <button id="t1" role="tab" aria-selected="false" data-state="inactive" data-fui-tab-index="1" data-fui-signal-set="vsig:1">B</button>
  </nav>
  <div class="fui-tabs-content">
    <div role="tabpanel" data-fui-tab-index="0"><p id="p0txt">alpha-body</p></div>
    <div role="tabpanel" data-fui-tab-index="1"></div>
    <script type="application/json" data-fui-tabs-stash="true">` + stashJSON + `</script>
  </div>
</div>`
}

// TestTabsVacateHiddenPanelsAndRestore pins the VacateHidden runtime
// contract end to end: hidden panel content is absent from the document (not
// just CSS-hidden), a switch restores it from the stash, the outgoing
// panel's LIVE nodes are stashed, and a later re-show restores those same
// nodes — island DOM the runtime swapped in and form state survive the
// round-trip, which is what distinguishes node-stashing from re-rendering
// the stash HTML.
func TestTabsVacateHiddenPanelsAndRestore(t *testing.T) {
	panel1 := `<p id="p1txt">beta-body</p><input id="p1in" value="">`
	srv := startTabsContractServer(t, vacateStripPage(t, panel1))
	ctx, _ := runTabsContractBrowser(t)

	var ok bool
	// Load the module through the real trigger: the kernel's prefetch
	// bridge listens for pointerover in the capture phase, which is what
	// arms the strip before a user can click.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#t1`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp (load module via prefetch trigger): %v", err)
	}

	// Vacated means absent from the document, even with the module live.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('p1txt') === null && !document.body.innerText.includes('beta-body')`, &ok),
	); err != nil || !ok {
		t.Fatalf("hidden panel content must be absent from the DOM after module load (ok=%v, err=%v)", ok, err)
	}

	// Switch to tab 1: the stash restores the panel's markup.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#t1`, chromedp.ByID),
		chromedp.Poll(`(() => { const p = document.getElementById('p1txt');
			return p !== null && p.innerText === 'beta-body'; })()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil || !ok {
		t.Fatalf("switch to tab 1 must restore its stashed content (ok=%v, err=%v)", ok, err)
	}

	// The outgoing panel's live nodes are detached, not display:none'd.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('p0txt') === null && !document.body.innerText.includes('alpha-body')`, &ok),
	); err != nil || !ok {
		t.Fatalf("outgoing panel must be vacated from the DOM (ok=%v, err=%v)", ok, err)
	}

	// Simulate the runtime having swapped island content into panel 1,
	// plus user form state.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('p1txt').textContent = 'beta-SWAPPED';
			document.getElementById('p1in').value = 'typed';`, nil),
		chromedp.Click(`#t0`, chromedp.ByID),
		chromedp.Poll(`(() => document.getElementById('p0txt') !== null &&
			document.getElementById('p1txt') === null)()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil || !ok {
		t.Fatalf("switch back to tab 0 must restore panel 0 and vacate panel 1 (ok=%v, err=%v)", ok, err)
	}

	// Re-show panel 1: the SAME live nodes come back — swapped content
	// and the input's value survive. An innerHTML re-render from the
	// stash would lose both (value="" and 'beta-body').
	var swapped, typed, active string
	if err := chromedp.Run(ctx,
		chromedp.Click(`#t1`, chromedp.ByID),
		chromedp.Poll(`(() => {
			const p = document.getElementById('p1txt');
			return p !== null && p.textContent === 'beta-SWAPPED' &&
				document.getElementById('p1in').value === 'typed'; })()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`document.getElementById('wrap').getAttribute('data-active')`, &active),
		chromedp.Evaluate(`document.getElementById('t1').getAttribute('data-state')`, &swapped),
		chromedp.Evaluate(`document.getElementById('t0').getAttribute('data-state')`, &typed),
	); err != nil || !ok {
		t.Fatalf("re-show must restore the same live nodes (ok=%v, err=%v)", ok, err)
	}
	if active != "1" {
		t.Errorf("data-active after switching to tab 1 = %q, want 1", active)
	}
	if swapped != "active" {
		t.Errorf("data-state on clicked tab = %q, want active", swapped)
	}
	if typed != "inactive" {
		t.Errorf("data-state on previous tab = %q, want inactive", typed)
	}
}

// TestTabsStateAttrMirrorsAfterClick covers the StateAttrs contract on a
// strip WITHOUT VacateHidden: SSR ships the correct data-state, and the
// module keeps it in step with data-active after client-side switches.
func TestTabsStateAttrMirrorsAfterClick(t *testing.T) {
	page := `<div id="wrap" class="fui-tabs" data-active="0" data-fui-signal="ssig"
       data-fui-signal-mode="attr" data-fui-signal-attr="data-active"
       data-fui-tabs-state="true" data-fui-prefetch="tabs">
  <nav role="tablist">
    <button id="t0" role="tab" aria-selected="true" data-state="active" data-fui-tab-index="0" data-fui-signal-set="ssig:0">A</button>
    <button id="t1" role="tab" aria-selected="false" data-state="inactive" data-fui-tab-index="1" data-fui-signal-set="ssig:1">B</button>
  </nav>
  <div class="fui-tabs-content">
    <div role="tabpanel" data-fui-tab-index="0">a</div>
    <div role="tabpanel" data-fui-tab-index="1">b</div>
  </div>
</div>`
	srv := startTabsContractServer(t, page)
	ctx, _ := runTabsContractBrowser(t)

	var ok bool
	var s0, s1 string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#t1`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Click(`#t1`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('t1').getAttribute('data-state') === 'active' &&
			document.getElementById('t0').getAttribute('data-state') === 'inactive'`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`document.getElementById('t0').getAttribute('data-state')`, &s0),
		chromedp.Evaluate(`document.getElementById('t1').getAttribute('data-state')`, &s1),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if s0 != "inactive" || s1 != "active" {
		t.Errorf("after clicking tab 1: data-state = [%q, %q], want [inactive, active]", s0, s1)
	}
}

// vacateStrip builds one two-tab strip in the component's exact SSR
// shape (VacateHidden + StateAttrs, panel 1 parked in the stash through
// the same marshal + </ → <\/ pipeline), with caller-chosen ids, signal
// name, and panel-0 content. Shared by the nested-strip and
// programmatic-switch probes below.
func vacateStrip(t *testing.T, id, sig, panel0, panel1 string) string {
	t.Helper()
	buf, err := json.Marshal(map[string]string{"1": panel1})
	if err != nil {
		t.Fatal(err)
	}
	stashJSON := strings.ReplaceAll(string(buf), `</`, `<\/`)
	return `<div id="` + id + `" class="fui-tabs" data-active="0" data-fui-signal="` + sig + `"
       data-fui-signal-mode="attr" data-fui-signal-attr="data-active"
       data-fui-tabs-state="true" data-fui-tabs-vacate="true" data-fui-prefetch="tabs">
  <nav role="tablist">
    <button id="` + id + `-t0" role="tab" aria-selected="true" data-state="active" data-fui-tab-index="0" data-fui-signal-set="` + sig + `:0">A</button>
    <button id="` + id + `-t1" role="tab" aria-selected="false" data-state="inactive" data-fui-tab-index="1" data-fui-signal-set="` + sig + `:1">B</button>
  </nav>
  <div class="fui-tabs-content">
    <div role="tabpanel" data-fui-tab-index="0"><p id="` + id + `-p0">` + panel0 + `</p></div>
    <div role="tabpanel" data-fui-tab-index="1"></div>
    <script type="application/json" data-fui-tabs-stash="true">` + stashJSON + `</script>
  </div>
</div>`
}

// TestTabsNestedStripWiredAfterRestore pins the scanner's root-matching
// contract. Core's DOM-insertion observer hands each module scanner the
// ADDED NODE, not a container: when an outer vacate panel's first-show
// restore writes its innerHTML, the inner strip IS that node. A scanner
// that only queries descendants never wires it — the inner tabs then
// flip data-active and CSS-show an EMPTY panel forever, content inert
// in the stash. Same failure shape as an island/RPC/html-signal region
// swap whose response root is a strip.
func TestTabsNestedStripWiredAfterRestore(t *testing.T) {
	inner := vacateStrip(t, "iwrap", "isig", "inner-alpha", `<p id="ip1">inner-beta</p>`)
	outer := vacateStrip(t, "owrap", "osig", "outer-alpha", inner)
	srv := startTabsContractServer(t, outer)
	ctx, _ := runTabsContractBrowser(t)

	var ok bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#owrap-t1`, chromedp.ByID),
		// Load the module through the real trigger (prefetch bridge).
		chromedp.Evaluate(`document.getElementById('owrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		// Outer switch: first-show restore writes the inner strip into
		// the panel via innerHTML. The MutationObserver hands the INNER
		// STRIP (the added node) to the tabs scanner.
		chromedp.Click(`#owrap-t1`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('iwrap') !== null &&
			document.getElementById('iwrap-t1') !== null`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		// Inner switch: the inner strip must have been wired as the
		// scope ROOT, or panel 1 stays empty for the page lifetime.
		chromedp.Click(`#iwrap-t1`, chromedp.ByID),
		chromedp.Poll(`(() => { const p = document.getElementById('ip1');
			return p !== null && p.innerText === 'inner-beta'; })()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil || !ok {
		t.Fatalf("inner strip nested in a restored vacate panel must be wired and restore its own stash (ok=%v, err=%v)", ok, err)
	}
}

// TestTabsRestoreLoadsComponentCSS pins the scanAndLoadCSS half of the
// restore pipeline: panel HTML can carry data-fui-comp components, and
// their stylesheets must load when the content is restored — the same
// insertion pipeline html-mode signal regions use.
func TestTabsRestoreLoadsComponentCSS(t *testing.T) {
	page := `<script>window.__gofastr_catalog = {"probe-card":{stylePath:"/__gofastr/comp/probe-card.css",version:"1"}};</script>` +
		vacateStripPage(t, `<div data-fui-comp="probe-card" id="pc">card-body</div>`)
	srv := startTabsContractServer(t, page)
	ctx, _ := runTabsContractBrowser(t)

	var ok bool
	var href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#t1`, chromedp.ByID),
		// Nothing may load at boot: the component marker only exists
		// as text inside the stash script, which no selector matches.
		chromedp.Evaluate(`document.querySelector('link[data-fui-style="probe-card"]') === null`, &ok),
	); err != nil || !ok {
		t.Fatalf("probe-card CSS must not load before the panel is restored (ok=%v, err=%v)", ok, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Click(`#t1`, chromedp.ByID),
		chromedp.Poll(`(() => { const l = document.querySelector('link[data-fui-style="probe-card"]');
			return l !== null && document.getElementById('pc') !== null; })()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`document.querySelector('link[data-fui-style="probe-card"]').getAttribute('href')`, &href),
	); err != nil || !ok {
		t.Fatalf("restoring a panel with a data-fui-comp element must load its stylesheet (ok=%v, err=%v)", ok, err)
	}
	if want := "/__gofastr/comp/probe-card.css?v=1"; href != want {
		t.Errorf("probe-card link href = %q, want %q", href, want)
	}
}

// TestTabsProgrammaticSwitchBeforeModuleLoad pins the initial apply() at
// wire time. The module loads on first pointerover/focusin, but a signal
// write can move data-active BEFORE that (an SSE/poll/RPC-driven update,
// or a hydration-time value differing from SSR). The write flips
// data-active while nobody observes it; when the module finally loads,
// its wiring pass must reconcile against the CURRENT data-active, or the
// newly-active panel stays empty until the next manual switch.
func TestTabsProgrammaticSwitchBeforeModuleLoad(t *testing.T) {
	srv := startTabsContractServer(t, vacateStripPage(t, `<p id="p1txt">beta-body</p>`))
	ctx, _ := runTabsContractBrowser(t)

	var ok, moduleLoaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#t1`, chromedp.ByID),
		// A synthetic .click() carries no pointerover, so the prefetch
		// bridge stays asleep: the signal write happens while the tabs
		// module is NOT loaded. Core still mirrors aria-selected.
		chromedp.Evaluate(`document.getElementById('t1').click()`, nil),
		chromedp.Poll(`document.getElementById('wrap').getAttribute('data-active') === '1' &&
			document.getElementById('t1').getAttribute('aria-selected') === 'true'`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`, &moduleLoaded),
		chromedp.Evaluate(`document.getElementById('p1txt') === null`, &ok),
	); err != nil || !ok || moduleLoaded {
		t.Fatalf("setup: switch must land before module load (active ok=%v, panel empty ok=%v, moduleLoaded=%v, err=%v)", ok, ok, moduleLoaded, err)
	}

	// First interaction loads the module; wiring must immediately
	// reconcile against data-active=1 and restore panel 1.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`(() => { const p = document.getElementById('p1txt');
			return p !== null && p.innerText === 'beta-body'; })()`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil || !ok {
		t.Fatalf("module load must reconcile the pre-interaction switch and restore panel 1 (ok=%v, err=%v)", ok, err)
	}
}
