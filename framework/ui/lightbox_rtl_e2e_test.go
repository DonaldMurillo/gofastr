package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The toolbar lays Prev at the inline start, so in a right-to-left
// lightbox Prev is on the right: ArrowRight goes back and ArrowLeft
// goes on. The arrows kept their left-to-right meaning and moved
// against the buttons (found by core-ui/check's keyguard lint).
func TestE2E_LightboxArrowsFlipInRTL(t *testing.T) {
	ctx := moduleTestCtx(t, `<div dir="rtl"><div data-cui-widget="lb">
  <div data-cui-comp="ui-lightbox" data-fui-lightbox="lb" data-fui-lightbox-nav="true">
    <button data-fui-lightbox-prev>prev</button><button data-fui-lightbox-next>next</button>
  </div>
</div></div>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=1.jpg&group=g">1</a>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=2.jpg&group=g">2</a>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=3.jpg&group=g">3</a>`)
	if !pollJS(ctx, `!!(window.__gofastr && window.__gofastr.lightbox && window.__gofastr.lightbox.rescan)`) {
		t.Fatal("the lightbox marker never loaded the lightbox module")
	}
	if !pollJS(ctx, moduleLoaded("widgets")) {
		t.Fatal("the widgets module never loaded")
	}
	var calls string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var G = window.__gofastr;
			G._signals = G._signals || {};
			G._signals.group = {value: 'g'}; G._signals.src = {value: '2.jpg'};
			window.__lbCalls = [];
			G.openWidget = function(name, o){ window.__lbCalls.push(((o && o.params) || {}).src); };
			G._modalStack.length = 0; G._modalStack.push('lb');
			['ArrowLeft', 'ArrowRight'].forEach(function (key) {
				document.body.dispatchEvent(new KeyboardEvent('keydown', {key: key, bubbles: true, cancelable: true}));
			});
			return true;
		})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.__lbCalls.join(',')`, &calls),
	); err != nil {
		t.Fatal(err)
	}
	if calls != "3.jpg,2.jpg" {
		t.Fatalf("[ArrowLeft, ArrowRight] from 2.jpg in RTL opened %q, want 3.jpg then back to 2.jpg", calls)
	}
}
