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

// Prev and Next reuse the open modal's <img> with a new src, so the
// modal never closes and the close-time zoom reset never runs: a
// zoomed or panned image must still reset when the lightbox steps,
// or the next picture opens already magnified and off-centre.
func TestE2E_LightboxStepResetsZoom(t *testing.T) {
	ctx := moduleTestCtx(t, `<div data-cui-widget="lb">
  <div data-cui-comp="ui-lightbox" data-fui-lightbox="lb" data-fui-lightbox-nav="true">
    <img data-fui-lightbox-image src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" alt="" width="200" height="200">
    <button data-fui-lightbox-prev>prev</button><button data-fui-lightbox-next>next</button>
  </div>
</div>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=1.jpg&group=g">1</a>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=2.jpg&group=g">2</a>`)
	if !pollJS(ctx, `!!(window.__gofastr && window.__gofastr.lightbox && window.__gofastr.lightbox.rescan)`) {
		t.Fatal("the lightbox marker never loaded the lightbox module")
	}
	var zoomed, after string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var G = window.__gofastr;
			G._signals = G._signals || {};
			G._signals.group = {value: 'g'}; G._signals.src = {value: '1.jpg'};
			G.openWidget = function(){};
			var img = document.querySelector('[data-fui-lightbox-image]');
			// A double tap zooms to 2x.
			for (var i = 0; i < 2; i++) {
				img.dispatchEvent(new PointerEvent('pointerdown', {pointerId: 1, clientX: 50, clientY: 50, bubbles: true}));
				img.dispatchEvent(new PointerEvent('pointerup', {pointerId: 1, clientX: 50, clientY: 50, bubbles: true}));
			}
			return (img.hasAttribute('data-fui-zoomed') ? 'zoomed' : 'flat') + '|' + img.style.transform;
		})()`, &zoomed),
		chromedp.Evaluate(`document.querySelector('[data-fui-lightbox-next]').click()`, nil),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var img = document.querySelector('[data-fui-lightbox-image]');
			return (img.hasAttribute('data-fui-zoomed') ? 'zoomed' : 'flat') + '|' + img.style.transform;
		})()`, &after),
	); err != nil {
		t.Fatal(err)
	}
	if zoomed == "" || zoomed[:6] != "zoomed" {
		t.Fatalf("the double tap did not zoom the image: %q", zoomed)
	}
	if after != "flat|" {
		t.Fatalf("Next kept the previous image's zoom: %q", after)
	}
}
