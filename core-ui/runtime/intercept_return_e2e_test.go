package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A save inside an intercept layer that names where to go next returns
// to that place when it is part of the stack: the record a create opened
// over, the list under the stack, or the pane itself. The rig's create
// screen opens over the list and over a record (the manifest's "also"),
// and its form navigates to the path the server saw as the origin, the
// way entityui's create form does.
const interceptReturnRoutes = `[{"path":"/list"},` +
	`{"path":"/rec/:id","intercept":{"from":"/list","also":["/rec/:id"],"as":"drawer"}},` +
	`{"path":"/new","intercept":{"from":"/list","also":["/rec/:id"],"as":"drawer"}},` +
	`{"path":"/away"}]`

func startInterceptReturnServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var saves atomic.Int64
	listBody := func() string {
		return fmt.Sprintf(`<p id="list-count">LIST %d</p><a id="to-a" href="/rec/a">rec a</a>`+
			`<a id="list-new" href="/new">new</a><a id="list-new-away" href="/new?to=away">new elsewhere</a>`, saves.Load())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		saves.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch {
		case r.Header.Get("X-Gofastr-Intercept") != "":
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			switch r.URL.Path {
			case "/rec/a":
				fmt.Fprintf(w, `<div id="rec-a"><p id="rec-count">REC %d</p><a id="rec-new" href="/new">add</a>`+
					`<a id="rec-to-b" href="/rec/b">rec b</a><a id="rec-query" href="/rec/a?view=other">other view</a>`+
					`<form id="rec-form" data-cui-rpc="/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/rec/a">`+
					`<button id="rec-save" type="submit">Save</button></form></div>`, saves.Load())
			case "/rec/b":
				fmt.Fprintf(w, `<div id="rec-b"><p id="rec-b-count">REC %d</p>`+
					`<form data-cui-rpc="/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/rec/b">`+
					`<button id="rec-b-save" type="submit">Save</button></form></div>`, saves.Load())
			case "/new":
				dest, _, _ := strings.Cut(r.Header.Get("X-Gofastr-From"), "?")
				if r.URL.Query().Get("to") == "away" {
					dest = "/away"
				}
				fmt.Fprintf(w, `<div id="new-pane"><form id="new-form" data-cui-rpc="/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="%s">`+
					`<button id="new-save" type="submit">Create</button></form></div>`, dest)
			}
		case r.Header.Get("X-Gofastr-Navigate") == "1":
			w.Header().Set("X-Gofastr-Partial", "true")
			if r.URL.Path == "/list" {
				fmt.Fprint(w, listBody())
				return
			}
			fmt.Fprintf(w, `<p id="away">AWAY %s</p>`, r.URL.Path)
		default:
			fmt.Fprint(w, `<!doctype html><html><head><title>return</title>`+
				`<script type="application/json" id="gofastr-routes">`+interceptReturnRoutes+`</script>`+
				`</head><body><main id="list-main">`+listBody()+`</main>`+
				`<script src="/__gofastr/runtime.js"></script></body></html>`)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func returnRig(t *testing.T) context.Context {
	t.Helper()
	srv := startInterceptReturnServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	return ctx
}

func returnClick(t *testing.T, ctx context.Context, id, waitFor string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Click("#"+id, chromedp.ByID)); err != nil {
		t.Fatalf("click #%s: %v", id, err)
	}
	if !interceptWait(ctx, waitFor) {
		snap := readStack(ctx)
		t.Fatalf("after #%s, never %s (url %s, %d layers)", id, waitFor, snap.URL, len(snap.Layers))
	}
}

func historyLen(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.length`, &n)); err != nil {
		t.Fatal(err)
	}
	return n
}

const textIs = `(document.getElementById('%s') || {}).textContent === '%s'`

func TestCreateOverRecordReturnsToIt(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	h := historyLen(t, ctx)
	returnClick(t, ctx, "rec-new", `!!document.getElementById('new-pane')`)
	returnClick(t, ctx, "new-save", `!document.getElementById('new-pane') && `+fmt.Sprintf(textIs, "rec-count", "REC 1"))
	snap := readStack(ctx)
	if len(snap.Layers) != 1 || snap.URL != "/rec/a" || snap.Main != "list-main" {
		t.Fatalf("want the record alone over the list at /rec/a, got %d layers at %s over %s", len(snap.Layers), snap.URL, snap.Main)
	}
	// The create's entry was consumed, not added to: one Back from the
	// record lands on the list.
	if got := historyLen(t, ctx); got != h+1 {
		t.Errorf("history length = %d, want %d (the create's entry, now forward)", got, h+1)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `location.pathname === '/list' && !document.getElementById('rec-a')`) {
		t.Fatalf("Back from the record did not land on the list: %+v", readStack(ctx))
	}
}

func TestCreateOverListReturnsToIt(t *testing.T) {
	ctx := returnRig(t)
	h := historyLen(t, ctx)
	returnClick(t, ctx, "list-new", `!!document.getElementById('new-pane')`)
	returnClick(t, ctx, "new-save", `!document.getElementById('new-pane') && `+fmt.Sprintf(textIs, "list-count", "LIST 1"))
	snap := readStack(ctx)
	if len(snap.Layers) != 0 || snap.URL != "/list" {
		t.Fatalf("want the list with no layers, got %d layers at %s", len(snap.Layers), snap.URL)
	}
	if got := historyLen(t, ctx); got != h+1 {
		t.Errorf("history length = %d, want %d: the return pushed an entry", got, h+1)
	}
}

func TestSaveInPaneKeepsIt(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	h := historyLen(t, ctx)
	returnClick(t, ctx, "rec-save", fmt.Sprintf(textIs, "rec-count", "REC 1"))
	snap := readStack(ctx)
	if len(snap.Layers) != 1 || snap.URL != "/rec/a" || snap.Main != "list-main" {
		t.Fatalf("want the record pane kept at /rec/a, got %d layers at %s over %s", len(snap.Layers), snap.URL, snap.Main)
	}
	if got := historyLen(t, ctx); got != h {
		t.Errorf("history length = %d, want %d", got, h)
	}
}

// A save that keeps its pane changed what the page under the stack
// shows: closing the pane, by Escape or by Back, re-renders the list.
func TestSaveInPaneRefreshesTheListOnClose(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close chromedp.Action
	}{
		{"escape", chromedp.KeyEvent(kb.Escape)},
		{"back", chromedp.Evaluate(`history.back()`, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := returnRig(t)
			returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
			returnClick(t, ctx, "rec-save", fmt.Sprintf(textIs, "rec-count", "REC 1"))
			if err := chromedp.Run(ctx, tc.close); err != nil {
				t.Fatal(err)
			}
			if !interceptWait(ctx, `!document.getElementById('rec-a') && `+fmt.Sprintf(textIs, "list-count", "LIST 1")) {
				var list string
				_ = chromedp.Run(ctx, chromedp.Text(`#list-count`, &list, chromedp.ByID))
				t.Fatalf("after the save and close the list reads %q, want LIST 1 (%+v)", list, readStack(ctx))
			}
		})
	}
}

// The same holds one layer up: a save in a record stacked over another
// re-renders the lower record when the upper one closes, and the list
// when the lower one closes.
func TestSaveInUpperPaneRefreshesTheLayersUnder(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
	returnClick(t, ctx, "rec-b-save", fmt.Sprintf(textIs, "rec-b-count", "REC 1"))
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!document.getElementById('rec-b') && `+fmt.Sprintf(textIs, "rec-count", "REC 1")) {
		t.Fatalf("closing the upper record left the lower one stale: %+v", readStack(ctx))
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!document.getElementById('rec-a') && `+fmt.Sprintf(textIs, "list-count", "LIST 1")) {
		t.Fatalf("closing the lower record left the list stale: %+v", readStack(ctx))
	}
}

func TestUncoveredStalePageSurvivesStartingANewStack(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	returnClick(t, ctx, "rec-save", fmt.Sprintf(textIs, "rec-count", "REC 1"))
	ngEval(t, ctx, `window.__pageGuardCalls = 0;
		__gofastr._leaveGuard = { ok: (scope) => scope !== document || ++window.__pageGuardCalls > 1 };`, nil)
	ngRun(t, ctx, chromedp.KeyEvent(kb.Escape))
	if !interceptWait(ctx, `location.pathname === '/list' && window.__pageGuardCalls === 1 && `+fmt.Sprintf(textIs, "list-count", "LIST 0")) {
		t.Fatalf("the page guard did not keep the list unchanged: %+v", readStack(ctx))
	}

	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	ngRun(t, ctx, chromedp.KeyEvent(kb.Escape))
	if !interceptWait(ctx, `location.pathname === '/list' && window.__pageGuardCalls === 2 && `+fmt.Sprintf(textIs, "list-count", "LIST 1")) {
		t.Fatalf("closing the new stack did not refresh the still-stale list: %+v", readStack(ctx))
	}
}

func TestUncoveredStaleLayerRetriesSupersededRefresh(t *testing.T) {
	for _, history := range []bool{false, true} {
		name := "escape"
		if history {
			name = "history-refetch"
		}
		t.Run(name, func(t *testing.T) {
			ctx := returnRig(t)
			returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
			if history {
				returnClick(t, ctx, "rec-query", `location.search === '?view=other'`)
			}
			returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
			returnClick(t, ctx, "rec-b-save", fmt.Sprintf(textIs, "rec-b-count", "REC 1"))
			ngEval(t, ctx, `{
				const fetch = __gofastr.fetch;
				window.__lowerFetches = 0;
				__gofastr.fetch = (url, opts) => {
					if (String(url).endsWith('/rec/a') && opts?.headers?.['X-Gofastr-Intercept'] && ++window.__lowerFetches === 1) {
						return new Promise(resolve => { window.__releaseLower = () => resolve(fetch(url, opts)); });
					}
					return fetch(url, opts);
				};
			}`, nil)
			if history {
				ngEval(t, ctx, `history.go(-2)`, nil)
			} else {
				ngRun(t, ctx, chromedp.KeyEvent(kb.Escape))
			}
			if !interceptWait(ctx, `!document.getElementById('rec-b') && window.__lowerFetches === 1 && location.pathname === '/rec/a' && !location.search`) {
				t.Fatalf("expected one pending refresh after uncovering the lower record: %+v", readStack(ctx))
			}

			returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
			ngRun(t, ctx, chromedp.KeyEvent(kb.Escape))
			if !interceptWait(ctx, `window.__lowerFetches === 2 && `+fmt.Sprintf(textIs, "rec-count", "REC 1")) {
				t.Fatalf("uncover did not retry the interrupted refresh: %+v", readStack(ctx))
			}
			ngEval(t, ctx, `window.__releaseLower()`, nil)
		})
	}
}

func TestSaveElsewhereLeavesTheStack(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "list-new-away", `!!document.getElementById('new-pane')`)
	returnClick(t, ctx, "new-save", `location.pathname === '/away' && !!document.getElementById('away')`)
	if snap := readStack(ctx); len(snap.Layers) != 0 {
		t.Fatalf("a save navigating out of the stack left %d layers", len(snap.Layers))
	}
}

// Re-rendering an uncovered record discards its edits, so the leave
// guard asks first; declined, the record keeps what it shows.
func TestUncoveredStaleLayerAsksTheLeaveGuard(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
	returnClick(t, ctx, "rec-b-save", fmt.Sprintf(textIs, "rec-b-count", "REC 1"))
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__asked = 0; __gofastr._leaveGuard = { ok: (s) => !(s.querySelector && s.querySelector('#rec-a') && ++window.__asked) }`, nil),
		chromedp.KeyEvent(kb.Escape),
	); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!document.getElementById('rec-b') && window.__asked === 1`) {
		t.Fatalf("closing the upper record never asked the guard about the lower one: %+v", readStack(ctx))
	}
	time.Sleep(300 * time.Millisecond)
	var count string
	if err := chromedp.Run(ctx, chromedp.Text(`#rec-count`, &count, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if count != "REC 0" {
		t.Fatalf("a declined guard still re-rendered the lower record: %q", count)
	}
}

func TestUncoveredStaleContentAsksNavigationGuards(t *testing.T) {
	for _, tc := range []struct {
		name, root, closed, count, before, after string
		allow                                    bool
	}{
		{"drawer/escape", "rec-a", "rec-b", "rec-count", "REC 0", "REC 1", false},
		{"page/back", "list-main", "rec-a", "list-count", "LIST 0", "LIST 1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := returnRig(t)
			returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
			if tc.root == "rec-a" {
				returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
				returnClick(t, ctx, "rec-b-save", fmt.Sprintf(textIs, "rec-b-count", "REC 1"))
			} else {
				returnClick(t, ctx, "rec-save", fmt.Sprintf(textIs, "rec-count", "REC 1"))
			}
			ngEval(t, ctx, fmt.Sprintf(`(async () => {
				await __gofastr.loadModule('navguard');
				const draft = document.createElement('input');
				draft.id = 'unsaved-draft';
				draft.value = 'unsaved';
				document.getElementById(%q).appendChild(draft);
				window.__blocked = false;
				addEventListener('gofastr:navigateblocked', () => { window.__blocked = true; });
				__gofastr.guardNavigation((nav) => {
					if (nav.affects(draft)) return new Promise((resolve) => { window.__answer = resolve; });
				});
			})()`, tc.root), nil)
			if tc.root == "rec-a" {
				ngRun(t, ctx, chromedp.KeyEvent(kb.Escape))
			} else {
				ngEval(t, ctx, `history.back()`, nil)
			}
			countIs := func(text string) string {
				return fmt.Sprintf(`document.getElementById(%q)?.textContent === %q`, tc.count, text)
			}
			preserved := countIs(tc.before) + ` && document.getElementById('unsaved-draft')?.value === 'unsaved'`
			if !interceptWait(ctx, fmt.Sprintf(`!document.getElementById(%q) && typeof window.__answer === 'function' && `, tc.closed)+preserved) {
				t.Fatalf("closing did not wait for a navigation guard on the uncovered draft: %+v", readStack(ctx))
			}
			ngEval(t, ctx, fmt.Sprintf(`window.__answer(%t)`, tc.allow), nil)
			want := `window.__blocked && ` + preserved
			if tc.allow {
				want = countIs(tc.after) + ` && !document.getElementById('unsaved-draft')`
			}
			if !interceptWait(ctx, want) {
				t.Fatalf("refresh did not honor the navigation guard (allow=%t): %+v", tc.allow, readStack(ctx))
			}
		})
	}
}

// An uncovered record whose refetch fails falls back to a plain
// navigation to it rather than going on showing what it showed.
func TestUncoveredStaleLayerFallsBackWhenItsRefetchFails(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
	returnClick(t, ctx, "rec-to-b", `!!document.getElementById('rec-b')`)
	returnClick(t, ctx, "rec-b-save", fmt.Sprintf(textIs, "rec-b-count", "REC 1"))
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`{ const f = __gofastr.fetch; let once = true;
			__gofastr.fetch = (u, o) => once && String(u).endsWith('/rec/a') && o?.headers?.['X-Gofastr-Intercept'] ? (once = false, Promise.reject(new Error('down'))) : f(u, o); }`, nil),
		chromedp.KeyEvent(kb.Escape),
	); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!document.getElementById('rec-a') && `+fmt.Sprintf(textIs, "away", "AWAY /rec/a")) {
		t.Fatalf("a failed refetch left the lower record stale: %+v", readStack(ctx))
	}
}
