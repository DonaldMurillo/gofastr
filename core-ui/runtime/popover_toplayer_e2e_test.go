package runtime

// The anchored popover on the top layer (#706): an open popover widget is
// a manual popover, so it shows above whatever is already in the top
// layer, still placed by the module beside its trigger; closing takes it
// out of the top layer so the next open raises it afresh.

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

func TestAnchoredPopoverOpensAboveTheTopLayer(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	// A manual popover already open over the whole page stands in for a
	// menu, a toast stack or a tooltip in the top layer.
	const cover = `(() => { const c = document.createElement('div'); c.id = 'cover'; c.popover = 'manual';
		c.style.cssText = 'position: fixed; inset: 0; width: auto; height: auto; margin: 0; background: #ccc';
		document.body.append(c); c.showPopover(); })()`
	const probe = `(() => {
		const w = document.querySelector('[data-cui-widget="notes"]');
		if (!w || w.hasAttribute('hidden')) return 'closed';
		if (!w.matches(':popover-open')) return 'not raised';
		const r = document.getElementById('note-body').getBoundingClientRect();
		const el = document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2);
		const b = document.getElementById('bell').getBoundingClientRect();
		if (Math.abs(w.getBoundingClientRect().top - (b.bottom + 10)) > 2) return 'not under the trigger';
		return el && el.id === 'note-body' ? 'on top' : 'covered by ' + (el ? el.id || el.tagName : 'nothing');
	})()`
	var first, closed, second string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(cover, nil),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(probe, &first),
		chromedp.Evaluate(`window.__gofastr.closeWidget('notes')`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => { const w = document.querySelector('[data-cui-widget="notes"]'); return !w ? 'gone' : (w.matches(':popover-open') ? 'still raised' : 'lowered'); })()`, &closed),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(probe, &second),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if first != "on top" {
		t.Fatalf("the open popover over a top-layer cover is %s, want it raised on top, under its trigger", first)
	}
	if closed == "still raised" {
		t.Fatal("closing the popover left its root in the top layer")
	}
	if second != "on top" {
		t.Fatalf("the reopened popover is %s, want it raised on top again", second)
	}
}

// A popover root that leaves the page while open (a swap's re-attach)
// comes back out of the top layer; its next placement raises it again.
func TestReattachedPopoverRisesAgain(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var raised, after string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('[data-cui-widget="notes"]').matches(':popover-open'))`, &raised),
		chromedp.Evaluate(`(() => { const w = document.querySelector('[data-cui-widget="notes"]'); w.remove(); window.__gofastr._reattachWidgets(); })()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => { const w = document.querySelector('[data-cui-widget="notes"]'); return !w ? 'gone' : String(w.matches(':popover-open')); })()`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if raised != "true" {
		t.Fatalf("the open popover is not raised (%s)", raised)
	}
	if after != "true" {
		t.Fatalf("after a re-attach the popover is %s, want it raised again", after)
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
	const probe = `(() => {
		const w = document.querySelector('[data-cui-widget="notes"]');
		if (w.hasAttribute('hidden')) return 'closed';
		const r = document.getElementById('note-body').getBoundingClientRect();
		const el = document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2);
		return el && el.id === 'note-body' ? 'on top' : 'covered by ' + (el ? el.id || el.tagName : 'nothing');
	})()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('bell').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(probe, &first),
		chromedp.Evaluate(`window.__gofastr.closeWidget('notes')`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => { const c = document.createElement('div'); c.id = 'cover'; c.popover = 'manual';
			c.style.cssText = 'position: fixed; inset: 0; width: auto; height: auto; margin: 0; background: #ccc';
			document.body.append(c); c.showPopover(); })()`, nil),
		chromedp.Evaluate(`window.__gofastr.openWidget('notes', document.getElementById('bell'))`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr._anchorPopover('notes', document.getElementById('bell'), 'bottom')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(probe, &second),
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
