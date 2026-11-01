package headless

// The keyboard grab follows the screen and ends with the reader's
// attention; a slow failure never repaints over a later save.

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// sortKeysOf reads a list's keys in order, a grabbed row starred.
func sortKeysOf(id string) string {
	return `[...document.querySelectorAll('#` + id + ` [data-hui-sort-key]')].map(r => r.getAttribute('data-hui-sort-key') + (r.classList.contains('is-grabbed') ? '*' : '')).join(',')`
}

// In a right-to-left board, ArrowLeft moves a grabbed card to the
// column on its LEFT, which is the next column in document order, and
// ArrowRight brings it back.
func TestE2E_SortableCrossColumnArrowsFlipInRTL(t *testing.T) {
	col := func(id, key string) string {
		return string(SortableList(SortableListProps{ID: id, Label: id, Group: "board", Container: id,
			Items: []SortableItem{{Key: key, Label: key}}}, nil))
	}
	body := `<div dir="rtl">` + col("todo", "t1") + col("done", "d1") + `</div>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-sortablelist")) {
		t.Fatal("module never loaded")
	}
	where := `document.querySelector('[data-hui-sort-key="t1"]').closest('[data-hui-sortable]').id`
	if err := chromedp.Run(ctx,
		chromedp.Focus(`[data-hui-sort-key="t1"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "),
		chromedp.KeyEvent(kb.ArrowLeft),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, where); got != "done" {
		t.Fatalf("ArrowLeft in RTL left the card in %q, want the visually-left column \"done\"", got)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.ArrowRight)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, where); got != "todo" {
		t.Fatalf("ArrowRight in RTL left the card in %q, want it back in \"todo\"", got)
	}
}

// A grabbed row whose reader moves focus away does not stay grabbed
// with an unsaved move on screen: the grab cancels and the row goes
// back, as Escape does. The moves themselves, which re-insert the
// focused row, keep the grab.
func TestE2E_SortableGrabEndsWhenFocusLeaves(t *testing.T) {
	list := SortableList(SortableListProps{ID: "sl", Label: "Order", Items: []SortableItem{
		{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "c", Label: "C"},
	}}, nil)
	b := startBehaviorServer(t, string(list)+`<button id="elsewhere">x</button>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-sortablelist")) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`[data-hui-sort-key="a"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.KeyEvent(kb.ArrowDown),
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := evalString(t, ctx, sortKeysOf("sl")); got != "b,c,a*" {
		t.Fatalf("two moves of a grabbed row read %q, want b,c,a* (still grabbed)", got)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#elsewhere`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, sortKeysOf("sl")+` === 'a,b,c'`) {
		t.Fatalf("after focus left a grabbed row the list reads %q, want a,b,c (the grab cancelled)", evalString(t, ctx, sortKeysOf("sl")))
	}
}

// Two keyboard reorders in a row: the first commit fails slowly, the
// second succeeds at once. The server saved the second order (which
// already includes the first move); the first failure's rollback must
// not then paint an order older than the one the server holds.
func TestE2E_SortableLateFailureDoesNotRevertALaterSave(t *testing.T) {
	var n atomic.Int32
	var saved atomic.Value
	saved.Store("")
	list := SortableList(SortableListProps{ID: "ol", Label: "Order", RPCPath: "/__hui/order", Items: []SortableItem{
		{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "c", Label: "C"},
	}}, nil)
	b := startBehaviorServer(t, string(list), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/order", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if n.Add(1) == 1 {
				time.Sleep(700 * time.Millisecond)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			saved.Store(r.PostForm.Get("order"))
			w.WriteHeader(http.StatusNoContent)
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-sortablelist")) {
		t.Fatal("module never loaded")
	}
	// Move a down (b,a,c), drop; then move c up (b,c,a), drop.
	if err := chromedp.Run(ctx,
		chromedp.Focus(`[data-hui-sort-key="a"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "), chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(" "),
		chromedp.Focus(`[data-hui-sort-key="c"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "), chromedp.KeyEvent(kb.ArrowUp), chromedp.KeyEvent(" "),
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	s := saved.Load().(string)
	if s != "b,c,a" {
		t.Fatalf("the second commit saved %q, want b,c,a", s)
	}
	if shown := evalString(t, ctx, sortKeysOf("ol")); shown != s {
		t.Fatalf("the server saved order %q but the list shows %q after the earlier commit's late failure rolled back", s, shown)
	}
}
