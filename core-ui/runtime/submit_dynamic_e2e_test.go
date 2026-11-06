package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestEnterSubmitsInsertedTextareas(t *testing.T) {
	base := startPollServer(t, `<!doctype html><html><head><title>Dynamic form</title></head><body>
<form id="compose" data-cui-submit-on-enter><div id="part"><textarea id="initial"></textarea></div><input id="other"></form>
<form id="peer" data-cui-submit-on-enter><textarea id="peer-text"></textarea></form>
<script src="/__gofastr/runtime.js"></script></body></html>`, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.Poll(`window.__gofastr?.loadedModules?.widgethelpers`, nil),
		chromedp.Evaluate(`(() => {
			const counts = {compose: 0, peer: 0};
			for (const id of Object.keys(counts)) document.getElementById(id).addEventListener('submit', event => { event.preventDefault(); counts[id]++; });
			const key = (id, type, options = {}) => {
				const event = new KeyboardEvent(type, {key: 'Enter', bubbles: true, cancelable: true, ...options});
				document.getElementById(id).dispatchEvent(event);
				return event.defaultPrevented;
			};
			key('initial', 'keydown');
			document.getElementById('part').innerHTML = '<textarea id="replacement"></textarea>';
			const ignored = [
				key('replacement', 'keydown', {shiftKey: true}),
				key('replacement', 'keydown', {isComposing: true}),
				key('replacement', 'keydown', {keyCode: 229}),
				key('other', 'keydown'), key('other', 'keypress')
			];
			key('replacement', 'keydown');
			const suppressed = key('replacement', 'keypress');
			document.getElementById('part').insertAdjacentHTML('beforeend', '<textarea id="appended"></textarea>');
			key('appended', 'keydown');
			key('peer-text', 'keydown');
			return counts.compose+'|'+counts.peer+'|'+ignored.every(value => !value)+'|'+suppressed;
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got != "3|1|true|true" {
		t.Fatalf("[compose submits|peer submits|modified and non-textarea keys ignored|keypress suppressed] = %q, want 3|1|true|true", got)
	}
}
