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
)

// A save inside an intercept layer that names where to go next returns
// to that place when it is part of the stack: the record a create opened
// over, the list under the stack, or the pane itself. The rig's create
// screen opens over the list and over a record (the manifest's "also"),
// and its form navigates to the path the server saw as the origin, the
// way entityui's create form does.
const interceptReturnRoutes = `[{"path":"/list"},` +
	`{"path":"/rec/:id","intercept":{"from":"/list","as":"drawer"}},` +
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
					`<form id="rec-form" data-cui-rpc="/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/rec/a">`+
					`<button id="rec-save" type="submit">Save</button></form></div>`, saves.Load())
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

func TestSaveElsewhereLeavesTheStack(t *testing.T) {
	ctx := returnRig(t)
	returnClick(t, ctx, "list-new-away", `!!document.getElementById('new-pane')`)
	returnClick(t, ctx, "new-save", `location.pathname === '/away' && !!document.getElementById('away')`)
	if snap := readStack(ctx); len(snap.Layers) != 0 {
		t.Fatalf("a save navigating out of the stack left %d layers", len(snap.Layers))
	}
}
