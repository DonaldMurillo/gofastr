package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The Enter that confirms an IME conversion belongs to the input method,
// so data-cui-submit-on-enter does not submit the form on it. Chrome
// flags that keydown isComposing; Safari sends it with isComposing false
// and the IME's keyCode 229 (found by core-ui/check's keyguard lint). A
// plain Enter afterwards still submits.
func TestSubmitOnEnterSkipsComposing(t *testing.T) {
	page := `<!doctype html><html><head><title>ime</title></head><body>
<form id="f" data-cui-submit-on-enter><textarea id="ta">きょう</textarea></form>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadModule)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__subs = 0; document.getElementById('f').addEventListener('submit', (e) => { e.preventDefault(); window.__subs++; }); true`, nil),
		chromedp.Evaluate(`window.__gofastr.loadModule('widgethelpers'), true`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.widgethelpers)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var ta = document.getElementById('ta');
			ta.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true, isComposing:true}));
			var chrome = window.__subs;
			ta.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', keyCode:229, bubbles:true, cancelable:true}));
			var safari = window.__subs;
			ta.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true}));
			return chrome + '|' + safari + '|' + window.__subs;
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got != "0|0|1" {
		t.Fatalf("[submits after the composing Enter (isComposing)|(keyCode 229)|a plain Enter] = %q, want 0|0|1", got)
	}
}
