package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestLightbox_MultiInstanceNoCrossTalk pins fix #6: two Lightbox
// widgets on one page must not cross-talk. The module used a single
// module-scoped `state` plus a first-match findViewer(), so the Prev/Next
// buttons of the SECOND lightbox resolved to the FIRST viewer in DOM
// order, and if that one was closed, recordOpen() set state=null and
// the click was a silent no-op (B's nav was dead whenever A preceded it
// in the DOM).
//
// Setup: lightbox A (closed) precedes lightbox B (open) in the DOM. B
// belongs to a 2-image group and is on the first image. Clicking B's
// Next MUST step B (openWidget called with B's widget name + the second
// image), not A.
func TestLightbox_MultiInstanceNoCrossTalk(t *testing.T) {
	// A is CLOSED (hidden) and first in DOM order, exactly the case
	// where the old first-match findViewer() picked the wrong viewer.
	// B is OPEN and carries the Prev/Next buttons.
	page := fmt.Sprintf(`<!doctype html><html><head></head><body>
<div id="lbA" data-fui-widget="lbA" hidden>
  <div data-fui-comp="ui-lightbox" data-fui-lightbox="lbA" data-fui-lightbox-nav="true">
    <button data-fui-lightbox-prev>A-prev</button><button data-fui-lightbox-next>A-next</button>
  </div>
</div>
<div id="lbB" data-fui-widget="lbB">
  <div data-fui-comp="ui-lightbox" data-fui-lightbox="lbB" data-fui-lightbox-nav="true">
    <button data-fui-lightbox-prev>B-prev</button><button id="bNext" data-fui-lightbox-next>B-next</button>
  </div>
</div>
<a data-fui-lightbox-group="grpB" data-fui-deeplink="src=img1.jpg&group=grpB">1</a>
<a data-fui-lightbox-group="grpB" data-fui-deeplink="src=img2.jpg&group=grpB">2</a>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	base := startPollServer(t, page, nil)

	ctx := newPollBrowserCtx(t)
	var lbCall string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Seed the signal store recordOpen() reads (group + current src).
		chromedp.Evaluate(`window.__gofastr._signals=window.__gofastr._signals||{}; window.__gofastr._signals.group={value:'grpB'}; window.__gofastr._signals.src={value:'img1.jpg'};`, nil),
		// Ensure the lightbox module is loaded so scan() watched both modals.
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.lightbox&&window.__gofastr.lightbox.rescan)`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		// The widgets module self-registers openWidget when it loads, and it
		// is idle-scheduled off the [data-fui-widget] marker this fixture
		// carries. Installing the spy first is a race: if widgets lands
		// afterwards it overwrites the spy, step() calls the real
		// openWidget, and __lbCall stays "", which is exactly the ~8%
		// flake this test had. Wait for it, THEN spy.
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.widgets)`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		// Spy on openWidget, step() calls it with the widget name + parsed params.
		chromedp.Evaluate(`window.__lbCall='';window.__gofastr.openWidget=function(name,opts){window.__lbCall=name+':'+(((opts&&opts.params)||{}).src||'');};`, nil),
		chromedp.Click(`#bNext`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__lbCall`, &lbCall),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if lbCall != "lbB:img2.jpg" {
		t.Errorf("lightbox cross-talk: clicking B's Next produced openWidget(%q), want \"lbB:img2.jpg\" — the click resolved to the wrong lightbox (single module-scoped state + first-match findViewer picked lightbox A, leaving B's nav dead)", lbCall)
	}

	var insertedWatched bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.body.insertAdjacentHTML('beforeend', '<div id="lbC" data-fui-widget="lbC" hidden><div data-fui-comp="ui-lightbox" data-fui-lightbox="lbC"></div></div>')`, nil),
		chromedp.Poll(`document.getElementById('lbC').dataset.fuiLightboxWatched === '1'`, &insertedWatched,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp dynamic lightbox scan: %v", err)
	}
	if !insertedWatched {
		t.Fatalf("dynamically inserted lightbox was not rescanned")
	}
}
