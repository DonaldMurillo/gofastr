package store

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gofastrruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Browser coverage for core-ui/store's browser-persisted slices
// (store.Slice.Persist) over the kernel's `local` primitive. The Go
// side is unit-tested above; what only a browser can show is the
// promise itself: a value set in the page is still there after a
// reload, a value over the declared cap is refused loudly, a second
// tab of the same origin converges, and a slice that did not opt in is
// never written at all.
//
// These tests deliberately do NOT isolate the registry: they exercise
// the shipped registration, the shipped marker, the shipped
// Requires("local"), and the shipped module sources.

// awaitPromise lets chromedp.Evaluate resolve a promise. The whole
// local primitive is asynchronous (IndexedDB is), so every assertion
// about what the browser holds goes through it.
func awaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// startPersistServer serves runtime.js, the registered modules, and one
// page carrying the bindings the caller passes.
func startPersistServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	js, err := gofastrruntime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"signal-persist", "local"} {
		if _, ok := gofastrruntime.Module(name); !ok {
			t.Fatalf("module %q is not served by runtime.Module — the registration did not reach the host", name)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := gofastrruntime.Module(name); ok {
			_, _ = w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	buf := gofastrruntime.BehaviorsJSON()
	if buf == nil {
		t.Fatal("BehaviorsJSON returned nil — the kernel would never learn the marker")
	}
	block := `<script type="application/json" id="gofastr-behaviors">` + string(buf) + `</script>`
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>persist</title>%s</head><body>
  <main role="main"><span id="ready">ready</span>%s</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, block, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// bindPersisted renders the SSR markup for a persisted slice the way an
// app would, and returns it with the slice.
func bindPersisted(t *testing.T, ns, name, def string, max int) (*Slice[string], string) {
	t.Helper()
	sl := New(ns).String(name, def).PersistMax(max)
	html := string(sl.Bind(context.Background(), "span", map[string]string{"id": "view"}))
	return sl, html
}

func persistPollTrue(ctx context.Context, js string) bool {
	for range 60 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v, awaitPromise)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

const persistLoadedExpr = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['signal-persist'] && window.__gofastr.local)`

// storedExpr reads what the browser store holds for a slice,
// through the primitive's own public API.
func storedExpr(name string) string {
	return fmt.Sprintf(`window.__gofastr.local.get(%q).then((v) => v === undefined ? '' : JSON.stringify(v))`, name)
}

// The headline promise: a value written into the signal in one page
// load is the value the signal holds after a reload, even though the
// server keeps sending its own default.
func TestPersistedSliceSurvivesAReload(t *testing.T) {
	sl, html := bindPersisted(t, "e2epersist", "draft", "server-default", 4096)
	srv := startPersistServer(t, html)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The module has to be loaded before the write, or the listener
	// that persists it is not attached yet.
	if !persistPollTrue(ctx, persistLoadedExpr) {
		t.Fatal("the signal-persist module never loaded for a page carrying the marker")
	}
	// The engine is IndexedDB, not the fallback.
	var engine map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.available()`, &engine, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if idb, _ := engine["idb"].(bool); !idb {
		t.Fatalf("available() = %v — headless Chrome must give us IndexedDB, or this test is measuring the fallback", engine)
	}
	// First paint shows the server's value: the browser has nothing yet.
	var first string
	if err := chromedp.Run(ctx, chromedp.Text(`#view`, &first, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if first != "server-default" {
		t.Fatalf("first paint = %q, want the server's seeded value", first)
	}

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'typed-in-the-browser')`, name), nil),
	); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, storedExpr(name)+` .then((s) => s === '"typed-in-the-browser"')`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(storedExpr(name), &got, awaitPromise))
		t.Fatalf("the browser store holds %q, want the value the page set", got)
	}
	// And it went nowhere the server can see.
	var cookies string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &cookies)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cookies, "gofastr.state") {
		t.Fatalf("a persisted slice planted a cookie — the value must stay invisible to the server: %q", cookies)
	}

	if err := chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !persistPollTrue(ctx, `Promise.resolve(document.querySelector('#view') && document.querySelector('#view').textContent === 'typed-in-the-browser')`) {
		var after string
		_ = chromedp.Run(ctx, chromedp.Text(`#view`, &after, chromedp.ByID))
		t.Fatalf("after reload the binding shows %q — the browser's value did not win over the server seed", after)
	}
	var sig string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`String(window.__gofastr.getSignal(%q))`, name), &sig)); err != nil {
		t.Fatal(err)
	}
	if sig != "typed-in-the-browser" {
		t.Fatalf("getSignal after reload = %q — a screen reading the slice sees the wrong value", sig)
	}
}

// A slice that did not opt in is never written, and the primitive is
// never even fetched: persistence is an explicit declaration, not a
// property of every signal.
func TestUnpersistedSliceIsNeverStored(t *testing.T) {
	sl := New("e2enopersist").String("draft", "server-default")
	html := string(sl.Bind(context.Background(), "span", map[string]string{"id": "view"}))
	srv := startPersistServer(t, html)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'should-not-persist')`, sl.Name()), nil),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var loaded bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!((window.__gofastr.loadedModules && window.__gofastr.loadedModules['signal-persist']) || window.__gofastr.local)`, &loaded)); err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Error("a page with no persisted binding loaded the persistence modules — the marker is the trigger")
	}
	var lsKeys int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Object.keys(localStorage).filter((k) => k.indexOf('gofastr.state.') === 0).length`, &lsKeys)); err != nil {
		t.Fatal(err)
	}
	if lsKeys != 0 {
		t.Fatalf("%d gofastr.state.* keys written for a slice that never declared Persist", lsKeys)
	}
}

// Over the declared cap the value is NOT written and the page is told,
// so an app can say "you have run out of room" instead of losing
// the write silently. The previously stored value stays put, and the
// in-memory signal is untouched: persistence is best-effort, the UI is
// not.
func TestPersistedSliceRefusesAValueOverItsCap(t *testing.T) {
	sl, html := bindPersisted(t, "e2epersistcap", "draft", "", 64)
	srv := startPersistServer(t, html)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !persistPollTrue(ctx, persistLoadedExpr) {
		t.Fatal("the signal-persist module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        window.__persistOverflow = [];
        window.addEventListener('gofastr:persist-overflow', (e) => window.__persistOverflow.push(e.detail));
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'fits')`, name), nil)); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, storedExpr(name)+` .then((s) => s === '"fits"')`) {
		t.Fatal("the value that fits never reached the browser store")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'x'.repeat(500))`, name), nil)); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, `Promise.resolve(window.__persistOverflow.length > 0)`) {
		t.Fatal("an over-cap write raised no gofastr:persist-overflow — an app cannot tell the user what it cannot see")
	}
	var reason string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__persistOverflow[0].reason`, &reason)); err != nil {
		t.Fatal(err)
	}
	if reason != "size" {
		t.Fatalf("gofastr:persist-overflow reason = %q, want \"size\"", reason)
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(storedExpr(name), &stored, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if stored != `"fits"` {
		t.Fatalf("stored = %q, want the last value that fit — an over-cap write must not replace or clear it", stored)
	}
	var live int
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`String(window.__gofastr.getSignal(%q)).length`, name), &live)); err != nil {
		t.Fatal(err)
	}
	if live != 500 {
		t.Fatalf("the in-memory signal is %d chars — a refused write must not roll the UI back", live)
	}
}

// Two real tabs of the same origin converge: a write in one reaches the
// other's binding without a reload. The transport is the primitive's
// BroadcastChannel; neither tab hears its own write back.
func TestPersistedSliceConvergesAcrossTwoTabs(t *testing.T) {
	sl, html := bindPersisted(t, "e2epersisttabs", "draft", "server-default", 4096)
	srv := startPersistServer(t, html)
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(tabA,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("tab A: %v", err)
	}
	if !persistPollTrue(tabA, persistLoadedExpr) {
		t.Fatal("tab A never loaded the persistence modules")
	}

	// A second target in the SAME browser: same origin, same IndexedDB,
	// same BroadcastChannel.
	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	if err := chromedp.Run(tabB,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("tab B: %v", err)
	}
	if !persistPollTrue(tabB, persistLoadedExpr) {
		t.Fatal("tab B never loaded the persistence modules")
	}
	if err := chromedp.Run(tabB,
		chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'written-next-door')`, name), nil),
	); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(tabA, `Promise.resolve(document.querySelector('#view').textContent === 'written-next-door')`) {
		var got string
		_ = chromedp.Run(tabA, chromedp.Text(`#view`, &got, chromedp.ByID))
		t.Fatalf("tab A shows %q — the sibling tab's write never arrived", got)
	}
}

// Trust does not survive a round trip through the browser store.
//
// runtime.js marks a signal untrusted when its value came from an input
// the page does not author (widgets.js seeds one from location.search)
// and refuses to write an untrusted value through innerHTML. Both ends
// of persistence used to launder that flag: the write-back stored an
// untrusted value like any other, and the restore called setSignal with
// no options, which CLEARS the flag, so the value the browser handed
// back took the innerHTML branch on the next load. Two assertions, one
// per end.
func TestPersistedSliceDoesNotLaunderAnUntrustedValue(t *testing.T) {
	sl := New("e2etrust").String("html", "server-default").PersistMax(4096)
	html := string(sl.BindHTML(context.Background(), "span", map[string]string{"id": "view"}))
	srv := startPersistServer(t, html)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !persistPollTrue(ctx, persistLoadedExpr) {
		t.Fatal("the signal-persist module never loaded for a page carrying the marker")
	}

	// End one: an untrusted value is never written to the store.
	const payload = `<img id="pwned" src="x" onerror="window.__pwned=1">`
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		fmt.Sprintf(`window.__gofastr.setSignal(%q, %q, { untrusted: true })`, name, payload), nil)); err != nil {
		t.Fatal(err)
	}
	// Give the listener the same window the passing case gets.
	var stored string
	for range 10 {
		if err := chromedp.Run(ctx, chromedp.Evaluate(storedExpr(name), &stored, awaitPromise)); err != nil {
			t.Fatal(err)
		}
		if stored != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if stored != "" {
		t.Fatalf("the browser store holds %q — an untrusted signal value must never be persisted", stored)
	}

	// End two: a value that IS in the store comes back untrusted, so an
	// html-mode binding renders it as text. Seed it through the
	// primitive, the way a previous session (or any script on the
	// origin) would have left it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		fmt.Sprintf(`window.__gofastr.local.set(%q, %q).then((r) => r.ok)`, name, payload), nil, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !persistPollTrue(ctx, fmt.Sprintf(
		`window.__gofastr.local.get(%q).then(() => document.getElementById('view').textContent === %q)`, name, payload)) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('view').innerHTML`, &got))
		t.Fatalf("#view innerHTML = %q — the restored value must land as TEXT, not markup", got)
	}
	var pwned bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__pwned || !!document.getElementById('pwned')`, &pwned)); err != nil {
		t.Fatal(err)
	}
	if pwned {
		t.Fatal("the restored value became live markup: persistence laundered the untrusted flag")
	}
}

// The cap is in bytes, and the bytes are UTF-8: PersistMax's doc, the
// attribute table and the primitive's entries() all say so. The module
// used to compare text.length, which counts UTF-16 units, so a CJK
// value three times its unit count in bytes passed a cap it was over.
// The value here is 30 CJK characters: 32 UTF-16 units of JSON text,
// under a 64-byte cap, and 92 UTF-8 bytes, over it.
func TestPersistedSliceCountsItsCapInUTF8Bytes(t *testing.T) {
	sl, html := bindPersisted(t, "e2epersistutf8", "draft", "", 64)
	srv := startPersistServer(t, html)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !persistPollTrue(ctx, persistLoadedExpr) {
		t.Fatal("the signal-persist module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        window.__persistOverflow = [];
        window.addEventListener('gofastr:persist-overflow', (e) => window.__persistOverflow.push(e.detail));
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, 'fits')`, name), nil)); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, storedExpr(name)+` .then((s) => s === '"fits"')`) {
		t.Fatal("the value that fits never reached the browser store")
	}
	const cjk = "漢字" // two CJK characters, 6 UTF-8 bytes, 2 UTF-16 units
	value := strings.Repeat(cjk, 15)
	if n := len(value) + 2; n != 92 {
		t.Fatalf("fixture: JSON text is %d UTF-8 bytes, want 92", n)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal(%q, %q)`, name, value), nil)); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, `Promise.resolve(window.__persistOverflow.length > 0)`) {
		t.Fatal("a value over its cap in bytes raised no gofastr:persist-overflow: the cap was measured in UTF-16 units")
	}
	var detail map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__persistOverflow[0]`, &detail)); err != nil {
		t.Fatal(err)
	}
	if reason, _ := detail["reason"].(string); reason != "size" {
		t.Fatalf("reason = %q, want \"size\"", reason)
	}
	if size, _ := detail["size"].(float64); int(size) != 92 {
		t.Fatalf("size = %v, want 92 UTF-8 bytes (the UTF-16 count is 32)", detail["size"])
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(storedExpr(name), &stored, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if stored != `"fits"` {
		t.Fatalf("stored = %q, want the last value that fit", stored)
	}
}

// A setSignal that lands while the restore's read is still in flight is
// newer than anything the store holds. The restore used to apply
// whatever came back, so a keystroke in the first few hundred
// milliseconds was overwritten by the previous session's value. The
// page is served without the binding so the read can be slowed before
// the module wires it: the primitive is loaded, its get() delayed, and
// only then is the binding inserted and the behaviour loaded.
func TestPersistedSliceRestoreDoesNotOverwriteANewerValue(t *testing.T) {
	sl, html := bindPersisted(t, "e2epersistrace", "draft", "server-default", 4096)
	srv := startPersistServer(t, `<div id="host"></div>`)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	name := sl.Name()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.loadModule('local')`, nil, awaitPromise),
		chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.local.set(%q, 'stale-from-store').then((r) => r.ok)`, name), nil, awaitPromise),
	); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}
	// Slow the read, insert the binding, load the behaviour (which wires
	// the binding and requests the restore), then type at +100ms.
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
        const local = window.__gofastr.local;
        const real = local.get;
        // Read now, resolve late: the value in flight is the one the
        // store held when the restore was requested.
        local.get = (k) => real.call(local, k).then((v) => new Promise((resolve) => setTimeout(() => resolve(v), 500)));
        document.getElementById('host').innerHTML = %q;
        return window.__gofastr.loadModule('signal-persist').then(() => {
          setTimeout(() => window.__gofastr.setSignal(%q, 'typed-while-reading'), 100);
        });
    })()`, html, name), nil, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if !persistPollTrue(ctx, persistLoadedExpr) {
		t.Fatal("the signal-persist module never loaded")
	}
	// Past the delayed read, with margin.
	time.Sleep(1200 * time.Millisecond)
	var sig string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`String(window.__gofastr.getSignal(%q))`, name), &sig)); err != nil {
		t.Fatal(err)
	}
	if sig != "typed-while-reading" {
		t.Fatalf("getSignal = %q: the restore overwrote a value set while its read was in flight", sig)
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(storedExpr(name), &stored, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	if stored != `"typed-while-reading"` {
		t.Fatalf("stored = %q, want the typed value", stored)
	}
}
