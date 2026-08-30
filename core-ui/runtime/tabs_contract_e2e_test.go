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
