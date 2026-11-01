package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// An Escape pressed while an IME is composing cancels the conversion: it
// belongs to the candidate window, so data-cui-clear-on-esc leaves the
// field alone. A plain Escape afterwards still clears it.
func TestClearOnEscSkipsComposing(t *testing.T) {
	page := `<!doctype html><html><head><title>ime</title></head><body>
<input id="clr" data-cui-clear-on-esc value="きょう">
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var composing, plain string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadModule)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.loadModule('widgethelpers'), true`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.widgethelpers)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var c = document.getElementById('clr');
			c.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true, cancelable:true, isComposing:true}));
			return c.value;
		})()`, &composing),
		chromedp.Evaluate(`(function(){
			var c = document.getElementById('clr');
			c.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true, cancelable:true}));
			return c.value;
		})()`, &plain),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if composing != "きょう" {
		t.Fatalf("an Escape mid-composition cleared the field: %q", composing)
	}
	if plain != "" {
		t.Fatalf("a plain Escape after the composition left %q", plain)
	}
}
