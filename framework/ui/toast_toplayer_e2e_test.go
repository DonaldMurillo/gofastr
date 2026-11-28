package ui_test

// The toast stack on the top layer (#706): a stack mounted as a widget is
// a manual popover while it holds a toast, so it shows above anything
// already in the top layer and keeps its mount position; a toast raised
// while a modal is open is held until the modal closes, as content behind
// a modal is inert and its announcement would be lost.

import (
	"context"
	"math"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

// toastPage serves the kit's sheets, the toast stack widget named
// "toasts" as uihost mounts it, a button, a modal dialog and a manual
// popover over the stack's corner.
func toastPage(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	def := preset.ToastStack("toasts").Definition()
	head := kitHead(t, true) + "<style>" + widget.RenderCSS(def) + "</style>"
	body := `<button id="elsewhere" type="button">Elsewhere</button>` +
		`<dialog id="dlg"><p>A dialog</p><button id="close" type="button" onclick="this.closest('dialog').close()">Close</button></dialog>` +
		`<div id="cover" popover="manual" style="position: fixed; inset: auto 0 0 auto; margin: 0; width: 600px; height: 400px; background: #ccc">Cover</div>` +
		widget.RenderChrome(def)
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, emulateFocus(), chromedp.EmulateViewport(900, 600))
	if !pollJS(ctx, moduleLoaded("headless-feedback")) {
		t.Fatal("headless-feedback did not load")
	}
	return ctx
}

const stackHost = `document.querySelector('[data-cui-widget="toasts"]')`

func rowCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	evalJS(t, ctx, `document.querySelectorAll('[data-cui-toast-stack="toasts"] [data-hui-toast-id]').length`, &n)
	return n
}

// A toast shows above a popover that was already open over its corner,
// with the stack still at the bottom right.
func TestToastStackRisesIntoTheTopLayer(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `document.getElementById('cover').showPopover()`, nil)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("the stack's widget is not an open popover after a toast")
	}
	var hit string
	evalJS(t, ctx, `(() => { const r = document.querySelector('[data-cui-toast-stack="toasts"] [data-hui-toast-id]').getBoundingClientRect(); const el = document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2); return el && el.closest('[data-hui-toast-id]') ? 'toast' : (el ? el.id || el.tagName : ''); })()`, &hit)
	if hit != "toast" {
		t.Fatalf("the point at the toast's centre hits %q: the toast is under the popover", hit)
	}
	var edge struct{ Left, Top, Right, Bottom, VW, VH float64 }
	evalJS(t, ctx, `(() => { const r = `+stackHost+`.getBoundingClientRect(); return {Left: r.left, Top: r.top, Right: r.right, Bottom: r.bottom, VW: document.documentElement.clientWidth, VH: document.documentElement.clientHeight}; })()`, &edge)
	if math.Abs(edge.VW-edge.Right-20) > 2 || math.Abs(edge.VH-edge.Bottom-20) > 2 || edge.Left < edge.VW/2 || edge.Top < edge.VH/2 {
		t.Fatalf("the raised stack spans (%v, %v)–(%v, %v) in a %v×%v viewport, want it in the bottom-right corner, 20px in", edge.Left, edge.Top, edge.Right, edge.Bottom, edge.VW, edge.VH)
	}
}

// The stack leaves the top layer once its last toast goes.
func TestToastStackLeavesTheTopLayerWhenEmpty(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `window.__toastID = window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("the stack did not rise for a toast")
	}
	evalJS(t, ctx, `(() => { const row = document.querySelector('[data-hui-toast-id="' + window.__toastID + '"]'); window.__gofastr._dismissToast(row, window.__toastID); })()`, nil)
	if !pollJS(ctx, `!document.querySelector('[data-hui-toast-id]') && !`+stackHost+`.matches(':popover-open')`) {
		t.Fatal("the stack stayed in the top layer after its last toast left")
	}
}

// A toast raised while a modal dialog is open is held, and shows once
// the dialog closes.
func TestToastHeldWhileAModalIsOpen(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `document.getElementById('dlg').showModal()`, nil)
	var id string
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, &id)
	if id == "" {
		t.Fatal("toast() returned no id for a held toast")
	}
	settle()
	if n := rowCount(t, ctx); n != 0 {
		t.Fatalf("%d toast rows were added behind an open modal, want them held", n)
	}
	evalJS(t, ctx, `document.getElementById('dlg').close()`, nil)
	if !pollJS(ctx, `!!document.querySelector('[data-hui-toast-id="`+id+`"]') && `+stackHost+`.matches(':popover-open')`) {
		t.Fatal("the held toast did not show once the modal closed")
	}
}

// A stack made inert (a focus trap that inerts the page's other
// children) holds its toasts the same way until it is live again.
func TestToastHeldWhileTheStackIsInert(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, stackHost+`.setAttribute('inert', '')`, nil)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	settle()
	if n := rowCount(t, ctx); n != 0 {
		t.Fatalf("%d toast rows were added to an inert stack, want them held", n)
	}
	evalJS(t, ctx, stackHost+`.removeAttribute('inert')`, nil)
	if !pollJS(ctx, `document.querySelectorAll('[data-cui-toast-stack="toasts"] [data-hui-toast-id]').length === 1`) {
		t.Fatal("the held toast did not show once the stack was live again")
	}
}

// A widget modal that is not a <dialog> (a custom skeleton over a
// backdrop) holds toasts too: widgets.js lists it on _modalStack while it
// is open and dispatches fui:widget-close when it closes.
func TestToastHeldWhileAWidgetModalIsOpen(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `(window.__gofastr._modalStack = window.__gofastr._modalStack || []).push('custom')`, nil)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	settle()
	if n := rowCount(t, ctx); n != 0 {
		t.Fatalf("%d toast rows were added behind an open widget modal, want them held", n)
	}
	evalJS(t, ctx, `window.__gofastr._modalStack.pop(); document.dispatchEvent(new CustomEvent('fui:widget-close', {detail: {name: 'custom'}}))`, nil)
	if !pollJS(ctx, `document.querySelectorAll('[data-cui-toast-stack="toasts"] [data-hui-toast-id]').length === 1`) {
		t.Fatal("the held toast did not show once the widget modal closed")
	}
}

// A toast raised after a popover opened over an already-raised stack
// shows above that popover, and focus on the first toast stays put.
func TestToastRisesAgainOverALaterPopover(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'First', ttl: 0})`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("the stack did not rise for the first toast")
	}
	evalJS(t, ctx, `document.querySelector('[data-hui-toast-dismiss]').focus(); document.getElementById('cover').showPopover()`, nil)
	var second string
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Second', ttl: 0})`, &second)
	settle()
	var hit string
	evalJS(t, ctx, `(() => { const r = document.querySelector('[data-hui-toast-id="`+second+`"]').getBoundingClientRect(); const el = document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2); return el && el.closest('[data-hui-toast-id]') ? 'toast' : (el ? el.id || el.tagName : ''); })()`, &hit)
	if hit != "toast" {
		t.Fatalf("the second toast's centre hits %q: it is under the popover opened after the stack rose", hit)
	}
	var focused bool
	evalJS(t, ctx, `document.activeElement === document.querySelector('[data-hui-toast-dismiss]')`, &focused)
	if !focused {
		t.Fatal("raising the stack again moved focus off the first toast's dismiss button")
	}
	var toggles int
	evalJS(t, ctx, `(() => { window.__toggles = 0; `+stackHost+`.addEventListener('toggle', () => window.__toggles++); window.__gofastr._initToasts(document.body); })()`, nil)
	settle()
	evalJS(t, ctx, `window.__toggles`, &toggles)
	if toggles != 0 {
		t.Fatalf("a sync that added no row toggled the stack's widget %d times, want none", toggles)
	}
}

// A row a signal swap puts inside a closed stack opens the stack's
// widget, as a toast() row does.
func TestToastRowSwappedIntoAStackOpensIt(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `document.querySelector('[data-cui-toast-stack="toasts"]').insertAdjacentHTML('beforeend', '<div id="slot"></div>')`, nil)
	settle()
	var open bool
	evalJS(t, ctx, stackHost+`.matches(':popover-open')`, &open)
	if open {
		t.Fatal("the stack rose with no row in it")
	}
	evalJS(t, ctx, `document.getElementById('slot').innerHTML = '<div data-hui-toast role="status">Swapped</div>'`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("a row swapped into the stack left its widget closed")
	}
}

// Dismissing a row a signal swap put in a slot inside the stack keeps
// the widget up while another row remains.
func TestToastDismissInASlotKeepsTheStackUp(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Kept', ttl: 0})`, nil)
	evalJS(t, ctx, `document.querySelector('[data-cui-toast-stack="toasts"]').insertAdjacentHTML('beforeend', '<div id="slot"><div data-hui-toast data-hui-toast-id="swapped" role="status">Swapped</div></div>')`, nil)
	if !pollJS(ctx, `!!document.querySelector('#slot [data-hui-toast-id]') && `+stackHost+`.matches(':popover-open')`) {
		t.Fatal("the swapped row was not armed in a raised stack")
	}
	evalJS(t, ctx, `(() => { const row = document.querySelector('#slot [data-hui-toast-id]'); window.__gofastr._dismissToast(row, row.getAttribute('data-hui-toast-id')); })()`, nil)
	if !pollJS(ctx, `!document.querySelector('#slot [data-hui-toast-id]')`) {
		t.Fatal("the swapped row was not dismissed")
	}
	settle()
	var open bool
	evalJS(t, ctx, stackHost+`.matches(':popover-open')`, &open)
	if !open {
		t.Fatal("dismissing the row in the slot closed the stack while another row remains")
	}
}

// A fullscreen element matches :modal but leaves the page around it
// live, and leaving fullscreen fires nothing the hold watches: a toast
// raised over it shows at once, above it.
func TestToastShowsOverAFullscreenElement(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `(() => { const b = document.createElement('button'); b.id = 'fs'; b.textContent = 'Fullscreen'; b.addEventListener('click', () => document.getElementById('cover').requestFullscreen()); document.body.prepend(b); })()`, nil)
	if err := chromedp.Run(ctx, chromedp.Click(`#fs`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.fullscreenElement === document.getElementById('cover')`) {
		t.Skip("this browser did not enter fullscreen")
	}
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, `document.querySelectorAll('[data-cui-toast-stack="toasts"] [data-hui-toast-id]').length === 1 && `+stackHost+`.matches(':popover-open')`) {
		t.Fatal("a toast raised over a fullscreen element was held")
	}
}

// A stack the browser refuses to raise (showPopover throws) drops the
// popover attribute, so its toast still draws instead of being hidden by
// the browser's rule for a [popover] out of the top layer.
func TestToastStackTheBrowserWillNotRaiseStillDraws(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `HTMLElement.prototype.showPopover = function () { throw new DOMException('refused', 'InvalidStateError'); }`, nil)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, `!!document.querySelector('[data-cui-toast-stack="toasts"] [data-hui-toast-id]')`) {
		t.Fatal("the toast was not added")
	}
	var shown string
	evalJS(t, ctx, `(() => { const h = `+stackHost+`; return h.hasAttribute('popover') ? 'kept the popover attribute' : getComputedStyle(h).display === 'none' ? 'not drawn' : 'drawn'; })()`, &shown)
	if shown != "drawn" {
		t.Fatalf("a stack whose showPopover threw is %s, want it drawn in place", shown)
	}
}

// A raised stack a move takes out of the top layer (a swap's re-attach, a
// closing dialog handing it back to <body>) rises again when the arrival
// scan sees it.
func TestMovedToastStackRisesAgain(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("the stack did not rise for a toast")
	}
	evalJS(t, ctx, `document.body.append(`+stackHost+`)`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("the moved stack stayed out of the top layer, hidden")
	}
}

// A toast shown before the widget runtime has loaded (a toast the server
// rendered, or one raised during the first paint) still raises its stack:
// headless-feedback does not wait on widgets.js for the step.
func TestToastStackRisesBeforeTheWidgetRuntimeLoads(t *testing.T) {
	ctx := toastPage(t)
	evalJS(t, ctx, `for (const k of ['mountWidget', 'openWidget', 'closeWidget', '_widgets']) delete window.__gofastr[k]`, nil)
	evalJS(t, ctx, `window.__gofastr.toast({title: 'Saved', ttl: 0})`, nil)
	if !pollJS(ctx, stackHost+`.matches(':popover-open')`) {
		t.Fatal("a toast shown before the widget runtime loaded left its stack out of the top layer")
	}
}
