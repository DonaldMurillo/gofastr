package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Alt, Ctrl, Meta or Shift with an arrow is the browser's (history
// back and forward, word and line jumps, selection): an open lightbox
// neither steps on it nor cancels it. A bare arrow still steps.
func TestE2E_LightboxSkipsModifiedArrows(t *testing.T) {
	ctx := moduleTestCtx(t, `<div data-cui-widget="lb">
  <div data-cui-comp="ui-lightbox" data-fui-lightbox="lb" data-fui-lightbox-nav="true">
    <button data-fui-lightbox-prev>prev</button><button data-fui-lightbox-next>next</button>
  </div>
</div>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=1.jpg&group=g">1</a>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=2.jpg&group=g">2</a>`)
	if !pollJS(ctx, `!!(window.__gofastr && window.__gofastr.lightbox && window.__gofastr.lightbox.rescan)`) {
		t.Fatal("the lightbox marker never loaded the lightbox module")
	}
	if !pollJS(ctx, moduleLoaded("widgets")) {
		t.Fatal("the widgets module never loaded")
	}
	var prevented string
	var calls string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var G = window.__gofastr;
			G._signals = G._signals || {};
			G._signals.group = {value: 'g'}; G._signals.src = {value: '1.jpg'};
			window.__lbCalls = [];
			G.openWidget = function(name, o){ window.__lbCalls.push(name + ':' + ((o && o.params) || {}).src); };
			G._modalStack.length = 0; G._modalStack.push('lb');
			var out = [];
			['altKey', 'ctrlKey', 'metaKey', 'shiftKey'].forEach(function (mod) {
				['ArrowLeft', 'ArrowRight'].forEach(function (key) {
					var init = {key: key, bubbles: true, cancelable: true};
					init[mod] = true;
					var e = new KeyboardEvent('keydown', init);
					document.body.dispatchEvent(e);
					if (e.defaultPrevented) out.push(mod + '+' + key);
				});
			});
			return out.join(',');
		})()`, &prevented),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.__lbCalls.join(',')`, &calls),
	); err != nil {
		t.Fatal(err)
	}
	if calls != "" || prevented != "" {
		t.Fatalf("a modified arrow drove the lightbox: calls %q, prevented %q", calls, prevented)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.body.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.__lbCalls.join(',')`, &calls),
	); err != nil {
		t.Fatal(err)
	}
	if calls != "lb:2.jpg" {
		t.Fatalf("a bare ArrowRight: calls %q, want lb:2.jpg", calls)
	}
}
