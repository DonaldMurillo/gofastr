package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2E_SortableKanbanDragPersist verifies the live kanban demo on
// /components/sortablelist: a keyboard cross-column move (Space →
// ArrowRight → Space) moves a card to the adjacent column, the RPC
// persists it, and a fresh page load reflects the new position.
func TestE2E_SortableKanbanDragPersist(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	resetKanbanBoard()
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var containerBefore, containerAfter, containerAfterReload string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		// Wait for the demand-loaded sortablelist module.
		waitModule(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-sortablelist'])`),
		// Verify k1 starts in the "todo" column.
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="k1"]').closest('[data-hui-sortable-container]').getAttribute('data-hui-sortable-container')`, &containerBefore),
		// Keyboard cross-column move: grab k1, ArrowRight, drop.
		chromedp.Evaluate(`(function(){
			var item = document.querySelector('[data-hui-sort-key="k1"]');
			item.focus();
			item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
			item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}));
			item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		})()`, nil),
		// Wait for the async commit POST to complete: on a 2xx the
		// module announces the destination list's "saved" copy in its
		// live region (a failure announces "reverted" instead).
		waitFor(`(() => {
			const dest = document.querySelector('[data-hui-sort-key="k1"]').closest('[data-hui-sortable-container]');
			const saved = dest.getAttribute('data-hui-sortable-s-saved') || '';
			const live = document.getElementById('hui-sortable-live');
			return saved !== '' && !!live && live.textContent === saved.replace(/\{\{(label|list|position)\}\}/g, '');
		})()`),
		// Verify k1 is now in the "doing" column.
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="k1"]').closest('[data-hui-sortable-container]').getAttribute('data-hui-sortable-container')`, &containerAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if containerBefore != "todo" {
		t.Errorf("k1 should start in todo, got %q", containerBefore)
	}
	if containerAfter != "doing" {
		t.Errorf("k1 should be in doing after move, got %q", containerAfter)
	}

	// Reload the page, the server-side board store should reflect
	// the persisted move.
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="k1"]').closest('[data-hui-sortable-container]').getAttribute('data-hui-sortable-container')`, &containerAfterReload),
	)
	if err != nil {
		t.Fatalf("chromedp reload: %v", err)
	}
	if containerAfterReload != "doing" {
		t.Errorf("k1 should persist in doing after reload, got %q", containerAfterReload)
	}

	// Clean up: reset the board so other tests aren't affected.
	resetKanbanBoard()
}

// TestE2E_SortableKanbanThreeMoves drives three sequential keyboard
// moves on the flagship board, a reorder, a crossing and a reorder in
// the destination, each waited out before the next. Every 204 renews
// the board's token on every column, so each move saves and a reload
// shows all three.
func TestE2E_SortableKanbanThreeMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	resetKanbanBoard()
	t.Cleanup(resetKanbanBoard)
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	move := func(key, arrow string) chromedp.Action {
		return chromedp.Evaluate(`(function(){
			var item = document.querySelector('[data-hui-sort-key="`+key+`"]');
			item.focus();
			[' ', '`+arrow+`', ' '].forEach(function (key) {
				item.dispatchEvent(new KeyboardEvent('keydown', {key: key, bubbles: true, cancelable: true}));
			});
		})()`, nil)
	}
	settled := func(token string) chromedp.Action {
		return waitFor(`Array.from(document.querySelectorAll('[data-hui-sortable]')).every(function (list) {
			return !list.__huiN && list.getAttribute('data-hui-sortable-version') === '` + token + `';
		})`)
	}
	columns := `Array.from(document.querySelectorAll('[data-hui-sortable-container]')).map(function (list) {
		return list.getAttribute('data-hui-sortable-container') + ':' + Array.from(list.querySelectorAll('[data-hui-sort-key]')).map(function (row) { return row.getAttribute('data-hui-sort-key'); }).join(',');
	}).join(' ')`
	var after, reloaded string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-sortablelist'])`),
		move("k1", "ArrowDown"), settled("v2"),
		move("k1", "ArrowRight"), settled("v3"),
		move("k3", "ArrowDown"), settled("v4"),
		chromedp.Evaluate(columns, &after),
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		chromedp.Evaluate(columns, &reloaded),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if want := "todo:k2 doing:k1,k3 done:k4"; after != want || reloaded != want {
		t.Fatalf("after three moves the board shows %q and reloads as %q, want %q", after, reloaded, want)
	}
}
