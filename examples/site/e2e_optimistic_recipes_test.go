package main

// Browser-level (chromedp) e2e for the four optimistic-UI recipe demos
// added under /components/optimistic-* (issue #104). Each test exercises
// one recipe end-to-end through a real headless Chrome: optimistic apply,
// success reconcile, failure rollback, and (for create) authoritative
// replacement. The consoleErrSink guard catches the strict-CSP failure
// mode where a demo used inline style="…" the browser silently strips.
//
// Gated by -short (the suite is slow and needs headless Chrome), matching
// the rest of examples/site's e2e convention.

import (
	"strings"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// TestE2E_Optimistic_InlineEdit_SuccessAndRollback covers Recipe 2:
// the Save button commits on 2xx; the Save (reject) button flips
// optimistically, then shakes and reverts when the 4xx lands. Both
// reconciliation paths in one test.
//
// The reject half INTENTIONALLY triggers a 422 from the demo handler —
// that 4xx is what flips the button into its error/shake state and back
// to idle. Chrome logs the failed network resource as a console entry;
// the test tolerates ONLY entries for the demo's reject endpoint
// (sink.errorsExcludingExpectedReject) AND asserts the 422 actually
// fired (sink.rejectSeen) — an unrelated 404/500 still fails the test.
func TestE2E_Optimistic_InlineEdit_SuccessAndRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	// Selectors: the demo renders two OptimisticAction buttons. The
	// first hits the ok endpoint, the second hits the fail endpoint.
	const okBtn = `document.querySelector('[data-fui-optimistic-endpoint="/__site/optimistic/edit/ok"]')`
	const failBtn = `document.querySelector('[data-fui-optimistic-endpoint="/__site/optimistic/edit/fail"]')`

	var okInitial, okAfterCommit string
	var failAfterClick, failAfterRollback string
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticinlineedit"),
		pageReady(),
		// Wait for the demand-loaded optimisticaction runtime module.
		waitModule(`!!(window.__gofastr && window.__gofastr.optimisticaction)`),
		chromedp.Evaluate(okBtn+`.getAttribute('data-state')`, &okInitial),
		// Click Save → flips to pending → committed (2xx).
		chromedp.Evaluate(okBtn+`.click()`, nil),
		settle(),
		chromedp.Evaluate(okBtn+`.getAttribute('data-state')`, &okAfterCommit),
		// Click Save (reject) → flips to pending → error (4xx) → idle.
		chromedp.Evaluate(failBtn+`.click()`, nil),
		settle(),
		chromedp.Evaluate(failBtn+`.getAttribute('data-state')`, &failAfterClick),
		// The error path waits ~600ms then reverts to idle.
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(failBtn+`.getAttribute('data-state')`, &failAfterRollback),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if okInitial != "idle" {
		t.Errorf("ok button initial state = %q, want idle", okInitial)
	}
	if okAfterCommit != "committed" {
		t.Errorf("ok button state after click = %q, want committed", okAfterCommit)
	}
	// After the 4xx the button MUST be in the deterministic `error`
	// (shake) state — the runtime holds it ~600ms before reverting to
	// idle. The reject endpoint fails immediately, so by `settle()` the
	// 422 has landed and the button has flipped to error. Accepting
	// `idle` here would let a no-op click (the RPC never fired, the
	// button never left idle) pass the test.
	if failAfterClick != "error" {
		t.Errorf("fail button state after 4xx = %q, want \"error\" (deterministic shake state)", failAfterClick)
	}
	if failAfterRollback != "idle" {
		t.Errorf("fail button state after rollback timer = %q, want idle", failAfterRollback)
	}

	// Positive assertion: the demo's 422 from the reject endpoint MUST
	// have actually fired. Defends against a silently-passing test
	// where the RPC was unreachable or returned 2xx by mistake.
	if !sink.rejectSeen("/__site/optimistic/edit/fail") {
		t.Error("expected a network-error entry for /__site/optimistic/edit/fail (the demo's 422 reject); none was seen — the reject path may not have fired")
	}
	// Every OTHER console/CSP/JS error stays fatal. Only entries for
	// the demo's reject endpoint are tolerated.
	if errs := sink.errorsExcludingExpectedReject("/__site/optimistic/edit/fail"); len(errs) > 0 {
		t.Errorf("inline edit produced %d non-reject console/CSP/network error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
}

// TestE2E_Optimistic_Create_AppendsAndPersists covers Recipe 3: clicking
// Add fires the create RPC, the response replaces the list region with
// the authoritative HTML, and the new row appears with a real server-
// assigned id. A fresh page load reflects the persisted append.
func TestE2E_Optimistic_Create_AppendsAndPersists(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	resetOptimisticNotes()
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	const listItemCount = `document.querySelectorAll('[data-fui-signal="opt-create-list"] [data-opt-id]').length`
	// lastID reads the server-assigned id from the NEWEST row (the
	// last <li> in the list region). Create is an APPEND, so the FIRST
	// row's id is unaffected by the click; the meaningful signal is
	// that the newest row carries a fresh server id (n4, n5, …) that
	// did not exist before the RPC resolved.
	const lastID = `(()=>{const rows=document.querySelectorAll('[data-fui-signal="opt-create-list"] [data-opt-id]');const el=rows[rows.length-1];return el?el.getAttribute('data-opt-id')||'':'';})()`

	var before, after, afterReload int
	var lastBefore, lastAfter string
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticcreate"),
		pageReady(),
		chromedp.Evaluate(listItemCount, &before),
		chromedp.Evaluate(lastID, &lastBefore),
		// Click Add. interactive.OnClick fires the POST and swaps the
		// list region's innerHTML with the response on 2xx.
		chromedp.Click(`button[data-fui-rpc="/__site/optimistic/create"]`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond), // wait for RPC + swap
		chromedp.Evaluate(listItemCount, &after),
		chromedp.Evaluate(lastID, &lastAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if before == 0 {
		t.Fatal("expected non-empty initial list; resetOptimisticNotes may have failed")
	}
	if after != before+1 {
		t.Errorf("list length after Add = %d, want %d (one appended)", after, before+1)
	}
	// The new row is an APPEND, so the newest id must change: the
	// server assigns n4, n5, … and the swapped list reflects it.
	if lastAfter == "" {
		t.Errorf("newest row has no data-opt-id after Add; the server id did not reconcile")
	}
	if lastBefore == lastAfter {
		t.Errorf("newest row id unchanged after append — got %q before and after (expected a fresh n4/n5/… id)", lastAfter)
	}

	// Reload — the persisted append should be visible on a fresh SSR.
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/optimisticcreate"),
		pageReady(),
		chromedp.Evaluate(listItemCount, &afterReload),
	)
	if err != nil {
		t.Fatalf("chromedp reload: %v", err)
	}
	if afterReload != after {
		t.Errorf("list length after reload = %d, want %d (persisted append)", afterReload, after)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("optimistic create produced %d console/CSP error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
	resetOptimisticNotes()
}

// TestE2E_Optimistic_Delete_RemovesOnConfirm covers Recipe 4: clicking a
// row's Delete trigger opens the ConfirmAction modal; clicking Confirm
// fires the delete RPC and the list region swaps to the shorter
// authoritative list.
func TestE2E_Optimistic_Delete_RemovesOnConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	resetOptimisticNotes()
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	const listItemCount = `document.querySelectorAll('[data-fui-signal="opt-delete-list"] [data-opt-id]').length`
	const n1Present = `!!document.querySelector('[data-fui-signal="opt-delete-list"] [data-opt-id="n1"]')`
	const trigger = `document.querySelector('button[data-fui-open="opt-delete-n1"]')`

	var before, after int
	var n1Before, n1After bool
	var dialogVisible bool
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticdelete"),
		pageReady(),
		chromedp.Evaluate(listItemCount, &before),
		chromedp.Evaluate(n1Present, &n1Before),
		// Click the row's Delete trigger — opens the ConfirmAction modal.
		chromedp.Evaluate(trigger+`.click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="opt-delete-n1"]')`, &dialogVisible),
		// Confirm is the danger button inside the modal's actions row.
		chromedp.Click(`[data-fui-widget="opt-delete-n1"] .ui-confirm-action__actions button.ui-button--danger`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond), // wait for RPC + swap
		chromedp.Evaluate(listItemCount, &after),
		chromedp.Evaluate(n1Present, &n1After),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !n1Before {
		t.Fatal("expected n1 to be present before delete")
	}
	if !dialogVisible {
		t.Fatal("ConfirmAction modal did not open after clicking Delete trigger")
	}
	if after != before-1 {
		t.Errorf("list length after delete = %d, want %d (one removed)", after, before-1)
	}
	if n1After {
		t.Errorf("n1 still present after delete confirm — list did not reconcile")
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("optimistic delete produced %d console/CSP error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
	resetOptimisticNotes()
}

// TestE2E_Optimistic_Slow_PendingThenCommit covers the slow half of
// Recipe 7: the Save (slow) button enters pending (aria-busy + disabled)
// while the 500ms RPC is in flight, then commits when the 2xx lands.
func TestE2E_Optimistic_Slow_PendingThenCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	const slowBtn = `document.querySelector('[data-fui-optimistic-endpoint="/__site/optimistic/slow"]')`

	var pendingState, pendingBusy string
	var pendingDisabled bool
	var committedState string
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticslow"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.optimisticaction)`),
		// Click Save (slow). The endpoint sleeps 500ms before 2xx, so a
		// 400ms sample lands inside the pending window.
		chromedp.Evaluate(slowBtn+`.click()`, nil),
		chromedp.Sleep(400*time.Millisecond), // sample during pending
		chromedp.Evaluate(slowBtn+`.getAttribute('data-state')`, &pendingState),
		chromedp.Evaluate(slowBtn+`.getAttribute('aria-busy')`, &pendingBusy),
		chromedp.Evaluate(slowBtn+`.disabled`, &pendingDisabled),
		// Wait out the endpoint (500ms) + settlement.
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(slowBtn+`.getAttribute('data-state')`, &committedState),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if pendingState != "pending" {
		t.Errorf("state during slow RPC = %q, want pending", pendingState)
	}
	if pendingBusy != "true" {
		t.Errorf("aria-busy during pending = %q, want \"true\"", pendingBusy)
	}
	if !pendingDisabled {
		t.Errorf("disabled during pending = false, want true")
	}
	if committedState != "committed" {
		t.Errorf("state after slow RPC resolves = %q, want committed", committedState)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("slow optimistic produced %d console/CSP error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
}

// TestE2E_Optimistic_Fail_RollsBack covers the failure half of Recipe 7:
// the Save (will fail) button flips optimistically, then shakes and
// reverts to idle when the 4xx lands.
//
// Same scoped reject filter as the inline-edit test: the demo's 422 is
// the trigger for the rollback, so Chrome's "Failed to load resource"
// log entry for that 4xx is expected and tolerated (only for the
// demo's reject endpoint). The 422 MUST have fired (rejectSeen), and
// any other console/CSP/network error still fails the test.
func TestE2E_Optimistic_Fail_RollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	const failBtn = `document.querySelector('[data-fui-optimistic-endpoint="/__site/optimistic/fail"]')`

	var afterClick, afterRollback string
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticslow"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.optimisticaction)`),
		chromedp.Evaluate(failBtn+`.click()`, nil),
		settle(),
		chromedp.Evaluate(failBtn+`.getAttribute('data-state')`, &afterClick),
		// The error path waits ~600ms then reverts to idle.
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(failBtn+`.getAttribute('data-state')`, &afterRollback),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	// The reject endpoint fails immediately and the runtime holds the
	// `error` shake state ~600ms, so by `settle()` the button MUST be
	// deterministically in `error`. Accepting `idle` here would let a
	// no-op click (the RPC never fired) pass the test.
	if afterClick != "error" {
		t.Errorf("state after 4xx = %q, want \"error\" (deterministic shake state before the rollback timer)", afterClick)
	}
	if afterRollback != "idle" {
		t.Errorf("state after rollback timer = %q, want idle", afterRollback)
	}

	if !sink.rejectSeen("/__site/optimistic/fail") {
		t.Error("expected a network-error entry for /__site/optimistic/fail (the demo's 422 reject); none was seen — the reject path may not have fired")
	}
	if errs := sink.errorsExcludingExpectedReject("/__site/optimistic/fail"); len(errs) > 0 {
		t.Errorf("fail optimistic produced %d non-reject console/CSP/network error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
}

// TestE2E_Optimistic_Delete_Fail_LeavesListUnchanged covers the failure
// half of Recipe 4: confirming a delete whose RPC returns 422 MUST
// leave the bound list region byte-identical to its pre-click state
// and the row MUST still be present. This pins the optimistic-UI
// invariant the runtime change in setSignal guarantees (html-mode +
// non-string value = no DOM write) and defends against regressions
// that would corrupt the list with the auto-built error object.
func TestE2E_Optimistic_Delete_Fail_LeavesListUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	resetOptimisticNotes()
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	const listHTML = `document.querySelector('[data-fui-signal="opt-delete-list"]').innerHTML`
	const listItemCount = `document.querySelectorAll('[data-fui-signal="opt-delete-list"] [data-opt-id]').length`
	const n1Present = `!!document.querySelector('[data-fui-signal="opt-delete-list"] [data-opt-id="n1"]')`
	const trigger = `document.querySelector('button[data-fui-open="opt-delete-fail-n1"]')`

	var beforeHTML, afterHTML string
	var beforeCount, afterCount int
	var n1Before, n1After bool
	var dialogVisible bool
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/components/optimisticdelete"),
		pageReady(),
		chromedp.Evaluate(listHTML, &beforeHTML),
		chromedp.Evaluate(listItemCount, &beforeCount),
		chromedp.Evaluate(n1Present, &n1Before),
		// Open the dedicated "will fail" ConfirmAction modal.
		chromedp.Evaluate(trigger+`.click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="opt-delete-fail-n1"]')`, &dialogVisible),
		// Confirm is the danger button inside the modal's actions row.
		chromedp.Click(`[data-fui-widget="opt-delete-fail-n1"] .ui-confirm-action__actions button.ui-button--danger`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond), // wait for RPC + (no) swap
		chromedp.Evaluate(listHTML, &afterHTML),
		chromedp.Evaluate(listItemCount, &afterCount),
		chromedp.Evaluate(n1Present, &n1After),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !n1Before {
		t.Fatal("expected n1 to be present before delete")
	}
	if !dialogVisible {
		t.Fatal("ConfirmAction \"will fail\" modal did not open after clicking its trigger")
	}
	// The pinning assertion: the list region MUST be byte-identical.
	// A regression that re-introduces writing the error object into
	// the html-mode region would replace this with a JSON blob.
	if beforeHTML != afterHTML {
		t.Errorf("list innerHTML changed after a failed delete — must be byte-identical\nbefore: %s\n--after: %s", beforeHTML, afterHTML)
	}
	if beforeCount != afterCount {
		t.Errorf("list item count changed after a failed delete: before=%d after=%d (want unchanged)", beforeCount, afterCount)
	}
	if !n1After {
		t.Error("n1 disappeared from the list after a failed delete — the row MUST remain")
	}

	if !sink.rejectSeen("/__site/optimistic/delete/fail") {
		t.Error("expected a network-error entry for /__site/optimistic/delete/fail (the demo's 422); none was seen — the reject path may not have fired")
	}
	if errs := sink.errorsExcludingExpectedReject("/__site/optimistic/delete/fail"); len(errs) > 0 {
		t.Errorf("failed delete produced %d non-reject console/CSP/network error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
	resetOptimisticNotes()
}
