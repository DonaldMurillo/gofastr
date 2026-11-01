package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The kernel hands a loaded module's scanner each inserted node, so a
// swap or append whose root IS the marked element must be wired like
// one inside a wrapper. textarea, widgethelpers and dropdown scanned
// descendants only and left such a node dead.
func TestModulesWireInsertedRoot(t *testing.T) {
	page := `<!doctype html><html><head><title>arrival</title></head><body>
<textarea data-cui-autogrow></textarea>
<div data-cui-dropdown-wrap><button data-cui-dropdown aria-expanded="false">a</button><div data-cui-dropdown-panel hidden>p</div></div>
<input id="src" value="abcd">
<div id="slot"></div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	loaded := `(function(){var l=window.__gofastr&&window.__gofastr.loadedModules;return !!(l&&l.textarea&&l.dropdown&&l.widgethelpers);})()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadModule)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.loadModule('widgethelpers'), true`, nil),
		chromedp.Poll(loaded, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// Each marked element is appended on its own, so it is the node
		// the kernel's MutationObserver hands the scanners.
		chromedp.Evaluate(`(function(){
			var slot = document.getElementById('slot');
			function add(html){ var t = document.createElement('template'); t.innerHTML = html; slot.appendChild(t.content.firstElementChild); }
			try { localStorage.setItem('gofastr.persist.k', 'saved'); } catch (_) {}
			add('<textarea id="ta" data-cui-autogrow></textarea>');
			add('<input id="ps" data-cui-persist-storage="k">');
			add('<input id="clr" data-cui-clear-on-esc value="x">');
			add('<span id="cnt" data-cui-charcount-source="#src"></span>');
			add('<span id="tick" data-cui-tick-elapsed="' + (Date.now() - 2000) + '"></span>');
			add('<form id="vf" data-cui-disable-when-invalid><input required><button id="vb" type="submit">go</button></form>');
			add('<form id="ef" data-cui-submit-on-enter><textarea id="et"></textarea></form>');
			document.getElementById('ef').addEventListener('submit', function (e) { e.preventDefault(); window.__entered = true; });
			add('<div id="dd" data-cui-dropdown-wrap data-cui-dropdown-open><button id="ddt" data-cui-dropdown aria-expanded="false">b</button><div data-cui-dropdown-panel hidden>q</div></div>');
		})()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var c = document.getElementById('clr');
			c.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true, cancelable:true}));
			document.getElementById('et').dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true}));
		})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var dead []string
	for _, c := range []struct{ name, expr string }{
		{"textarea autogrow", `document.getElementById('ta').style.height !== ''`},
		{"persist-storage", `document.getElementById('ps').value === 'saved'`},
		{"clear-on-esc", `document.getElementById('clr').value === ''`},
		{"charcount", `document.getElementById('cnt').textContent !== ''`},
		{"tick-elapsed", `document.getElementById('tick').textContent !== ''`},
		{"disable-when-invalid", `document.getElementById('vb').disabled === true`},
		{"submit-on-enter", `window.__entered === true`},
		{"dropdown open state", `document.getElementById('ddt').getAttribute('aria-expanded') === 'true'`},
	} {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(c.expr, &ok)); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !ok {
			dead = append(dead, c.name)
		}
	}
	if len(dead) > 0 {
		t.Errorf("inserted root never wired: %s", strings.Join(dead, ", "))
	}
}
