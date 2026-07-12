package main

import (
	"testing"
	"time"

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
		waitModule(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.sortablelist)`),
		// Verify k1 starts in the "todo" column.
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerBefore),
		// Keyboard cross-column move: grab k1, ArrowRight, drop.
		chromedp.Evaluate(`(function(){
			var item = document.querySelector('[data-fui-sort-key="k1"]');
			item.focus();
			item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
			item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}));
			item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		})()`, nil),
		// Wait for the async commit POST to complete.
		chromedp.Sleep(500*time.Millisecond),
		// Verify k1 is now in the "doing" column.
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerAfter),
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

	// Reload the page — the server-side board store should reflect
	// the persisted move.
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerAfterReload),
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
