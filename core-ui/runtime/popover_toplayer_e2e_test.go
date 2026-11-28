package runtime

// The anchored popover on the top layer (#706): an open popover widget is
// a manual popover, so it shows above whatever is already in the top
// layer, still placed by the module beside its trigger; closing takes it
// out of the top layer so the next open raises it afresh, and a move that
// takes an open one out of it raises it again.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// until waits for a JS expression to be true, failing with msg when it
// never is.
func until(expr string) chromedp.Action {
	return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
}

const (
	notesRoot   = `document.querySelector('[data-cui-widget="notes"]')`
	notesRaised = `!!` + notesRoot + ` && !` + notesRoot + `.hasAttribute('hidden') && ` + notesRoot + `.matches(':popover-open') && ` + notesRoot + `.hasAttribute('data-cui-popover-side')`
	// coverPage opens a manual popover over the whole page: a menu, a
	// toast stack or a tooltip already in the top layer.
	coverPage = `(() => { const c = document.createElement('div'); c.id = 'cover'; c.popover = 'manual';
		c.style.cssText = 'position: fixed; inset: 0; width: auto; height: auto; margin: 0; background: #ccc';
		document.body.append(c); c.showPopover(); })()`
	// onTop answers what the centre of the note hits.
	onTop = `(() => {
		const r = document.getElementById('note-body').getBoundingClientRect();
		const el = document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2);
		return el && el.id === 'note-body' ? 'on top' : 'covered by ' + (el ? el.id || el.tagName : 'nothing');
	})()`
	runtimeReady = `!!window.__gofastr && typeof window.__gofastr.openWidget === 'function'`
)

func TestAnchoredPopoverOpensAboveTheTopLayer(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var first, second string
	var gap float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		until(runtimeReady),
		chromedp.Evaluate(coverPage, nil),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(notesRaised),
		chromedp.Evaluate(onTop, &first),
		chromedp.Evaluate(notesRoot+`.getBoundingClientRect().top - document.getElementById('bell').getBoundingClientRect().bottom`, &gap),
		chromedp.Evaluate(`window.__gofastr.closeWidget('notes')`, nil),
		until(`!`+notesRoot+` || !`+notesRoot+`.matches(':popover-open')`),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(notesRaised),
		chromedp.Evaluate(onTop, &second),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if first != "on top" {
		t.Fatalf("the open popover over a top-layer cover is %s, want it on top", first)
	}
	if gap < 8 || gap > 12 {
		t.Fatalf("the raised popover sits %vpx under its trigger, want the module's 10px", gap)
	}
	if second != "on top" {
		t.Fatalf("the reopened popover is %s, want it on top again", second)
	}
}

// A popover root that leaves the page while open (a swap's re-attach)
// comes back out of the top layer and rises again.
func TestReattachedPopoverRisesAgain(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		until(runtimeReady),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(notesRaised),
		chromedp.Evaluate(`(() => { `+notesRoot+`.remove(); window.__gofastr._reattachWidgets(); })()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if err := chromedp.Run(ctx, until(notesRaised)); err != nil {
		t.Fatal("after a re-attach the popover did not rise again")
	}
}

// A closing dialog hands the panels it held back to <body>, which takes
// an open popover out of the top layer; once that close is done the
// popover rises again instead of staying hidden.
func TestPopoverMovedByAClosingDialogRisesAgain(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var moved bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		until(runtimeReady),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(notesRaised),
		// What the dialog's teardown does after it announces its close.
		chromedp.Evaluate(`(() => {
			document.dispatchEvent(new CustomEvent('fui:widget-close', {detail: {name: 'dialog'}}));
			document.body.append(`+notesRoot+`);
			return !`+notesRoot+`.matches(':popover-open');
		})()`, &moved),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !moved {
		t.Fatal("the move did not take the popover out of the top layer; the test needs it to")
	}
	if err := chromedp.Run(ctx, until(notesRaised)); err != nil {
		t.Fatal("the popover a closing dialog moved stayed out of the top layer, hidden")
	}
}

// inlineAnchorServer serves a page whose popover root is SSR-inlined and
// hidden, so the runtime hydrates it on open and only re-hides it on
// close.
func inlineAnchorServer(t *testing.T) *httptest.Server {
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
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"cfg":{"name":"notes","hidden":true,"closeOnEscape":true,"closeOnClickOutside":true,"stylePath":"/style/notes.css"},"hidden":true}]`)
	})
	mux.HandleFunc("/style/notes.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.cui-widget { position: fixed; } .cui-widget[data-cui-popover-side] { max-inline-size: 360px; background: #fff; }`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>inline</title></head><body>
  <button id="bell" type="button" data-cui-open="notes" data-cui-popover-anchor="bottom">Notifications</button>
  <div class="cui-widget" data-cui-widget="notes" role="dialog" aria-label="Notifications" hidden><p id="note-body">a note</p></div>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A hydrated popover, only re-hidden on close, leaves the top layer then,
// so when it reopens it rises above a popover opened while it was closed.
func TestHydratedPopoverReopensAboveALaterPopover(t *testing.T) {
	srv := inlineAnchorServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var first, second string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		until(runtimeReady),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(notesRaised),
		chromedp.Evaluate(onTop, &first),
		chromedp.Evaluate(`window.__gofastr.closeWidget('notes')`, nil),
		until(notesRoot+`.hasAttribute('hidden')`),
		chromedp.Evaluate(coverPage, nil),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(`!`+notesRoot+`.hasAttribute('hidden') && `+notesRoot+`.hasAttribute('data-cui-popover-side')`),
		chromedp.Evaluate(onTop, &second),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if first != "on top" {
		t.Fatalf("the hydrated popover opened %s, want it on top", first)
	}
	if second != "on top" {
		t.Fatalf("the reopened hydrated popover is %s, want it above the popover opened while it was closed", second)
	}
}

// openStubbed opens the notes popover after stub has replaced part of the
// browser's API, waits for the module to place it, and answers probe.
func openStubbed(t *testing.T, stub, probe string) string {
	t.Helper()
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var out string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		until(runtimeReady),
		chromedp.Evaluate(stub, nil),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		until(`!!`+notesRoot+` && `+notesRoot+`.hasAttribute('data-cui-popover-side')`),
		chromedp.Evaluate(probe, &out),
	); err != nil {
		t.Fatalf("chromedp: %v (placement did not run)", err)
	}
	return out
}

// A popover the browser refuses to raise (showPopover throws) drops the
// popover attribute, so it still draws in place instead of being hidden
// by the browser's rule for a [popover] out of the top layer.
func TestPopoverTheBrowserWillNotRaiseStillDraws(t *testing.T) {
	shown := openStubbed(t,
		`HTMLElement.prototype.showPopover = function () { throw new DOMException('refused', 'InvalidStateError'); }`,
		`(() => { const w = `+notesRoot+`; return w.hasAttribute('popover') ? 'kept the popover attribute' : getComputedStyle(w).display === 'none' ? 'not drawn' : 'drawn'; })()`)
	if shown != "drawn" {
		t.Fatalf("a popover whose showPopover threw is %s, want it drawn in place", shown)
	}
}

// A browser without the Popover API (no showPopover, and :popover-open an
// invalid selector) still gets the popover placed beside its trigger, as
// before the top layer: placement never asks it about :popover-open.
func TestPopoverWithoutThePopoverAPIIsStillPlaced(t *testing.T) {
	state := openStubbed(t, `(() => {
			delete HTMLElement.prototype.showPopover;
			const m = Element.prototype.matches;
			Element.prototype.matches = function (sel) {
				if (String(sel).includes(':popover-open')) throw new DOMException('not a valid selector', 'SyntaxError');
				return m.call(this, sel);
			};
		})()`,
		`(() => { const w = `+notesRoot+`; return [w.style.position, w.hasAttribute('popover') ? 'popover' : 'no popover'].join(' '); })()`)
	if state != "fixed no popover" {
		t.Fatalf("without the Popover API the popover is %q, want it placed (fixed) with no popover attribute", state)
	}
}
