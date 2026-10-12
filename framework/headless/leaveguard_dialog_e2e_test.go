package headless

// A page that carries the kit's <template data-cui-confirm-dialog> asks
// the leave guard's question in that dialog, never in window.confirm.
// The hooks the guard answers are synchronous, so the move is declined
// at once and the dialog opens after; Cancel keeps the edits where they
// are, Discard cleans the forms the move drops and makes the move
// again. Every fixture stubs window.confirm to say yes, so a fallback
// to the native prompt would move on its own and fail the ask count.

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// leaveDialogTemplate is a hook-only dialog: what the kit renders,
// minus its classes.
const leaveDialogTemplate = `<template data-cui-confirm-dialog><dialog>
<h2 data-cui-confirm-part="title">Are you sure?</h2>
<p data-cui-confirm-part="message"></p>
<button type="button" data-cui-confirm-part="cancel">Cancel</button>
<button type="button" data-cui-confirm-part="accept">Confirm</button>
<button type="button" data-cui-confirm-part="accept-danger">Confirm</button>
</dialog></template>`

const leaveDialogState = `(() => {
	const d = document.querySelector('body > dialog');
	if (!d || !d.open) return 'closed';
	const p = (n) => d.querySelector('[data-cui-confirm-part="' + n + '"]');
	return [p('title').textContent, p('message').textContent,
		p('accept') ? 'plain' : p('accept-danger').textContent].join('|');
})()`

// leaveNavCount counts finished client navigations, so a test waits for
// the swap rather than a node both pages carry.
const leaveNavCount = `window.__navs = 0; window.addEventListener('gofastr:navigate', function () { window.__navs++; })`

const leaveGuardYes = `window.__confirms = []; window.confirm = function (m) { window.__confirms.push(String(m)); return true; };`

func leaveDialogClick(part string) chromedp.Action {
	return chromedp.Evaluate(`document.querySelector('body > dialog [data-cui-confirm-part="`+part+`"]').click()`, nil)
}

const leaveDialogForm = `<form id="gf" data-hui-leave-guard data-hui-leave-guard-message="Your edits will be lost."` +
	` data-hui-leave-guard-title="Discard changes?" data-hui-leave-guard-accept="Discard">` +
	`<input id="gf-name" name="name"></form>`

func TestLeaveGuardLinkAsksInKitDialog(t *testing.T) {
	b := startBehaviorServer(t, leaveGuardBody(leaveDialogForm)+leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	var state, path, dirty string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardYes, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]')`) {
		t.Fatal("a dirty link never opened the kit's dialog")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveDialogState, &state),
		chromedp.Evaluate(`location.pathname`, &path),
	); err != nil {
		t.Fatalf("read: %v", err)
	}
	if state != "Discard changes?|Your edits will be lost.|Discard" {
		t.Errorf("dialog = %q, want the form's title, message and danger accept", state)
	}
	if path != "/" {
		t.Errorf("the link moved while its dialog was open: at %q", path)
	}
	// Cancel: the page and the edit stay, still dirty.
	if err := chromedp.Run(ctx,
		leaveDialogClick("cancel"),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(leaveDialogState, &state),
		chromedp.Evaluate(`location.pathname + '|' + document.getElementById('gf-name').value + '|' + document.getElementById('gf').hasAttribute('data-hui-dirty')`, &dirty),
	); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if state != "closed" || dirty != "/|typed|true" {
		t.Errorf("Cancel must close the dialog and keep the dirty edit: dialog %q, page %q", state, dirty)
	}
	// Discard: the same link goes through.
	if err := chromedp.Run(ctx, chromedp.Click(`#go`, chromedp.ByID)); err != nil {
		t.Fatalf("again: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]')`) {
		t.Fatal("the second click never opened the dialog")
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, `location.pathname === '/away'`) {
		t.Fatal("Discard did not make the move")
	}
	var asks int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__confirms.length`, &asks)); err != nil {
		t.Fatal(err)
	}
	if asks != 0 {
		t.Errorf("the guard fell back to window.confirm %d times with the kit's dialog on the page", asks)
	}
}

func TestLeaveGuardPageBackDiscardReplays(t *testing.T) {
	b := startBehaviorServer(t, leaveGuardBody(leaveDialogForm)+leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveNavCount, nil),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatalf("go: %v", err)
	}
	if !pollTrue(ctx, `window.__navs > 0 && location.pathname === '/away'`) {
		t.Fatal("the clean page never navigated to /away")
	}
	var lenBefore, lenAfter int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardYes, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('gf-name').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
		chromedp.Evaluate(`history.length`, &lenBefore),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("back: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]') && location.pathname === '/away'`) {
		t.Fatal("Back from a dirty page never opened the dialog on the page it left")
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, `location.pathname === '/'`) {
		t.Fatal("Discard did not make the Back move again")
	}
	var asks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`history.length`, &lenAfter),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
	); err != nil {
		t.Fatal(err)
	}
	if lenAfter != lenBefore {
		t.Errorf("the replayed Back must not add entries (before %d, after %d)", lenBefore, lenAfter)
	}
	if asks != 0 {
		t.Errorf("the guard fell back to window.confirm %d times", asks)
	}
}

func TestLeaveGuardLayerCloseDiscardCloses(t *testing.T) {
	b := leaveGuardLayerServer(t, leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	layer := `location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`
	var at string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardYes, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Click(`#layer-close`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]')`) {
		t.Fatal("the close control never opened the dialog")
	}
	// A second close while the dialog is open is declined without a
	// second dialog.
	var dialogs int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('layer-close').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('body > dialog').length`, &dialogs),
		chromedp.Evaluate(layer, &at),
	); err != nil {
		t.Fatal(err)
	}
	if dialogs != 1 {
		t.Errorf("a close under the open dialog opened %d dialogs, want 1", dialogs)
	}
	if at != "/rec/1:open" {
		t.Errorf("the layer closed while its dialog was open: %q", at)
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, layer+` === '/:closed'`) {
		chromedp.Run(ctx, chromedp.Evaluate(layer, &at))
		t.Fatalf("Discard must close the layer back to /, at %q", at)
	}
}

func TestLeaveGuardLayerBackDiscardCloses(t *testing.T) {
	b := leaveGuardLayerServer(t, leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	layer := `location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`
	var at string
	var lenBefore, lenAfter int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardYes, nil),
		chromedp.Evaluate(`history.length`, &lenBefore),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]') && location.pathname === '/rec/1'`) {
		chromedp.Run(ctx, chromedp.Evaluate(layer, &at))
		t.Fatalf("Back from a dirty layer never opened the dialog over the layer (at %q)", at)
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, layer+` === '/:closed'`) {
		chromedp.Run(ctx, chromedp.Evaluate(layer, &at))
		t.Fatalf("Discard must make the Back move and close the layer, at %q", at)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.length`, &lenAfter)); err != nil {
		t.Fatal(err)
	}
	if lenAfter != lenBefore+1 {
		t.Errorf("history.length = %d, want %d: the layer's one entry, nothing pushed by the replay", lenAfter, lenBefore+1)
	}
}

// Forward onto a layer's own later entry refetches the layer, dropping
// its dirty form: the dialog opens over the entry it left, and Discard
// goes forward again.
func TestLeaveGuardLayerForwardDiscardReplays(t *testing.T) {
	b := leaveGuardStackServer(t, leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(leaveGuardYes, nil), chromedp.Click(`#open`, chromedp.ByID)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('gf-name')`) {
		t.Fatal("the record layer never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#tab`, chromedp.ByID)); err != nil {
		t.Fatalf("tab: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('rec-history')`) {
		t.Fatal("the history tab never rendered in the layer")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('gf-name')`) {
		t.Fatal("Back never brought the form back")
	}
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('gf-name').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
		chromedp.Evaluate(`history.forward()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]') && location.search === '' && !!document.getElementById('gf-name')`) {
		t.Fatal("Forward from a dirty layer never opened the dialog over the form")
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, `location.search === '?tab=history' && !!document.getElementById('rec-history')`) {
		var at string
		chromedp.Run(ctx, chromedp.Evaluate(`location.pathname + location.search`, &at))
		t.Fatalf("Discard must go forward again to the history tab, at %q", at)
	}
}

// A save in a layer stacked over a dirty record returns to that record,
// re-rendering it and dropping its edits: the dialog asks first, and
// Discard makes the return.
func TestLeaveGuardSaveReturnDiscardReturns(t *testing.T) {
	body := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}},` +
		`{"path":"/new","intercept":{"from":"/rec/:id","as":"drawer"}}]</script>` +
		leaveDialogTemplate + `<a id="open" href="/rec/1">open</a>`
	b := startBehaviorServer(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true}`)
		})
		layer := func(html string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if r.Header.Get("X-Gofastr-Intercept") != "" {
					w.Header().Set("X-Gofastr-Overlay", "drawer")
				}
				fmt.Fprint(w, html)
			}
		}
		mux.HandleFunc("/rec/1", layer(`<div id="rec-layer">`+leaveDialogForm+`<a id="new" href="/new">new</a></div>`))
		mux.HandleFunc("/new", layer(`<div id="new-pane"><form id="new-form" data-cui-rpc="/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/rec/1">`+
			`<button id="new-save" type="submit">Create</button></form></div>`))
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(leaveGuardYes, nil), chromedp.Click(`#open`, chromedp.ByID)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('gf-name')`) {
		t.Fatal("the record layer never mounted")
	}
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('gf-name').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
		chromedp.Click(`#new`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('new-save')`) {
		t.Fatal("the create layer never stacked over the record")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#new-save`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]') && !!document.getElementById('new-pane')`) {
		t.Fatal("the save's return over a dirty record never opened the dialog")
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, `!document.getElementById('new-pane') && location.pathname === '/rec/1' && document.getElementById('gf-name') && document.getElementById('gf-name').value === ''`) {
		var at string
		chromedp.Run(ctx, chromedp.Evaluate(`location.pathname + ':' + !!document.getElementById('new-pane')`, &at))
		t.Fatalf("Discard must close the create layer and re-render the record, at %q", at)
	}
}

// Back onto an entry the guard did not number has no known distance:
// declined, the page is pushed again, and Discard navigates to the URL
// the Back was headed for.
func TestLeaveGuardUntaggedBackDiscardNavigates(t *testing.T) {
	b := startBehaviorServer(t, leaveGuardBody(leaveDialogForm)+leaveDialogTemplate)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveNavCount, nil),
		chromedp.Evaluate(`history.replaceState(null, '', location.href)`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatalf("go: %v", err)
	}
	if !pollTrue(ctx, `window.__navs > 0 && location.pathname === '/away'`) {
		t.Fatal("the clean page never navigated to /away")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardYes, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('gf-name').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("back: %v", err)
	}
	if !pollTrue(ctx, `!!document.querySelector('body > dialog[open]') && location.pathname === '/away'`) {
		t.Fatal("Back onto an untagged entry never opened the dialog on the page it left")
	}
	if err := chromedp.Run(ctx, leaveDialogClick("accept-danger")); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !pollTrue(ctx, `location.pathname === '/'`) {
		t.Fatal("Discard did not navigate to where the Back was headed")
	}
}
