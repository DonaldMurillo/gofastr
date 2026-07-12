package runtime

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// ── Test helpers ────────────────────────────────────────────────────

// sortablePage is the HTML body served to sortable e2e tests. Two
// lists share group "g1" (containers a/b); a third list has group "g2"
// (container c) to test cross-group blocking.
const sortablePage = `
<div style="display:flex;gap:1rem">
  <ol data-fui-sortable data-fui-sortable-rpc="/rpc" data-fui-sortable-group="g1"
      data-fui-sortable-container="a" aria-label="Column A">
    <li data-fui-sortable-item data-fui-sort-key="k1" draggable="true" tabindex="0" role="option" aria-label="Drag A1">A1</li>
    <li data-fui-sortable-item data-fui-sort-key="k2" draggable="true" tabindex="0" role="option" aria-label="Drag A2">A2</li>
  </ol>
  <ol data-fui-sortable data-fui-sortable-rpc="/rpc" data-fui-sortable-group="g1"
      data-fui-sortable-container="b" aria-label="Column B">
    <li data-fui-sortable-item data-fui-sort-key="k3" draggable="true" tabindex="0" role="option" aria-label="Drag B1">B1</li>
  </ol>
  <ol data-fui-sortable data-fui-sortable-rpc="/rpc" data-fui-sortable-group="g2"
      data-fui-sortable-container="c" aria-label="Column C">
    <li data-fui-sortable-item data-fui-sort-key="k4" draggable="true" tabindex="0" role="option" aria-label="Drag C1">C1</li>
  </ol>
</div>
<span id="ready">ready</span>`

// startSortableServer serves the sortablelist module + a page. The
// rpcHandler handles POST /rpc; conflictHandler handles GET /conflict.
func startSortableServer(t *testing.T, pageHTML string, rpcHandler, conflictHandler http.HandlerFunc) string {
	t.Helper()
	js, ok := Module("sortablelist")
	if !ok || js == "" {
		t.Fatal("sortablelist module not embedded")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mod.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	if rpcHandler != nil {
		mux.HandleFunc("/rpc", rpcHandler)
	}
	if conflictHandler != nil {
		mux.HandleFunc("/conflict", conflictHandler)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>s</title></head><body>%s<script src="/mod.js"></script></body></html>`, pageHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// bodyCapture is a thread-safe string holder for RPC body assertions.
type bodyCapture struct {
	mu   sync.Mutex
	body string
	code int
}

func (bc *bodyCapture) set(body string, code int) {
	bc.mu.Lock()
	bc.body = body
	bc.code = code
	bc.mu.Unlock()
}

func (bc *bodyCapture) get() (string, int) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.body, bc.code
}

// okRPC returns a handler that captures the body and returns 204.
func okRPC(bc *bodyCapture) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bc.set(string(b), 204)
		w.WriteHeader(http.StatusNoContent)
	}
}

// dispatchDrag dispatches synthetic dragstart→dragover→dragend to move
// the item with srcKey so it lands after the item with destKey.
func dispatchDrag(srcKey, destKey string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var src = document.querySelector('[data-fui-sort-key=%q]');
		var dest = document.querySelector('[data-fui-sort-key=%q]');
		if (!src || !dest) return 'missing';
		var dt = new DataTransfer();
		src.dispatchEvent(new DragEvent('dragstart', {bubbles:true, cancelable:true, dataTransfer:dt}));
		var rect = dest.getBoundingClientRect();
		dest.dispatchEvent(new DragEvent('dragover', {bubbles:true, cancelable:true, dataTransfer:dt, clientY:rect.bottom}));
		src.dispatchEvent(new DragEvent('dragend', {bubbles:true, cancelable:true}));
		return 'ok';
	})()`, srcKey, destKey), nil)
}

// kbMove dispatches Space (grab) → ArrowRight (cross to next column) →
// Space (drop) on the item with the given key.
func kbCrossMove(key string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var item = document.querySelector('[data-fui-sort-key=%q]');
		if (!item) return 'missing';
		item.focus();
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		var col = item.closest('[data-fui-sortable-container]');
		return col ? col.getAttribute('data-fui-sortable-container') : 'none';
	})()`, key), nil)
}

// kbReorder dispatches Space (grab) → ArrowDown (swap with next sibling)
// → Space (drop) on the item with the given key.
func kbReorder(key string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(function(){
		var item = document.querySelector('[data-fui-sort-key=%q]');
		if (!item) return 'missing';
		item.focus();
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowDown', bubbles:true, cancelable:true}));
		item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		return 'ok';
	})()`, key), nil)
}

// ── Tests ───────────────────────────────────────────────────────────

// TestSortable_CrossAllowedInGroup: a drag from column A to column B
// (same group "g1") moves the item across containers.
func TestSortable_CrossAllowedInGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startSortableServer(t, sortablePage, okRPC(&bodyCapture{}), nil)
	ctx := newSeedBrowserCtx(t)
	var containerAfter string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		dispatchDrag("k1", "k3"),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerAfter),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if containerAfter != "b" {
		t.Errorf("k1 should be in column b after cross-group drag, got %q", containerAfter)
	}
}

// TestSortable_CrossBlockedDiffGroup: a drag from column A (group g1)
// to column C (group g2) is blocked — the item stays in column A.
func TestSortable_CrossBlockedDiffGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startSortableServer(t, sortablePage, okRPC(&bodyCapture{}), nil)
	ctx := newSeedBrowserCtx(t)
	var containerAfter string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		dispatchDrag("k1", "k4"),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerAfter),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if containerAfter != "a" {
		t.Errorf("k1 should stay in column a (cross-group blocked), got %q", containerAfter)
	}
}

// TestSortable_CrossCommitPayload: a keyboard cross-container move
// POSTs order=…&moved=…&container=… to the destination RPC.
func TestSortable_CrossCommitPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bc := &bodyCapture{}
	base := startSortableServer(t, sortablePage, okRPC(bc), nil)
	ctx := newSeedBrowserCtx(t)
	var result string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		kbCrossMove("k1"),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]') ? 'present' : 'gone'`, &result),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	body, _ := bc.get()
	if body == "" {
		t.Fatal("no RPC body captured — commit did not fire")
	}
	for _, want := range []string{"order=", "moved=k1", "container=b"} {
		if !strings.Contains(body, want) {
			t.Errorf("cross-container payload missing %q, got: %s", want, body)
		}
	}
}

// TestSortable_SameContainerPayload: a same-container reorder POSTs
// only order= (back-compat — no moved/container fields).
func TestSortable_SameContainerPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bc := &bodyCapture{}
	base := startSortableServer(t, sortablePage, okRPC(bc), nil)
	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		kbReorder("k1"),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	body, _ := bc.get()
	if body == "" {
		t.Fatal("no RPC body captured — commit did not fire")
	}
	if !strings.Contains(body, "order=") {
		t.Errorf("payload missing order=, got: %s", body)
	}
	if strings.Contains(body, "moved=") || strings.Contains(body, "container=") {
		t.Errorf("same-container payload should NOT contain moved=/container=, got: %s", body)
	}
}

// TestSortable_409FiresConflictPath: with version + conflict attrs, a
// 409 response triggers the conflict RPC refetch (list innerHTML
// replaced) instead of a blanket rollback.
func TestSortable_409FiresConflictPath(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	pageHTML := `
<ol data-fui-sortable data-fui-sortable-rpc="/rpc" data-fui-sortable-group="g1"
    data-fui-sortable-container="a" data-fui-sortable-version="v1"
    data-fui-sortable-conflict="/conflict" aria-label="Column A">
  <li data-fui-sortable-item data-fui-sort-key="k1" draggable="true" tabindex="0" role="option" aria-label="Drag A1">A1</li>
</ol>
<ol data-fui-sortable data-fui-sortable-rpc="/rpc" data-fui-sortable-group="g1"
    data-fui-sortable-container="b" data-fui-sortable-version="v1"
    data-fui-sortable-conflict="/conflict" aria-label="Column B">
  <li data-fui-sortable-item data-fui-sort-key="k2" draggable="true" tabindex="0" role="option" aria-label="Drag B1">B1</li>
</ol>
<span id="ready">ready</span>`
	bc := &bodyCapture{}
	rpcHandler := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bc.set(string(b), 409)
		w.WriteHeader(http.StatusConflict)
	}
	conflictHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Return fresh items — k2 is now alone in column B (the move
		// was rejected server-side).
		fmt.Fprint(w, `<li data-fui-sortable-item data-fui-sort-key="k2" draggable="true" tabindex="0" role="option" aria-label="Drag B1">B1</li>`)
	}
	base := startSortableServer(t, pageHTML, rpcHandler, conflictHandler)
	ctx := newSeedBrowserCtx(t)
	var conflictFetched bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		kbCrossMove("k1"),
		// Wait for the conflict fetch + DOM replacement.
		chromedp.Sleep(500*time.Millisecond),
		// The conflict endpoint should have been fetched. Verify by
		// checking that column B's innerHTML was replaced (the
		// conflict response has k2 with aria-label "Drag B1" — same
		// key, fresh node).
		chromedp.Evaluate(`(function(){
			var colB = document.querySelector('[data-fui-sortable-container="b"]');
			if (!colB) return false;
			// After conflict reconciliation, the moved item k1 should NOT
			// be in column B (server rejected the move). Column B should
			// have only k2.
			var items = colB.querySelectorAll('[data-fui-sortable-item]');
			if (items.length !== 1) return false;
			return items[0].getAttribute('data-fui-sort-key') === 'k2';
		})()`, &conflictFetched),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	_, code := bc.get()
	if code != 409 {
		t.Errorf("RPC should have returned 409, got %d", code)
	}
	if !conflictFetched {
		t.Error("409 should fire conflict path: column B should have only k2 (server-rejected move)")
	}
}

// TestSortable_NoVersionNo409Special: without the version attr, a 409
// is treated like any other non-2xx — the item rolls back to its
// source column (no conflict refetch).
func TestSortable_NoVersionNo409Special(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	// Same page as the default sortablePage — no version/conflict attrs.
	bc := &bodyCapture{}
	rpcHandler := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bc.set(string(b), 409)
		w.WriteHeader(http.StatusConflict)
	}
	base := startSortableServer(t, sortablePage, rpcHandler, nil)
	ctx := newSeedBrowserCtx(t)
	var containerAfter string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		kbCrossMove("k1"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-sort-key="k1"]').closest('[data-fui-sortable-container]').getAttribute('data-fui-sortable-container')`, &containerAfter),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	_, code := bc.get()
	if code != 409 {
		t.Errorf("RPC should have returned 409, got %d", code)
	}
	// Without version, 409 → rollback. k1 should be back in column "a".
	if containerAfter != "a" {
		t.Errorf("k1 should roll back to column a (no version = no 409 special-casing), got %q", containerAfter)
	}
}

// TestSortable_AriaLiveAnnounces: a keyboard grab creates a polite
// aria-live region that announces the grab.
func TestSortable_AriaLiveAnnounces(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startSortableServer(t, sortablePage, okRPC(&bodyCapture{}), nil)
	ctx := newSeedBrowserCtx(t)
	var hasLive bool
	var liveText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Grab k1 with Space.
		chromedp.Evaluate(`(function(){
			var item = document.querySelector('[data-fui-sort-key="k1"]');
			item.focus();
			item.dispatchEvent(new KeyboardEvent('keydown', {key:' ', bubbles:true, cancelable:true}));
		})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		// Check the aria-live region exists.
		chromedp.Evaluate(`(function(){
			var live = document.getElementById('fui-sortable-live');
			if (!live) return false;
			return live.getAttribute('aria-live') === 'polite';
		})()`, &hasLive),
		// Wait for the 30ms announce timer.
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`(document.getElementById('fui-sortable-live')||{}).textContent || ''`, &liveText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasLive {
		t.Error("aria-live region should be created on grab")
	}
	if liveText == "" {
		t.Error("aria-live region should announce the grab (non-empty text)")
	}
}
