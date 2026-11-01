package ui_test

// Browser coverage for framework/ui's own behaviour modules on the
// paths the kernel drives after boot: an arrival pass handed a node
// that IS the component root (an island swap or an append whose root
// is the wrapper), and the lightbox's arrow keys while a newer modal
// sits above it. Same harness as headless_modules_e2e_test.go: the real
// runtime, every registered module, the behaviours block.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A SearchInput appended after the module loaded arrives as its own
// wrapper: the kernel hands the scanner that node, and a
// descendants-only lookup left the clear button and Escape dead.
func TestE2E_SearchInputWiresAsSwapRoot(t *testing.T) {
	first := ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "first", Placeholder: "Filter"})
	second := ui.SearchInput(ui.SearchInputConfig{Name: "q2", ID: "second", Placeholder: "Filter"})
	ctx := moduleTestCtx(t, string(first)+`<template id="tpl">`+string(second)+`</template><div id="slot"></div>`)
	if !pollJS(ctx, moduleLoaded("searchinput")) {
		t.Fatal("the SearchInput marker never loaded the searchinput module")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var w = document.getElementById('tpl').content.firstElementChild.cloneNode(true);
		document.getElementById('slot').appendChild(w);
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('second').__searchWired === true`) {
		t.Fatal("a SearchInput whose wrapper was the inserted node was never wired")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var i = document.getElementById('second');
		i.value = 'abc';
		i.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true, cancelable:true}));
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('second').value === ''`) {
		t.Fatal("Escape did not clear the swapped-in SearchInput")
	}
}

// A preview input that arrives as the inserted node itself, already
// holding an image, renders its thumbnail.
func TestE2E_DropzonePreviewsAsSwapRoot(t *testing.T) {
	zone := ui.FileDropzone(ui.FileDropzoneConfig{Name: "a", Label: "Images", ShowPreview: true})
	ctx := moduleTestCtx(t, string(zone)+`<div data-fui-dropzone-preview-for="late"></div><div id="slot"></div>`)
	if !pollJS(ctx, moduleLoaded("filedropzone")) {
		t.Fatal("the preview marker never loaded the filedropzone module")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var i = document.createElement('input');
		i.type = 'file'; i.id = 'late'; i.setAttribute('data-fui-dropzone-preview', '');
		var dt = new DataTransfer();
		dt.items.add(new File([new Uint8Array([137,80,78,71])], 'a.png', {type:'image/png'}));
		i.files = dt.files;
		document.getElementById('slot').appendChild(i);
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelectorAll('[data-fui-dropzone-preview-for="late"] img').length === 1`) {
		t.Fatal("a preview input that was the inserted node rendered no thumbnail")
	}
}

// The arrow keys belong to the top modal: with a newer modal (a
// command palette, a confirm) above an open lightbox, ArrowRight in
// that modal's field neither steps the gallery beneath nor is
// swallowed. With the lightbox on top again, it steps.
func TestE2E_LightboxArrowsDeferToTopModal(t *testing.T) {
	ctx := moduleTestCtx(t, `<div data-cui-widget="lb">
  <div data-cui-comp="ui-lightbox" data-fui-lightbox="lb" data-fui-lightbox-nav="true">
    <button data-fui-lightbox-prev>prev</button><button data-fui-lightbox-next>next</button>
  </div>
</div>
<div data-cui-widget="palette"><input id="search" aria-label="Search"></div>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=1.jpg&group=g">1</a>
<a data-fui-lightbox-group="g" data-cui-deeplink="src=2.jpg&group=g">2</a>`)
	if !pollJS(ctx, `!!(window.__gofastr && window.__gofastr.lightbox && window.__gofastr.lightbox.rescan)`) {
		t.Fatal("the lightbox marker never loaded the lightbox module")
	}
	// The widgets module installs openWidget when it lands; spy after.
	if !pollJS(ctx, moduleLoaded("widgets")) {
		t.Fatal("the widgets module never loaded")
	}
	var prevented bool
	var calls string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var G = window.__gofastr;
			G._signals = G._signals || {};
			G._signals.group = {value: 'g'}; G._signals.src = {value: '1.jpg'};
			window.__lbCalls = [];
			G.openWidget = function(name, o){ window.__lbCalls.push(name + ':' + ((o && o.params) || {}).src); };
			G._modalStack.length = 0; G._modalStack.push('lb', 'palette');
			var s = document.getElementById('search'); s.focus();
			var e = new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true});
			s.dispatchEvent(e);
			return e.defaultPrevented;
		})()`, &prevented),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.__lbCalls.join(',')`, &calls),
	); err != nil {
		t.Fatal(err)
	}
	if calls != "" || prevented {
		t.Fatalf("ArrowRight under a newer modal drove the lightbox: calls %q, prevented %v", calls, prevented)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var G = window.__gofastr;
			G._modalStack.length = 0; G._modalStack.push('lb');
			document.body.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}));
		})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.__lbCalls.join(',')`, &calls),
	); err != nil {
		t.Fatal(err)
	}
	if calls != "lb:2.jpg" {
		t.Fatalf("ArrowRight with the lightbox on top: calls %q, want lb:2.jpg", calls)
	}
}
