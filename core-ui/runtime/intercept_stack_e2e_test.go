package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Intercept stacking (admin rebuild P0, worker C): an intercepted link
// clicked inside an open intercept pane opens the target as a NEW layer
// over the current one, at most four deep, with one history entry per
// open. The rig is a hand-rolled server in the beforenavigate shape: the
// routes manifest declares a chain five intercepts deep, so a click
// inside each layer can open the next, and every overlay answer carries
// X-Gofastr-Overlay the way a real host does for a declared origin.
//
//		/list ── /rec/{id} (drawer, from /list)
//		      └─ /rec/{id} ── /rel/{id} (drawer, from /rec/{id})
//		                    └─ /rel/{id} ── /deep/{id} (drawer, from /rel/{id})
//	                                   └─ /deep/{id} ── /deepest/{id} (drawer, from /deep/{id})
//	                                                    └─ /deepest/{id} ── /beyond/{id} (drawer, from /deepest/{id})
const interceptStackRoutes = `[{"path":"/list"},` +
	`{"path":"/rec/:id","intercept":{"from":"/list","as":"drawer"}},` +
	`{"path":"/rel/:id","intercept":{"from":"/rec/:id","as":"drawer"}},` +
	`{"path":"/deep/:id","intercept":{"from":"/rel/:id","as":"drawer"}},` +
	`{"path":"/deepest/:id","intercept":{"from":"/deep/:id","as":"drawer"}},` +
	`{"path":"/beyond/:id","intercept":{"from":"/deepest/:id","as":"drawer"}},` +
	`{"path":"/plain"}]`

// interceptOverlayBody is the overlay variant each intercepted path
// answers with. The record layer carries a note input (unsaved edits
// must survive a layer opening above it and closing again), a query link
// (a list inside a drawer keeps the drawer when its query changes) and
// a close control.
func interceptOverlayBody(path, query string) string {
	if path == "/rec/a" && strings.HasPrefix(query, "page=") {
		n := strings.TrimPrefix(query, "page=")
		return `<div id="rec-a-page-` + n + `"><p>PAGE ` + n + `</p>` + interceptPager + `</div>`
	}
	if path == "/rec/a" && query == "sort=name" {
		return `<div id="rec-a-sorted"><p>REC-A-SORTED</p>` +
			`<a id="sorted-to-rel" href="/rel/r1">related</a>` +
			`<a id="sort-back" href="/rec/a">natural order</a></div>`
	}
	switch path {
	case "/rec/a":
		return `<div id="rec-a"><p>REC-A</p><span data-cui-comp="intercept-probe"></span>` +
			`<input id="rec-note" value="" aria-label="note">` +
			`<a id="a-to-rel" href="/rel/r1">related</a>` +
			`<a id="a-sort" href="/rec/a?sort=name">sort</a>` + interceptPager +
			`<button id="a-close" type="button" data-cui-intercept-close>Close</button></div>`
	case "/rel/r1":
		return `<div id="rel-r1"><p>REL-R1</p>` +
			`<a id="r-to-deep" href="/deep/d1">deep</a>` +
			`<form id="rel-form" data-cui-rpc="/save" data-cui-rpc-method="POST">` +
			`<div data-hui-field><input id="f-title" name="title" value="x" aria-label="title"></div>` +
			`<button id="save" type="submit">Save</button></form></div>`
	case "/deep/d1":
		return `<div id="deep-d1"><p>DEEP-D1</p>` +
			`<a id="d-to-deepest" href="/deepest/x1">deepest</a></div>`
	case "/deepest/x1":
		return `<div id="deepest-x1"><p>DEEPEST-X1</p>` +
			`<a id="x-to-beyond" href="/beyond/b1">beyond</a></div>`
	}
	return `<div id="overlay-` + strings.Trim(path, "/") + `"></div>`
}

// interceptPager is the record pane's own pager: query-only links that
// re-render the pane in place. page=slow answers after a delay, so a
// test can close the pane while that refetch is in flight.
const interceptPager = `<a id="pg-2" href="/rec/a?page=2">2</a>` +
	`<a id="pg-3" href="/rec/a?page=3">3</a>` +
	`<a id="pg-4" href="/rec/a?page=4">4</a>` +
	`<a id="pg-slow" href="/rec/a?page=slow">slow</a>`

// interceptOverlayAs is the presentation the server picks per path: the
// deep record is a sheet, so a stack mixes them.
func interceptOverlayAs(path string) string {
	if path == "/deep/d1" {
		return "sheet"
	}
	return "drawer"
}

// interceptFullPage is the canonical full-page render: the routes
// manifest plus a main cell whose text names the URL. /list carries the
// record link the tests click.
func interceptFullPage(key string) string {
	id := "full-main"
	body := "FULL " + key
	if key == "/list" {
		id = "list-main"
		body = `<a id="to-a" href="/rec/a">rec a</a>LIST`
	}
	return `<!doctype html><html><head><title>stack</title>` +
		`<script type="application/json" id="gofastr-routes">` + interceptStackRoutes + `</script>` +
		`<script>window.__gofastr_catalog={"intercept-probe":{stylePath:"/css/intercept-probe.css"}};</script>` +
		`</head><body><main id="` + id + `">` + body + `</main>` +
		`<span id="ready">ready</span>` +
		`<script src="/__gofastr/runtime.js"></script></body></html>`
}

// interceptStackServer records every overlay fetch's X-Gofastr-From so
// tests can pin which origin each request named.
type interceptStackServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	froms []string
}

func (s *interceptStackServer) overlayFroms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.froms))
	copy(out, s.froms)
	return out
}

func startInterceptStackServer(t *testing.T) *interceptStackServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &interceptStackServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/css/intercept-probe.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `[data-cui-comp="intercept-probe"]{display:block;inline-size:7px}`)
	})
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":"Check the fields","fields":{"title":["Too short"]}}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		switch {
		case r.Header.Get("X-Gofastr-Intercept") != "":
			s.mu.Lock()
			s.froms = append(s.froms, r.Header.Get("X-Gofastr-From"))
			s.mu.Unlock()
			if r.URL.RawQuery == "page=slow" {
				time.Sleep(800 * time.Millisecond)
			}
			w.Header().Set("X-Gofastr-Overlay", interceptOverlayAs(r.URL.Path))
			fmt.Fprint(w, interceptOverlayBody(r.URL.Path, r.URL.RawQuery))
		case r.Header.Get("X-Gofastr-Navigate") == "1":
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprintf(w, `<p id="plain-screen">plain %s</p>`, key)
		default:
			fmt.Fprint(w, interceptFullPage(key))
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// interceptWait polls a boolean expression until true or the budget
// runs out. Layer opens and closes are async (a fetch, a history move),
// so a fixed sleep flakes; a poll waits for exactly the observed state.
func interceptWait(ctx context.Context, js string) bool {
	for range 50 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// stackStateJS snapshots what the tests assert on: the URL, every layer
// child's landmark ids, its inert/aria-hidden marks, and the page under
// the stack.
const stackStateJS = `(function () {
  const c = document.getElementById('cui-intercept');
  const kids = c ? Array.from(c.children) : [];
  return JSON.stringify({
    url: location.pathname + location.search,
    layers: kids.map(function (k) {
      return {
        ids: Array.from(k.querySelectorAll('[id]')).map(function (e) { return e.id; }),
        inert: k.hasAttribute('inert'),
        ariaHidden: k.getAttribute('aria-hidden'),
        as: k.getAttribute('data-cui-intercept-as') || '',
      };
    }),
    main: document.querySelector('main') ? document.querySelector('main').id : '',
    focus: document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : '',
  });
})()`

type stackLayer struct {
	IDs        []string `json:"ids"`
	Inert      bool     `json:"inert"`
	AriaHidden string   `json:"ariaHidden"`
	As         string   `json:"as"`
}

type stackSnapshot struct {
	URL    string       `json:"url"`
	Layers []stackLayer `json:"layers"`
	Main   string       `json:"main"`
	Focus  string       `json:"focus"`
}

func readStack(ctx context.Context) stackSnapshot {
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(stackStateJS, &raw)); err != nil {
		return stackSnapshot{}
	}
	var snap stackSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return stackSnapshot{}
	}
	return snap
}

// stackStep clicks the chain's i-th opener (0-based): step 0 opens the
// record from the list, step 1 the related record from inside the
// record, step 2 the deep record from inside that one.
func stackStep(t *testing.T, ctx context.Context, i int) {
	t.Helper()
	steps := []string{"to-a", "a-to-rel", "r-to-deep", "d-to-deepest"}
	if !interceptWait(ctx, `!!document.getElementById('`+steps[i]+`')`) {
		t.Fatalf("step %d: trigger #%s never appeared", i+1, steps[i])
	}
	if err := chromedp.Run(ctx, chromedp.Click("#"+steps[i], chromedp.ByID)); err != nil {
		t.Fatalf("click #%s: %v", steps[i], err)
	}
}

// stackTo drives the rig to n layers (1-based).
func stackTo(t *testing.T, ctx context.Context, n int) {
	t.Helper()
	for i := range n {
		stackStep(t, ctx, i)
	}
}

func idListHas(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestInterceptStacksFourLayersThenRefuses(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Four layers deep. Lower layers keep their DOM (unsaved edits in
	// the record's note input survive) and go inert + aria-hidden; only
	// the top layer takes focus.
	stackTo(t, ctx, 1)
	if !interceptWait(ctx, `!!document.getElementById('rec-a')`) {
		t.Fatal("layer 1 never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.SetValue(`#rec-note`, "unsaved", chromedp.ByID)); err != nil {
		t.Fatalf("set note: %v", err)
	}
	want := []string{"rec-a", "rel-r1", "deep-d1", "deepest-x1"}
	for i := 1; i < len(want); i++ {
		stackStep(t, ctx, i)
		if !interceptWait(ctx, `!!document.getElementById('`+want[i]+`')`) {
			t.Fatalf("layer %d never mounted", i+1)
		}
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 4 {
		t.Fatalf("want 4 layers, got %d (%s)", len(snap.Layers), snap.URL)
	}
	for i, id := range want {
		if !idListHas(snap.Layers[i].IDs, id) {
			t.Errorf("layer %d holds %v, want %s", i+1, snap.Layers[i].IDs, id)
		}
		if i < 3 && !snap.Layers[i].Inert {
			t.Errorf("layer %d must be inert under the open top layer", i+1)
		}
		if i < 3 && snap.Layers[i].AriaHidden != "true" {
			t.Errorf("layer %d must carry aria-hidden under the open top layer", i+1)
		}
	}
	if snap.Layers[3].Inert {
		t.Error("the top layer must never be inert")
	}
	if snap.Main != "list-main" {
		t.Errorf("page under the stack replaced: main = %q", snap.Main)
	}
	var note string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('rec-note').value`, &note)); err != nil || note != "unsaved" {
		t.Errorf("lower layer lost its DOM edits: note = %q (err %v)", note, err)
	}
	if snap.Focus == "BODY" || snap.Focus == "" {
		t.Error("the top layer must take focus")
	}

	// A fifth open is refused: a toast says why, nothing is fetched, and
	// the stack, the URL and every layer's content stay as they were.
	before := len(s.overlayFroms())
	if err := chromedp.Run(ctx, chromedp.Click("#x-to-beyond", chromedp.ByID)); err != nil {
		t.Fatalf("click beyond: %v", err)
	}
	if !interceptWait(ctx, `/panels is the limit/.test(document.body.textContent)`) {
		t.Fatal("the refused fifth open showed no toast")
	}
	snap = readStack(ctx)
	if len(snap.Layers) != 4 || snap.URL != "/deepest/x1" {
		t.Fatalf("a refused open changed the stack: %d layers at %q", len(snap.Layers), snap.URL)
	}
	if !idListHas(snap.Layers[3].IDs, "deepest-x1") {
		t.Errorf("a refused open replaced the top layer: %v", snap.Layers[3].IDs)
	}
	if got := len(s.overlayFroms()); got != before {
		t.Errorf("a refused open fetched an overlay: %d requests, want %d", got, before)
	}
}

func TestInterceptBackClosesOnlyTopLayer(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 2)
	if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
		t.Fatal("layer 2 never mounted")
	}
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#rec-note`, "keep me", chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rec-a') && !document.querySelector('#cui-intercept > :nth-child(1)').hasAttribute('inert')`) {
		t.Fatal("Back never closed the top layer and un-inerted the layer below")
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 1 {
		t.Fatalf("Back must close exactly the top layer, %d remain", len(snap.Layers))
	}
	if !idListHas(snap.Layers[0].IDs, "rec-a") {
		t.Errorf("remaining layer holds %v, want the record", snap.Layers[0].IDs)
	}
	if snap.URL != "/rec/a" {
		t.Errorf("URL after Back = %q, want /rec/a", snap.URL)
	}
	if snap.Main != "list-main" {
		t.Errorf("Back through a stack must not refetch the page underneath, main = %q", snap.Main)
	}
	if snap.Focus != "a-to-rel" {
		t.Errorf("focus after Back = %q, want the control that opened the closed layer (a-to-rel)", snap.Focus)
	}
	var note string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('rec-note').value`, &note)); err != nil || note != "keep me" {
		t.Errorf("lower layer lost its DOM edits on close: note = %q (err %v)", note, err)
	}
}

func TestInterceptEscClosesOnlyTopLayer(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 2)
	if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
		t.Fatal("layer 2 never mounted")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil),
	); err != nil {
		t.Fatalf("esc: %v", err)
	}
	if !interceptWait(ctx, `document.querySelectorAll('#cui-intercept > *').length === 1`) {
		t.Fatal("Escape never closed the top layer")
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 1 || !idListHas(snap.Layers[0].IDs, "rec-a") {
		t.Fatalf("Escape must close only the top layer, snapshot %+v", snap)
	}
	if snap.URL != "/rec/a" {
		t.Errorf("URL after Escape = %q, want /rec/a (Esc routes through history)", snap.URL)
	}
	if snap.Focus != "a-to-rel" {
		t.Errorf("focus after Escape = %q, want a-to-rel", snap.Focus)
	}
}

func TestInterceptCloseButtonClosesLayer(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
		chromedp.Click("#to-a", chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('a-close')`) {
		t.Fatal("layer 1 never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Click("#a-close", chromedp.ByID)); err != nil {
		t.Fatalf("click close: %v", err)
	}
	if !interceptWait(ctx, `!document.getElementById('cui-intercept')`) {
		t.Fatal("the close control never dropped the overlay")
	}
	snap := readStack(ctx)
	if snap.URL != "/list" {
		t.Errorf("URL after close = %q, want /list (close routes through history)", snap.URL)
	}
	if snap.Focus != "to-a" {
		t.Errorf("focus after close = %q, want the trigger that opened the layer", snap.Focus)
	}
}

func TestInterceptColdLoadRendersFullPage(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 2)
	if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
		t.Fatal("layer 2 never mounted")
	}
	// A refresh / cold load of the top URL renders it as a full page: no
	// X-Gofastr-Intercept header, no overlay, the canonical render.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/rel/r1"),
		chromedp.WaitVisible(`#full-main`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 0 {
		t.Errorf("a cold load must render the record full page, %d layers mounted", len(snap.Layers))
	}
	var main string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('main').textContent`, &main)); err != nil {
		t.Fatalf("read main: %v", err)
	}
	if main != "FULL /rel/r1" {
		t.Errorf("cold load main = %q, want the full page render of the record", main)
	}
}

func TestInterceptForwardAfterBackLoadsPage(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 2)
	if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
		t.Fatal("layer 2 never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatalf("back: %v", err)
	}
	if !interceptWait(ctx, `document.querySelectorAll('#cui-intercept > *').length === 1`) {
		t.Fatal("Back never closed the top layer")
	}
	// Forward today does what the single-layer intercept does: no layer
	// re-opens; the URL renders as a plain page in the content cell.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.forward()`, nil)); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if !interceptWait(ctx, `location.pathname === '/rel/r1' && !document.getElementById('cui-intercept')`) {
		t.Fatal("Forward never landed on the record as a plain page")
	}
	var main string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(document.querySelector('main p')||{}).textContent || ''`, &main)); err != nil {
		t.Fatalf("read main: %v", err)
	}
	if !strings.Contains(main, "/rel/r1") {
		t.Errorf("Forward must load the record as a page, main = %q", main)
	}
}

func TestInterceptQueryLinkStaysInPane(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
		chromedp.Click("#to-a", chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('a-sort')`) {
		t.Fatal("layer 1 never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Click("#a-sort", chromedp.ByID)); err != nil {
		t.Fatalf("click sort: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rec-a-sorted')`) {
		t.Fatal("the query link never re-rendered inside the pane")
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 1 {
		t.Fatalf("a query-only link must keep the pane, %d layers", len(snap.Layers))
	}
	if !idListHas(snap.Layers[0].IDs, "rec-a-sorted") {
		t.Errorf("pane holds %v, want the sorted render", snap.Layers[0].IDs)
	}
	if snap.URL != "/rec/a?sort=name" {
		t.Errorf("pane URL = %q, want /rec/a?sort=name", snap.URL)
	}
	if snap.Main != "list-main" {
		t.Errorf("a query-only link inside a pane must not navigate the whole page, main = %q", snap.Main)
	}
	// The overlay refetch names the page under the stack as its origin,
	// the same From that opened the layer.
	froms := s.overlayFroms()
	if len(froms) < 2 || froms[len(froms)-1] != "/list" {
		t.Errorf("query refetch X-Gofastr-From = %v, want the last request to name /list", froms)
	}
	// Back walks the pane's own query states, still inside the pane.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatalf("back: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rec-a') && !!document.getElementById('cui-intercept')`) {
		t.Fatal("Back never restored the pane's previous query state inside the pane")
	}
	snap = readStack(ctx)
	if snap.URL != "/rec/a" || len(snap.Layers) != 1 {
		t.Errorf("after Back through a query state: url %q, %d layers; want /rec/a, 1 layer", snap.URL, len(snap.Layers))
	}
}

func TestInterceptForm422ShowsErrorsInLayer(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 2)
	if !interceptWait(ctx, `!!document.getElementById('save')`) {
		t.Fatal("layer 2 (with the save form) never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Click("#save", chromedp.ByID)); err != nil {
		t.Fatalf("click save: %v", err)
	}
	if !interceptWait(ctx, `(function () {
		const layer = document.querySelector('#cui-intercept > :nth-child(2)');
		const p = layer && layer.querySelector('[data-hui-field-error]');
		return !!(p && p.textContent.indexOf('Too short') !== -1);
	})()`) {
		t.Fatal("the 422 envelope never landed beside the field inside the pane layer")
	}
	var invalid string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('f-title').getAttribute('aria-invalid')`, &invalid)); err != nil {
		t.Fatalf("read aria-invalid: %v", err)
	}
	if invalid != "true" {
		t.Errorf("aria-invalid = %q, want true on the refused field", invalid)
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 2 {
		t.Errorf("a refused save must keep the stack, %d layers", len(snap.Layers))
	}
}

// Safari never focuses a clicked link, so the module must not learn which
// link was clicked from document.activeElement: a click inside the pane
// that leaves focus on <body> still opens the next layer over it.
func TestInterceptStacksWithoutLinkFocus(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 1)
	if !interceptWait(ctx, `!!document.getElementById('a-to-rel')`) {
		t.Fatal("layer 1 never mounted")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement.blur(); document.getElementById('a-to-rel').click()`, nil)); err != nil {
		t.Fatalf("unfocused click: %v", err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
		t.Fatal("layer 2 never mounted")
	}
	snap := readStack(ctx)
	if len(snap.Layers) != 2 || snap.Main != "list-main" {
		t.Fatalf("want 2 layers over the list, got %d layers, main %q (%s)", len(snap.Layers), snap.Main, snap.URL)
	}
}

// interceptOpenRecord loads the list and opens the record pane over it.
func interceptOpenRecord(t *testing.T) (*interceptStackServer, context.Context) {
	t.Helper()
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 1)
	if !interceptWait(ctx, `!!document.getElementById('pg-2')`) {
		t.Fatal("the record pane never mounted")
	}
	return s, ctx
}

// interceptPage clicks the pane's pager link to page n and waits for
// that render.
func interceptPage(t *testing.T, ctx context.Context, n string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Click("#pg-"+n, chromedp.ByID)); err != nil {
		t.Fatalf("click page %s: %v", n, err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rec-a-page-`+n+`')`) {
		t.Fatalf("page %s never rendered in the pane", n)
	}
}

// interceptHistory moves history by delta and waits for the URL.
func interceptHistory(t *testing.T, ctx context.Context, delta int, url string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`history.go(%d)`, delta), nil)); err != nil {
		t.Fatalf("go(%d): %v", delta, err)
	}
	if !interceptWait(ctx, `location.pathname + location.search === '`+url+`'`) {
		t.Fatalf("go(%d) never reached %s", delta, url)
	}
}

// interceptEscClosesToList presses Esc and asserts the stack closed onto
// the list it opened from: the URL, no overlay, the list still mounted.
func interceptEscClosesToList(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil)); err != nil {
		t.Fatalf("esc: %v", err)
	}
	if !interceptWait(ctx, `location.pathname === '/list' && !document.getElementById('cui-intercept')`) {
		snap := readStack(ctx)
		t.Fatalf("Esc must close the pane onto /list; at %s with %d layers, main %q", snap.URL, len(snap.Layers), snap.Main)
	}
	time.Sleep(300 * time.Millisecond)
	if snap := readStack(ctx); snap.URL != "/list" || len(snap.Layers) != 0 || snap.Main != "list-main" {
		t.Errorf("after Esc: at %s with %d layers, main %q; want /list, 0 layers, the list", snap.URL, len(snap.Layers), snap.Main)
	}
}

// A refetch still in flight when its pane closes is dropped: it must not
// bring the overlay back or push the closed pane's URL.
func TestInterceptCloseDropsInFlightQuery(t *testing.T) {
	_, ctx := interceptOpenRecord(t)
	if err := chromedp.Run(ctx, chromedp.Click("#pg-slow", chromedp.ByID)); err != nil {
		t.Fatalf("click slow page: %v", err)
	}
	interceptEscClosesToList(t, ctx)
	time.Sleep(1200 * time.Millisecond) // past the slow answer
	if snap := readStack(ctx); snap.URL != "/list" || len(snap.Layers) != 0 {
		t.Errorf("the late answer revived the closed pane: at %s with %d layers", snap.URL, len(snap.Layers))
	}
	var host bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.getElementById('cui-intercept')`, &host)); err != nil {
		t.Fatal(err)
	}
	if host {
		t.Error("the late answer re-created the overlay host (an empty scrim over the page)")
	}
}

// A pane visiting one URL twice still knows which entry it is on:
// Forward onto the second visit, then Esc, closes exactly the pane's
// entries.
func TestInterceptPaneRevisitClosesExactly(t *testing.T) {
	_, ctx := interceptOpenRecord(t)
	interceptPage(t, ctx, "2")
	interceptPage(t, ctx, "3")
	interceptPage(t, ctx, "2")
	interceptHistory(t, ctx, -2, "/rec/a?page=2")
	interceptHistory(t, ctx, 1, "/rec/a?page=3")
	interceptHistory(t, ctx, 1, "/rec/a?page=2")
	if !interceptWait(ctx, `!!document.getElementById('rec-a-page-2')`) {
		t.Fatal("Forward never re-rendered page 2 in the pane")
	}
	interceptEscClosesToList(t, ctx)
}

// A query move after Back replaces the entries ahead, as the browser
// does; the pane forgets them, so Esc still closes exactly its own.
func TestInterceptQueryAfterBackDropsAhead(t *testing.T) {
	_, ctx := interceptOpenRecord(t)
	interceptPage(t, ctx, "2")
	interceptPage(t, ctx, "3")
	interceptHistory(t, ctx, -1, "/rec/a?page=2")
	if !interceptWait(ctx, `!!document.getElementById('rec-a-page-2')`) {
		t.Fatal("Back never re-rendered page 2 in the pane")
	}
	interceptPage(t, ctx, "4")
	interceptEscClosesToList(t, ctx)
}

// Back past two layers hands focus to the control that opened the lower
// of them, which is in the layer left showing.
func TestInterceptBackTwoRestoresFocus(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 3)
	if !interceptWait(ctx, `!!document.getElementById('deep-d1')`) {
		t.Fatal("layer 3 never mounted")
	}
	interceptHistory(t, ctx, -2, "/rec/a")
	if !interceptWait(ctx, `document.querySelectorAll('#cui-intercept > *').length === 1`) {
		t.Fatal("Back two never closed two layers")
	}
	if snap := readStack(ctx); snap.Focus != "a-to-rel" {
		t.Errorf("focus = %q, want a-to-rel (the control that opened layer 2)", snap.Focus)
	}
}

// Back from the only layer onto the list hands focus back to the link
// that opened it.
func TestInterceptBackToListRestoresFocus(t *testing.T) {
	_, ctx := interceptOpenRecord(t)
	interceptHistory(t, ctx, -1, "/list")
	if !interceptWait(ctx, `!document.getElementById('cui-intercept')`) {
		t.Fatal("Back never closed the pane")
	}
	if snap := readStack(ctx); snap.Focus != "to-a" {
		t.Errorf("focus = %q, want to-a (the link that opened the pane)", snap.Focus)
	}
}

// Each layer wears the presentation the server chose for it: a sheet
// opened over two drawers leaves the drawers as drawers.
func TestInterceptLayersKeepOwnPresentation(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/list"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	stackTo(t, ctx, 3)
	if !interceptWait(ctx, `!!document.getElementById('deep-d1')`) {
		t.Fatal("layer 3 never mounted")
	}
	snap := readStack(ctx)
	var got []string
	for _, l := range snap.Layers {
		got = append(got, l.As)
	}
	if strings.Join(got, ",") != "drawer,drawer,sheet" {
		t.Errorf("layer presentations = %v, want [drawer drawer sheet]", got)
	}
}
