package headless

// Browser coverage for headless-sortablelist: the keyboard reorder
// (Space grabs, arrows move, Space drops, Escape cancels), the
// announcement each move makes through the polite live region (from
// the Strings that travel as attributes), the server round trip (a
// commit POSTs the new order; a non-2xx reverts the DOM), and the
// plain-list no-script posture. Same harness as behavior_e2e_test.go.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const sortableLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-sortablelist'])`

func sortKey(key string) chromedp.Action {
	return chromedp.Evaluate("document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'"+key+"',bubbles:true,cancelable:true}))", nil)
}

// sortableServer serves a page with one list bound to a POST /commit
// that records the order and answers ok or fail.
func sortableServer(t *testing.T, body string, fail bool) (*behaviorServer, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/commit", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			mu.Lock()
			got = append(got, r.FormValue("order"))
			mu.Unlock()
			if fail {
				http.Error(w, "no", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	b := startBehaviorServer(t, body, extra)
	return b, &got
}

func sortablePage(items ...SortableItem) string {
	return string(SortableList(SortableListProps{
		Label: "Priorities", RPCPath: "/commit", Items: items,
	}, nil))
}

// TestE2E_SortableKeyboardReorderAnnouncesAndCommits walks the whole
// keyboard model: Space grabs (the grabbed announcement lands in the
// live region), ArrowDown moves the row (the position announcement
// follows), Space drops and the commit POSTs the new order, which a
// fresh read of the DOM confirms survived.
func TestE2E_SortableKeyboardReorderAnnouncesAndCommits(t *testing.T) {
	b, commits := sortableServer(t, sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
		SortableItem{Key: "c", Label: "Gamma"},
	), false)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}

	var live, orderAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a"]').focus()`, nil),
		sortKey(" "),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('hui-sortable-live').textContent`, &live),
		sortKey("ArrowDown"),
		sortKey("ArrowDown"),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('hui-sortable-live').textContent`, &live),
		sortKey(" "),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',')`, &orderAfter),
	); err != nil {
		t.Fatal(err)
	}
	if live == "" {
		t.Fatal("the moves never announced through the live region")
	}
	if want := "b,c,a"; orderAfter != want {
		t.Fatalf("after the keyboard reorder the order is %q, want %q", orderAfter, want)
	}
	// The commit is async: yield the page until the server has seen
	// it, then read the recorded order.
	for range 10 {
		if len(*commits) > 0 {
			break
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*commits) == 0 {
		t.Fatal("the drop never POSTed the order")
	}
	if got := (*commits)[len(*commits)-1]; got != "b,c,a" {
		t.Fatalf("the commit sent order=%q, want b,c,a", got)
	}
}

// TestE2E_SortableFailedCommitReverts proves the server stays
// authoritative: a non-2xx commit puts the rows back where they were.
func TestE2E_SortableFailedCommitReverts(t *testing.T) {
	b, _ := sortableServer(t, sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), true)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	var orderAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a"]').focus()`, nil),
		sortKey(" "),
		sortKey("ArrowDown"),
		sortKey(" "),
	); err != nil {
		t.Fatal(err)
	}
	// The revert races the response; poll for it.
	if !pollTrue(ctx, `Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',') === 'a,b'`) {
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',')`, &orderAfter)); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("a failed commit must revert the DOM; order is %q", orderAfter)
	}
}

// TestE2E_SortableEscapeCancels proves Escape puts the grabbed row
// back uncommitted, with the cancellation announced.
func TestE2E_SortableEscapeCancels(t *testing.T) {
	b, commits := sortableServer(t, sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), false)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	var live string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a"]').focus()`, nil),
		sortKey(" "),
		sortKey("ArrowDown"),
		sortKey("Escape"),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('hui-sortable-live').textContent`, &live),
	); err != nil {
		t.Fatal(err)
	}
	if live != "Cancelled." {
		t.Fatalf("Escape should announce the cancellation, got %q", live)
	}
	if len(*commits) != 0 {
		t.Fatalf("Escape must not commit; the server saw %d commits", len(*commits))
	}
}

// TestE2E_SortableCrossContainerMove proves the kanban crossing: two
// linked lists share a Group, ArrowRight moves the grabbed row to the
// adjacent column, and the commit carries moved= and container=.
func TestE2E_SortableCrossContainerMove(t *testing.T) {
	todo := string(SortableList(SortableListProps{
		Label: "To do", Group: "board", Container: "todo", RPCPath: "/commit",
		Items: []SortableItem{{Key: "k1", Label: "Design API"}},
	}, nil))
	doing := string(SortableList(SortableListProps{
		Label: "Doing", Group: "board", Container: "doing", RPCPath: "/commit",
	}, nil))

	var mu sync.Mutex
	var bodies []string
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/commit", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})
	}
	b := startBehaviorServer(t, todo+doing, extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="k1"]').focus()`, nil),
		sortKey(" "),
		sortKey("ArrowRight"),
		sortKey(" "),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[aria-label="Doing"] > [data-hui-sortable-item]').length === 1`) {
		t.Fatal("the row never crossed into the Doing column")
	}
	// The row moves in the DOM before its commit reaches the handler, so
	// wait for the request instead of reading bodies once.
	var got string
	for range 40 {
		mu.Lock()
		if len(bodies) > 0 {
			got = bodies[len(bodies)-1]
		}
		mu.Unlock()
		if got != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got == "" {
		t.Fatal("the crossing never committed")
	}
	v, _ := url.ParseQuery(got)
	if v.Get("moved") != "k1" || v.Get("container") != "doing" || v.Get("order") != "k1" {
		t.Fatalf("cross-container commit sent %q, want moved=k1 container=doing order=k1", got)
	}
}

// TestE2E_SortablePlainListWithoutScript proves the no-script
// fallback the pattern offered: with the runtime blocked, the page is
// still a plain ordered list of rows a form would submit.
func TestE2E_SortablePlainListWithoutScript(t *testing.T) {
	b, _ := sortableServer(t, sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), false)
	// The rows are the no-script contract: with the module loaded or
	// not, the list is an <ol> of <li> rows the server rendered.
	ctx := behaviorPage(t, b)
	var rows int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-sortable] > li').length`, &rows),
	); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("the list should render its rows as plain <li>, got %d", rows)
	}
}

// ── the versioned-409 conflict family and the pointer-drag path ────
//
// Ported from the retired core-ui/runtime sortablelist e2e suite,
// which died with the module: these are the browser proofs of the
// hard bounds on the 409 body (JSON content-type, ~4 KB read, the
// {"error":{"message"}} shape), the conflict refresh that replaces a
// column with the server's own rows, and the DragEvent triple the
// pointer path runs — none of which the keyboard tests above touch.

// sortableColumn renders one linked column against /rpc.
func sortableColumn(label, container, group string, items ...SortableItem) string {
	return string(SortableList(SortableListProps{
		Label: label, Group: group, Container: container, RPCPath: "/rpc",
		Items: items,
	}, nil))
}

// sortableConflictPage renders two linked, versioned columns whose
// commits can 409: the kanban shape where the server holds the
// authoritative order. Column A holds k1, column B holds k2.
func sortableConflictPage() string {
	a := string(SortableList(SortableListProps{
		Label: "Column A", Group: "g1", Container: "a",
		RPCPath: "/rpc", Version: "v1", ConflictRPC: "/conflict",
		Items: []SortableItem{{Key: "k1", Label: "A1"}},
	}, nil))
	b := string(SortableList(SortableListProps{
		Label: "Column B", Group: "g1", Container: "b",
		RPCPath: "/rpc", Version: "v1", ConflictRPC: "/conflict",
		Items: []SortableItem{{Key: "k2", Label: "B1"}},
	}, nil))
	return a + b
}

// conflict409Server serves the two versioned columns plus a /rpc
// that answers 409 with the given content type and body, and a
// /conflict that returns the server's own rows for column B — k2
// alone, the move rejected server-side.
func conflict409Server(t *testing.T, contentType, body409 string) *behaviorServer {
	t.Helper()
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			w.WriteHeader(http.StatusConflict)
			if body409 != "" {
				fmt.Fprint(w, body409)
			}
		})
		mux.HandleFunc("/conflict", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, string(SortableItems(SortableListProps{
				Label: "Column B", Items: []SortableItem{{Key: "k2", Label: "B1"}},
			}, nil)))
		})
	}
	return startBehaviorServer(t, sortableConflictPage(), extra)
}

// sortableBodyServer serves the body with a POST /rpc that records
// every raw request body and answers 204.
func sortableBodyServer(t *testing.T, body string) (*behaviorServer, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})
	}
	b := startBehaviorServer(t, body, extra)
	return b, &bodies
}

// waitCommit yields until the server has seen a commit and returns
// the last raw body, "" when none arrived in the window.
func waitCommit(ctx context.Context, bodies *[]string) string {
	for range 30 {
		if len(*bodies) > 0 {
			return (*bodies)[len(*bodies)-1]
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
			return ""
		}
	}
	return ""
}

// kbCrossMove dispatches Space (grab) → ArrowRight (cross to the
// adjacent column of the group) → Space (drop) on the row with the
// given key.
func kbCrossMove(key string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var item = document.querySelector('[data-hui-sort-key=%q]');
		if (!item) return 'missing';
		item.focus();
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		return 'ok';
	})()`, key), nil)
}

// kbReorder dispatches Space (grab) → ArrowDown (swap with the next
// sibling) → Space (drop) on the row with the given key.
func kbReorder(key string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var item = document.querySelector('[data-hui-sort-key=%q]');
		if (!item) return 'missing';
		item.focus();
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowDown', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		return 'ok';
	})()`, key), nil)
}

// dispatchDrag dispatches synthetic dragstart → dragover → dragend
// moving the row with srcKey so it lands after the row with destKey.
func dispatchDrag(srcKey, destKey string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var src = document.querySelector('[data-hui-sort-key=%q]');
		var dest = document.querySelector('[data-hui-sort-key=%q]');
		if (!src || !dest) return 'missing';
		var dt = new DataTransfer();
		src.dispatchEvent(new DragEvent('dragstart', {bubbles:true, cancelable:true, dataTransfer:dt}));
		var rect = dest.getBoundingClientRect();
		dest.dispatchEvent(new DragEvent('dragover', {bubbles:true, cancelable:true, dataTransfer:dt, clientY:rect.bottom}));
		src.dispatchEvent(new DragEvent('dragend', {bubbles:true, cancelable:true}));
		return 'ok';
	})()`, srcKey, destKey), nil)
}

// dispatchDragToColumn dispatches the drag triple landing the srcKey
// row on the empty list of the given container: with no row to
// hit-test, the list element itself is the drop target.
func dispatchDragToColumn(srcKey, container string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var src = document.querySelector('[data-hui-sort-key=%q]');
		var col = document.querySelector('[data-hui-sortable-container=%q]');
		if (!src || !col) return 'missing';
		var dt = new DataTransfer();
		src.dispatchEvent(new DragEvent('dragstart', {bubbles:true, cancelable:true, dataTransfer:dt}));
		var rect = col.getBoundingClientRect();
		col.dispatchEvent(new DragEvent('dragover', {bubbles:true, cancelable:true, dataTransfer:dt, clientY:rect.top + rect.height/2}));
		src.dispatchEvent(new DragEvent('dragend', {bubbles:true, cancelable:true}));
		return 'ok';
	})()`, srcKey, container), nil)
}

// waitLive polls until the live region contains want.
func waitLive(ctx context.Context, want string) bool {
	return pollTrue(ctx, `((document.getElementById('hui-sortable-live')||{}).textContent||'').indexOf(`+strconv.Quote(want)+`) !== -1`)
}

// readLive reads the live region's full text into dst.
func readLive(ctx context.Context, dst *string) {
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(document.getElementById('hui-sortable-live')||{}).textContent || ''`, dst)); err != nil {
		return
	}
}

// TestE2E_Sortable409FiresConflictPath proves the reconciliation a
// versioned 409 runs instead of a blanket rollback: the conflict
// endpoint's rows replace the destination's innerHTML, so a move the
// server rejected leaves column B holding exactly the server's own
// k2 — and the source snapshot puts k1 back in column A.
func TestE2E_Sortable409FiresConflictPath(t *testing.T) {
	b := conflict409Server(t, "", "")
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	var k1InA bool
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	// The refreshed copy is what tells reconciliation from a plain
	// rollback: both leave column B holding k2 alone and k1 back in
	// A, only the conflict path announces the refresh.
	if !waitLive(ctx, "List refreshed") {
		var live string
		readLive(ctx, &live)
		t.Fatalf("the 409 never fired the conflict path (live region says %q)", live)
	}
	if !pollTrue(ctx, `(function(){
		var colB = document.querySelectorAll('[data-hui-sortable-container="b"] > [data-hui-sortable-item]');
		return colB.length === 1 && colB[0].getAttribute('data-hui-sort-key') === 'k2';
	})()`) {
		t.Fatal("the conflict refresh should reconcile column B to the server's own rows (k2 alone)")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('[data-hui-sortable-container="a"] > [data-hui-sort-key="k1"]')`, &k1InA)); err != nil {
		t.Fatal(err)
	}
	if !k1InA {
		t.Error("the conflict refresh should restore k1 to column A from the source snapshot")
	}
}

// TestE2E_Sortable409ConflictMessageAnnounced: a 409 with a valid
// JSON problem-detail body surfaces error.message through the polite
// live region AND still runs the authoritative refresh.
func TestE2E_Sortable409ConflictMessageAnnounced(t *testing.T) {
	msg := "Cannot move ORB-12 to Done because ORB-9 is incomplete."
	body := `{"error":{"code":"transition_blocked","message":` + strconv.Quote(msg) + `}}`
	b := conflict409Server(t, "application/json; charset=utf-8", body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "ORB-12") {
		t.Fatal("the bounded server message never reached the live region")
	}
	var live string
	readLive(ctx, &live)
	if !strings.Contains(live, "ORB-12") || !strings.Contains(live, "ORB-9") {
		t.Errorf("live region should announce the conflict message %q, got %q", msg, live)
	}
	var colBHasK1 bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('[data-hui-sortable-container="b"] [data-hui-sort-key="k1"]')`, &colBHasK1)); err != nil {
		t.Fatal(err)
	}
	if colBHasK1 {
		t.Error("conflict refresh should still run: k1 must not remain in column B")
	}
}

// TestE2E_Sortable409InvariantMessage: a business-invariant 409
// surfaces its distinct message, which REPLACES the generic conflict
// copy the way the retired module's finishConflict did.
func TestE2E_Sortable409InvariantMessage(t *testing.T) {
	msg := "ORB-9 is incomplete; complete it before moving dependents."
	body := `{"error":{"code":"dependency_blocked","message":` + strconv.Quote(msg) + `}}`
	b := conflict409Server(t, "application/json", body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "dependents") {
		t.Fatal("the invariant message never reached the live region")
	}
	var live string
	readLive(ctx, &live)
	if !strings.Contains(live, "dependents") || !strings.Contains(live, "ORB-9") {
		t.Errorf("live region should announce the invariant message, got %q", live)
	}
	if strings.Contains(live, "List refreshed") {
		t.Errorf("when a message is present it should replace the generic copy, got %q", live)
	}
}

// TestE2E_Sortable409MalformedFallback: a 409 with a JSON
// content-type but an unparseable body falls back to the generic
// copy — and never toasts it.
func TestE2E_Sortable409MalformedFallback(t *testing.T) {
	b := conflict409Server(t, "application/json", "{not valid json")
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "Conflict.") {
		t.Fatal("a malformed 409 never announced its outcome")
	}
	var live string
	readLive(ctx, &live)
	if live != "Conflict. List refreshed from server." {
		t.Errorf("malformed 409 should fall back to the generic copy, got %q", live)
	}
	if strings.Contains(live, "not valid json") {
		t.Errorf("malformed body must not leak into the live region, got %q", live)
	}
	assertNoToast(ctx, t)
}

// TestE2E_Sortable409OversizedFallback: a 409 body larger than the
// ~4 KB read bound falls back to the generic copy (truncation breaks
// the parse); nothing of the body leaks.
func TestE2E_Sortable409OversizedFallback(t *testing.T) {
	big := strings.Repeat("x", 8000)
	body := `{"error":{"message":"` + big + `"}}`
	b := conflict409Server(t, "application/json", body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "Conflict.") {
		t.Fatal("an oversized 409 never announced its outcome")
	}
	var live string
	readLive(ctx, &live)
	if len(live) > 100 {
		t.Errorf("oversized 409 should fall back to the short generic copy, got %d bytes: %q", len(live), live)
	}
	if strings.Contains(live, "xxxx") {
		t.Errorf("oversized body must not leak into the live region, got %q", live)
	}
}

// TestE2E_Sortable409HTMLFallback: a 409 whose content-type is not
// JSON is never parsed, whatever its body says. The body here is
// deliberately well-formed JSON — a parseable message under
// text/html — so the content-type check is the only thing standing
// between the body and the live region.
func TestE2E_Sortable409HTMLFallback(t *testing.T) {
	b := conflict409Server(t, "text/html; charset=utf-8", `{"error":{"message":"<html>boom</html>"}}`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "Conflict.") {
		t.Fatal("a non-JSON 409 never announced its outcome")
	}
	var live string
	readLive(ctx, &live)
	if live != "Conflict. List refreshed from server." {
		t.Errorf("non-JSON 409 should fall back to the generic copy, got %q", live)
	}
	if strings.Contains(live, "boom") {
		t.Errorf("an HTML body must not reach the live region, got %q", live)
	}
}

// TestE2E_Sortable409EmptyBodyBackwardCompat: an empty 409 body
// keeps the generic copy and still runs the conflict refresh.
func TestE2E_Sortable409EmptyBodyBackwardCompat(t *testing.T) {
	b := conflict409Server(t, "", "")
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !waitLive(ctx, "Conflict.") {
		t.Fatal("an empty 409 never announced its outcome")
	}
	var live string
	readLive(ctx, &live)
	if live != "Conflict. List refreshed from server." {
		t.Errorf("empty 409 body should keep the generic copy, got %q", live)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-sortable-container="b"] > [data-hui-sortable-item]').length === 1`) {
		t.Error("empty 409 body should still run the conflict refresh (column B = 1 item)")
	}
}

// TestE2E_SortableCrossBlockedDiffGroup: lists of different groups
// stay isolated — a drag from g1 onto g2 neither moves the row nor
// commits anything.
func TestE2E_SortableCrossBlockedDiffGroup(t *testing.T) {
	page := sortableColumn("Column A", "a", "g1",
		SortableItem{Key: "k1", Label: "A1"}, SortableItem{Key: "k2", Label: "A2"}) +
		sortableColumn("Column B", "b", "g1", SortableItem{Key: "k3", Label: "B1"}) +
		sortableColumn("Column C", "c", "g2", SortableItem{Key: "k4", Label: "C1"})
	b, bodies := sortableBodyServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, dispatchDrag("k1", "k4")); err != nil {
		t.Fatal(err)
	}
	chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond))
	var containerAfter string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(document.querySelector('[data-hui-sort-key="k1"]').closest('[data-hui-sortable-container]')||{}).getAttribute('data-hui-sortable-container') || 'none'`, &containerAfter)); err != nil {
		t.Fatal(err)
	}
	if containerAfter != "a" {
		t.Errorf("k1 should stay in column a (cross-group blocked), got %q", containerAfter)
	}
	if got := waitCommit(ctx, bodies); got != "" {
		t.Errorf("a blocked cross-group drag must not commit, got %q", got)
	}
}

// TestE2E_SortableSameContainerPayload: a same-container reorder on
// a list WITH a container POSTs order= plus container= and no
// moved=, so the server can route the write without inferring the
// column from the key set.
func TestE2E_SortableSameContainerPayload(t *testing.T) {
	b, bodies := sortableBodyServer(t, sortableColumn("Column A", "a", "",
		SortableItem{Key: "k1", Label: "A1"}, SortableItem{Key: "k2", Label: "A2"}))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbReorder("k1")); err != nil {
		t.Fatal(err)
	}
	got := waitCommit(ctx, bodies)
	if got == "" {
		t.Fatal("the reorder never committed")
	}
	v, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("commit body does not parse: %q", got)
	}
	if v.Get("order") != "k2,k1" {
		t.Errorf("same-container payload order = %q, want k2,k1", v.Get("order"))
	}
	if v.Get("container") != "a" {
		t.Errorf("same-container payload should carry container=a, got %q", got)
	}
	if v.Has("moved") {
		t.Errorf("same-container payload should NOT carry moved=, got %q", got)
	}
}

// TestE2E_SortableCrossCommitNoContainer: a cross-container move
// between lists that never configured a container still carries the
// container field, empty — cross behaviour is unchanged by the
// same-container rules.
func TestE2E_SortableCrossCommitNoContainer(t *testing.T) {
	page := sortableColumn("Column A", "", "g1",
		SortableItem{Key: "k1", Label: "A1"}, SortableItem{Key: "k2", Label: "A2"}) +
		sortableColumn("Column B", "", "g1", SortableItem{Key: "k3", Label: "B1"})
	b, bodies := sortableBodyServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	got := waitCommit(ctx, bodies)
	if got == "" {
		t.Fatal("the crossing never committed")
	}
	v, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("commit body does not parse: %q", got)
	}
	if v.Get("order") != "k3,k1" {
		t.Errorf("no-container cross payload order = %q, want k3,k1", v.Get("order"))
	}
	if v.Get("moved") != "k1" {
		t.Errorf("no-container cross payload moved = %q, want k1", v.Get("moved"))
	}
	if !v.Has("container") || v.Get("container") != "" {
		t.Errorf("no-container cross payload should carry an empty container=, got %q", got)
	}
}

// TestE2E_SortableSameContainerDragWithContainer: the pointer path
// for a same-container reorder on a list with a container carries
// container= too — the payload rules cover drag and keyboard alike.
func TestE2E_SortableSameContainerDragWithContainer(t *testing.T) {
	b, bodies := sortableBodyServer(t, sortableColumn("Column A", "a", "",
		SortableItem{Key: "k1", Label: "A1"}, SortableItem{Key: "k2", Label: "A2"}))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, dispatchDrag("k1", "k2")); err != nil {
		t.Fatal(err)
	}
	got := waitCommit(ctx, bodies)
	if got == "" {
		t.Fatal("the drag never committed")
	}
	v, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("commit body does not parse: %q", got)
	}
	if v.Get("container") != "a" {
		t.Errorf("same-container drag should carry container=a, got %q", got)
	}
	if v.Has("moved") {
		t.Errorf("same-container drag should NOT carry moved=, got %q", got)
	}
}

// TestE2E_SortableDragIntoEmptyColumn: the pointer path onto an
// empty column — no row to hit-test, the list itself is the drop
// target — moves the row across and commits the crossing.
func TestE2E_SortableDragIntoEmptyColumn(t *testing.T) {
	page := sortableColumn("Column A", "a", "g1", SortableItem{Key: "k1", Label: "A1"}) +
		sortableColumn("Column B", "b", "g1")
	b, bodies := sortableBodyServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, dispatchDragToColumn("k1", "b")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.querySelector('[data-hui-sortable-container="b"] [data-hui-sort-key="k1"]')`) {
		t.Fatal("the row never dropped into the empty column")
	}
	got := waitCommit(ctx, bodies)
	v, _ := url.ParseQuery(got)
	if v.Get("moved") != "k1" || v.Get("container") != "b" {
		t.Errorf("the drop into the empty column should commit moved=k1 container=b, got %q", got)
	}
}

// TestE2E_SortableConflictToastShowsTheMessage proves the visible
// half of a conflict message: a versioned 409 with a server message
// raises an error toast carrying that message (the live region is
// visually hidden — without the toast a sighted reader learns nothing
// of the conflict), with the error tone and the retired module's
// 6 s lifetime. The toast module loads on demand through the kernel.
func TestE2E_SortableConflictToastShowsTheMessage(t *testing.T) {
	msg := "Cannot move ORB-12 to Done because ORB-9 is incomplete."
	body := `{"error":{"code":"transition_blocked","message":` + strconv.Quote(msg) + `}}`
	b := conflict409Server(t, "application/json; charset=utf-8", body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(function(){
		var t = document.querySelector('[data-hui-toast]');
		return !!t && t.textContent.indexOf('ORB-12') !== -1 && t.textContent.indexOf('ORB-9') !== -1;
	})()`) {
		t.Fatal("the conflict message never showed as a toast")
	}
	var props string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var t = document.querySelector('[data-hui-toast]');
		if (!t) return '';
		// The lifetime rides the item row, the wrapper's parent.
		var ttl = t.parentElement && t.parentElement.getAttribute('data-hui-toast-ttl-ms');
		return [t.className, t.getAttribute('role') || '', ttl || ''].join('|');
	})()`, &props)); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(props, "|")
	if len(parts) != 3 {
		t.Fatalf("the toast row lost its props: %q", props)
	}
	cls, role, ttl := parts[0], parts[1], parts[2]
	if !strings.Contains(cls, "fui-notification--danger") {
		t.Errorf("the conflict toast should carry the danger tone, got class %q", cls)
	}
	if role != "alert" {
		t.Errorf("an error toast interrupts: role = %q, want alert", role)
	}
	if ttl != "6000" {
		t.Errorf("the conflict toast should keep the retired 6s lifetime, got ttl %q", ttl)
	}
}

// assertNoToast fails when a toast row exists: the generic conflict
// copy is announcement-only and must never toast.
func assertNoToast(ctx context.Context, t *testing.T) {
	t.Helper()
	chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))
	var n int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-hui-toast]').length`, &n)); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the generic conflict copy must never toast, found %d toast rows", n)
	}
}

// TestE2E_SortableDataShapedKeysReorderAndCommit: keys are data a
// database hands the page. Two rows whose keys carry a space and a
// quote render (no refusal), reorder under the keyboard model, and
// the commit POSTs order= with both keys intact — the module reads
// keys back through getAttribute and never interpolates one into a
// selector.
func TestE2E_SortableDataShapedKeysReorderAndCommit(t *testing.T) {
	b, commits := sortableServer(t, sortablePage(
		SortableItem{Key: "a b", Label: "Alpha"},
		SortableItem{Key: `x"y`, Label: "Beta"},
	), false)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	var orderAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a b"]').focus()`, nil),
		sortKey(" "),
		sortKey("ArrowDown"),
		sortKey(" "),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',') === 'x"y,a b'`) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',')`, &orderAfter)); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("data-shaped keys should reorder under the keyboard model, got %q", orderAfter)
	}
	for range 30 {
		if len(*commits) > 0 {
			break
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*commits) == 0 {
		t.Fatal("the drop never POSTed the order")
	}
	if got := (*commits)[len(*commits)-1]; got != `x"y,a b` {
		t.Fatalf("the commit sent order=%q, want %q", got, `x"y,a b`)
	}
}
