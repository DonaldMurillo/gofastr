package headless

// Overlapping commits that fail roll back to the order the server last
// confirmed, never to an order that carries an earlier unsaved move.

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// overlapServer serves a three-row list whose first commit answers
// after a delay with firstStatus and whose later commits answer at
// once with laterStatus.
func overlapServer(t *testing.T, firstStatus, laterStatus int) *behaviorServer {
	t.Helper()
	var n atomic.Int32
	list := SortableList(SortableListProps{ID: "ol", Label: "Order", RPCPath: "/__hui/order", Items: []SortableItem{
		{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "c", Label: "C"},
	}}, nil)
	return startBehaviorServer(t, string(list), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/order", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if n.Add(1) == 1 {
				time.Sleep(700 * time.Millisecond)
				w.WriteHeader(firstStatus)
				return
			}
			w.WriteHeader(laterStatus)
		})
	})
}

// twoMoves moves a down (b,a,c) and drops, then moves c up (b,c,a)
// and drops: two commits in flight on one list.
func twoMoves(t *testing.T, b *behaviorServer) func() string {
	t.Helper()
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-sortablelist")) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`[data-hui-sort-key="a"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "), chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(" "),
		chromedp.Focus(`[data-hui-sort-key="c"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "), chromedp.KeyEvent(kb.ArrowUp), chromedp.KeyEvent(" "),
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	return func() string { return evalString(t, ctx, sortKeysOf("ol")) }
}

// Both commits fail, the newer first: its rollback started from b,a,c,
// an order holding the first move the server never saved. The list
// must come back to a,b,c, the only order the server confirmed.
func TestE2E_SortableOverlappingFailuresRestoreTheConfirmedOrder(t *testing.T) {
	shown := twoMoves(t, overlapServer(t, http.StatusInternalServerError, http.StatusInternalServerError))
	if got := shown(); got != "a,b,c" {
		t.Fatalf("after both overlapping commits failed the list shows %q, want the server's a,b,c", got)
	}
}

// The newer commit fails at once while the older one is still out;
// the older one then succeeds. The server holds b,a,c, so the list
// ends there, not at the a,b,c the newer rollback first painted.
func TestE2E_SortableLateSaveAfterANewerFailureShowsTheSave(t *testing.T) {
	shown := twoMoves(t, overlapServer(t, http.StatusNoContent, http.StatusInternalServerError))
	if got := shown(); got != "b,a,c" {
		t.Fatalf("the older commit saved b,a,c after the newer one failed, but the list shows %q", got)
	}
}
