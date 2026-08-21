package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// This file pins fix #5: computed + animate tear down their signal
// listeners when their node is detached by a NON-navigate swap
// (island/poll/signal innerHTML). Both modules used to tear down only on
// gofastr:navigate, so a swap that removed a [data-fui-computed] /
// [data-fui-animate-signal] node leaked its recompute/apply closure into
// G._signals[dep].listeners forever (along with the detached DOM node
// it closed over).

func detachPage(body string) string {
	return fmt.Sprintf(`<!doctype html><html><head></head><body>
%s
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, body)
}

// TestAnimate_TearsDownOnNonNavigateDetach: removing an animate node
// without navigating MUST splice its listener out of the signal slot.
func TestAnimate_TearsDownOnNonNavigateDetach(t *testing.T) {
	body := `<div id="host"><div id="t" data-fui-animate-signal="a" data-fui-animate-class="on">x</div></div>`
	base := startPollServer(t, detachPage(body), nil)

	ctx := newPollBrowserCtx(t)
	var before, after float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`(window.__gofastr?._signals?.["a"]?.listeners?.length ?? -1) >= 1`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(`window.__gofastr._signals["a"].listeners.length`, &before),
		// Detach via innerHTML swap on the parent, the island/poll/signal
		// swap path. NO gofastr:navigate fires.
		chromedp.Evaluate(`document.getElementById('host').innerHTML='<span>swapped</span>'`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr._signals["a"].listeners.length`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before < 1 {
		t.Fatalf("test is vacuous: animate node never wired (listeners=%v)", before)
	}
	if after != before-1 {
		t.Errorf("non-navigate detach leaked the animate listener: listeners %v → %v, want %v (the apply closure must be spliced out when its node leaves the DOM without a navigate)", before, after, before-1)
	}
}

// TestComputed_TearsDownOnNonNavigateDetach: same shape for a computed
// node, its recompute closure must leave the dependency signal's
// listeners when the node is swapped away without a navigate.
func TestComputed_TearsDownOnNonNavigateDetach(t *testing.T) {
	body := `<div id="host"><div id="c" data-fui-computed="echo" data-fui-computed-deps="a" data-fui-signal="total">0</div></div>`
	base := startPollServer(t, detachPage(body), nil)

	ctx := newPollBrowserCtx(t)
	var before, after float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Register the reducer the computed node names, then ensure the
		// module is loaded so the node wires (the marker scanner also
		// idle-loads it; this is belt-and-suspenders).
		chromedp.Evaluate(`window.__gofastr._reducers={echo:function(v){return v.a}}; window.__gofastr.loadModule && window.__gofastr.loadModule('computed');`, nil),
		chromedp.Poll(`(window.__gofastr?._signals?.["a"]?.listeners?.length ?? -1) >= 1`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(`window.__gofastr._signals["a"].listeners.length`, &before),
		chromedp.Evaluate(`document.getElementById('host').innerHTML='<span>swapped</span>'`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr._signals["a"].listeners.length`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before < 1 {
		t.Fatalf("test is vacuous: computed node never wired (listeners=%v)", before)
	}
	if after != before-1 {
		t.Errorf("non-navigate detach leaked the computed recompute closure: listeners %v → %v, want %v", before, after, before-1)
	}
}
