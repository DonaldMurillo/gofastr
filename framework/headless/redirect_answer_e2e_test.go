package headless

// fetch follows a 3xx itself, so a behaviour's request whose answer was
// a redirect resolves with a 200 for another URL; resp.redirected is the
// only sign (#577). Each test answers one behaviour's request with a 303
// to a same-origin URL whose body the behaviour could otherwise take as
// its own answer. The runtime's own callers are pinned in
// core-ui/runtime/redirect_answer_e2e_test.go.

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// raLogin serves the page an expired session sends a request to.
func raLogin(mux *http.ServeMux) {
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Sign in</title></head><body>`+
			`<main role="main"><h1 id="login">Sign in</h1></main></body></html>`)
	})
}

const raOrder = `Array.from(document.querySelectorAll('[data-hui-sortable-item]')).map(function (li) { return li.getAttribute('data-hui-sort-key'); }).join(',')`

// A reorder whose commit redirects (an expired session) was not saved:
// the rows go back and the reverted copy is announced, never the saved
// one.
func TestE2E_SortableRedirectedCommitNotSaved(t *testing.T) {
	b := startBehaviorServer(t, sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), raLogin, func(mux *http.ServeMux) {
		mux.HandleFunc("/commit", func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbReorder("a")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, raOrder+` === 'a,b'`) {
		var order string
		_ = chromedp.Run(ctx, chromedp.Evaluate(raOrder, &order))
		t.Fatalf("a redirected commit must revert the rows; order is %q", order)
	}
	var live string
	if !waitLive(ctx, "Save failed. Reverted.") {
		readLive(ctx, &live)
		t.Errorf("the live region says %q, want the reverted copy", live)
	}
}

// A 409's conflict refresh that redirects mounts nothing: the redirect
// target's markup never replaces the column, and the move rolls back.
func TestE2E_SortableRedirectedConflictRefreshMountsNothing(t *testing.T) {
	var stolen atomic.Int32
	b := startBehaviorServer(t, sortableConflictPage(), func(mux *http.ServeMux) {
		mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			w.WriteHeader(http.StatusConflict)
		})
		mux.HandleFunc("/conflict", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/conflict2", http.StatusSeeOther)
		})
		mux.HandleFunc("/conflict2", func(w http.ResponseWriter, _ *http.Request) {
			stolen.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, string(SortableItems(SortableListProps{
				Label: "Column B", Items: []SortableItem{{Key: "k9", Label: "STOLEN"}},
			}, nil)))
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}
	if err := chromedp.Run(ctx, kbCrossMove("k1")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for stolen.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if stolen.Load() == 0 {
		t.Fatal("the conflict refresh never followed the redirect")
	}
	if !pollTrue(ctx, `!!document.querySelector('[data-hui-sortable-container="a"] > [data-hui-sort-key="k1"]')`) {
		t.Error("the move must roll back to column A")
	}
	var k9 bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('[data-hui-sort-key="k9"]')`, &k9)); err != nil {
		t.Fatal(err)
	}
	if k9 {
		t.Error("the redirect target's rows replaced the column")
	}
}

// A selection's Copy whose export redirects copies nothing and says the
// copy failed: the redirect target's HTML never reaches the clipboard.
func TestE2E_SelectionCopyRedirectedFails(t *testing.T) {
	body := `<div data-hui-selection><label><input type="checkbox" name="ids" value="r2" checked> two</label>` +
		`<span data-hui-selection-copy="/export.csv" data-hui-selection-copied="Copied {{n}} rows" ` +
		`data-hui-selection-copy-failed="The rows could not be copied."><button id="copy" type="button">Copy</button></span></div>`
	b := startBehaviorServer(t, body, raLogin, func(mux *http.ServeMux) {
		mux.HandleFunc("/export.csv", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules || {})['headless-selection-copy']`) {
		t.Fatal("headless-selection-copy never loaded")
	}
	var clip, toast string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			window.__clip = ''; window.__toasts = [];
			Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {
				write: (items) => items[0].getType('text/plain').then((b) => b.text()).then((t) => { window.__clip = t; }),
				writeText: (t) => { window.__clip = t; return Promise.resolve(); },
			}});
			window.__gofastr.toast = (cfg) => { window.__toasts.push(cfg); return 't'; };
			document.getElementById('copy').click();
		})()`, nil),
		chromedp.Poll(`window.__toasts.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__clip`, &clip),
		chromedp.Evaluate(`window.__toasts[0].title`, &toast),
	); err != nil {
		t.Fatal(err)
	}
	if clip != "" {
		t.Errorf("the clipboard holds %d bytes of the redirect target", len(clip))
	}
	if toast != "The rows could not be copied." {
		t.Errorf("toast %q, want the copy-failed words", toast)
	}
}
