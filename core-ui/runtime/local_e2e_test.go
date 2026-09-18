package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Browser coverage for src/local.js, the browser-store primitive: the
// engine is IndexedDB, the fallback is localStorage for tiny values
// only, every key lives under one namespace, and a second tab of the
// same origin hears a write.
//
// The module has no marker; these tests load it the way an application
// does, with __gofastr.loadModule('local').

func awaitLocalPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// startLocalServer serves runtime.js and the runtime modules, plus one
// page. head is injected into <head> before runtime.js runs, which is
// where a test disables IndexedDB to reach the fallback.
func startLocalServer(t *testing.T, head string) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Module("local"); !ok {
		t.Fatal("src/local.js is not embedded")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			_, _ = w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// /idb is the same page on the same ORIGIN with head omitted, so
		// one test can write on the fallback and then come back with
		// IndexedDB available to the same store.
		injected := head
		if r.URL.Path == "/idb" {
			injected = ""
		}
		fmt.Fprintf(w, `<!doctype html><html><head><title>local</title>%s</head><body>
  <main role="main"><span id="ready">ready</span></main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, injected)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// openLocal navigates and loads the primitive, failing the test if it
// never registers.
func openLocal(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.loadModule('local')`, nil, awaitLocalPromise),
	); err != nil {
		t.Fatalf("loading the local module: %v", err)
	}
	var ok bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__gofastr.local`, &ok)); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the local module ran without publishing window.__gofastr.local")
	}
}

func localPollTrue(ctx context.Context, js string) bool {
	for range 60 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v, awaitLocalPromise)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// The engine is IndexedDB: a value round-trips with its type intact, it
// is enumerable by its application key, remove clears it, and none of
// it lands in localStorage.
func TestLocalRoundTripsThroughIndexedDB(t *testing.T) {
	srv := startLocalServer(t, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	var avail map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.available()`, &avail, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if idb, _ := avail["idb"].(bool); !idb {
		t.Fatalf("available() = %v, want idb true — this test would otherwise measure the fallback", avail)
	}

	var res map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.set('teams', { list: ['a', 'b'], n: 2 })`, &res, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("set() = %v, want ok", res)
	}
	var round string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.get('teams').then((v) => JSON.stringify(v))`, &round, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if round != `{"list":["a","b"],"n":2}` {
		t.Fatalf("get() = %s — the value did not round-trip with its type", round)
	}
	var keys []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.keys().then((r) => r.keys)`, &keys, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "teams" {
		t.Fatalf("keys() = %v, want [teams] — the namespace must be stripped, not leaked", keys)
	}
	// Nothing touched Web storage: IndexedDB is the engine, not a cache
	// in front of localStorage.
	var lsCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Object.keys(localStorage).length`, &lsCount)); err != nil {
		t.Fatal(err)
	}
	if lsCount != 0 {
		t.Fatalf("%d localStorage keys written while IndexedDB was available", lsCount)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.remove('teams')`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.get('teams').then((v) => v === undefined ? 'gone' : 'still-there')`, &after, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if after != "gone" {
		t.Fatalf("after remove(), get() says %q", after)
	}
}

// A read of a key nobody wrote is undefined, not a throw and not a
// stale neighbour: the best-effort contract's quiet half.
func TestLocalMissingKeyIsUndefined(t *testing.T) {
	srv := startLocalServer(t, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.get('never-written').then((v) => v === undefined ? 'undefined' : JSON.stringify(v))`, &got, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if got != "undefined" {
		t.Fatalf("get() on an absent key = %s", got)
	}
}

// noIDB blanks window.indexedDB before runtime.js runs, which is the
// only honest way to reach the fallback path: the module caches the
// open attempt, so the API has to be gone before the first call.
const noIDB = `<script>Object.defineProperty(window, 'indexedDB', { value: undefined, configurable: true });</script>`

// Without IndexedDB the primitive falls back to localStorage, under the
// same namespace, and only for tiny values. A larger one is refused
// with a reason rather than silently dropped or half-written.
func TestLocalFallsBackToLocalStorageForTinyValuesOnly(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	var avail map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.available()`, &avail, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if idb, _ := avail["idb"].(bool); idb {
		t.Fatal("IndexedDB was still reachable — the fallback path is untested")
	}
	if ls, _ := avail["ls"].(bool); !ls {
		t.Fatalf("available() = %v, want ls true", avail)
	}

	var res map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.set('pref', 'dark')`, &res, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("a tiny value was refused by the fallback: %v", res)
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`localStorage.getItem('gofastr.state.' + encodeURIComponent('pref')) || ''`, &stored)); err != nil {
		t.Fatal(err)
	}
	if stored != `"dark"` {
		t.Fatalf("fallback stored %q under gofastr.state.pref, want the JSON text — the namespace or the engine is wrong", stored)
	}
	var round string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.get('pref').then((v) => String(v))`, &round, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if round != "dark" {
		t.Fatalf("fallback get() = %q", round)
	}
	var keys []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.keys().then((r) => r.keys)`, &keys, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "pref" {
		t.Fatalf("fallback keys() = %v, want [pref]", keys)
	}

	// Over the fallback's tiny-value cap: refused, with a reason, and
	// nothing written.
	var big map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.set('bulk', 'x'.repeat(20000))`, &big, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := big["ok"].(bool); ok {
		t.Fatal("the fallback accepted a 20 KB value — localStorage is the origin's whole budget")
	}
	if reason, _ := big["reason"].(string); reason != "size" {
		t.Fatalf("set() refusal reason = %q, want \"size\"", reason)
	}
	var bulk string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`localStorage.getItem('gofastr.state.' + encodeURIComponent('bulk')) || 'absent'`, &bulk)); err != nil {
		t.Fatal(err)
	}
	if bulk != "absent" {
		t.Fatal("a refused write still reached localStorage")
	}
}

// An application key can never name storage outside the namespace, and
// a key that would collide with one if it were not encoded does not.
func TestLocalKeysStayInsideTheNamespace(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`localStorage.setItem('gofastr.colorScheme', 'light')`, nil),
		chromedp.Evaluate(`window.__gofastr.local.set('../colorScheme', 'dark')`, nil, awaitLocalPromise),
		chromedp.Evaluate(`window.__gofastr.local.set('gofastr.colorScheme', 'dark')`, nil, awaitLocalPromise),
	); err != nil {
		t.Fatal(err)
	}
	var scheme string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`localStorage.getItem('gofastr.colorScheme') || ''`, &scheme)); err != nil {
		t.Fatal(err)
	}
	if scheme != "light" {
		t.Fatalf("an application key reached another feature's storage: gofastr.colorScheme = %q", scheme)
	}
	var outside int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Object.keys(localStorage).filter((k) => k.indexOf('gofastr.state.') !== 0 && k !== 'gofastr.colorScheme').length`, &outside)); err != nil {
		t.Fatal(err)
	}
	if outside != 0 {
		t.Fatalf("%d keys written outside the namespace", outside)
	}
}

// Two real tabs of the same origin: a subscriber in one hears the
// other's write, and never its own.
func TestLocalSubscribeCrossesTabs(t *testing.T) {
	srv := startLocalServer(t, "")
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, tabA, srv.URL+"/")
	if err := chromedp.Run(tabA, chromedp.Evaluate(`(() => {
        window.__heard = [];
        window.__gofastr.local.subscribe('shared', (v) => window.__heard.push(v));
        window.__gofastr.local.set('shared', 'written-here');
    })()`, nil)); err != nil {
		t.Fatal(err)
	}

	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openLocal(t, tabB, srv.URL+"/")
	if err := chromedp.Run(tabB, chromedp.Evaluate(
		`window.__gofastr.local.set('shared', 'written-next-door')`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}

	if !localPollTrue(tabA, `Promise.resolve(window.__heard.indexOf('written-next-door') >= 0)`) {
		var heard []string
		_ = chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard))
		t.Fatalf("tab A heard %v — the sibling tab's write never arrived", heard)
	}
	var heard []string
	if err := chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard)); err != nil {
		t.Fatal(err)
	}
	for _, v := range heard {
		if v == "written-here" {
			t.Fatal("tab A heard its own write back — a subscriber is a cross-tab channel, not a change feed")
		}
	}
}

// keys and entries take a prefix and enumerate only what lies under it,
// through the engine's key range rather than a scan: a layer that
// groups records under a common prefix can list one group without
// reading its neighbours, and an entry's size is the stored UTF-8
// length of its JSON text.
func TestLocalPrefixEnumeration(t *testing.T) {
	srv := startLocalServer(t, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	if err := chromedp.Run(ctx, chromedp.Evaluate(`Promise.all([
        window.__gofastr.local.set('local.site.drafts:b', { title: 'second' }),
        window.__gofastr.local.set('local.site.drafts:a', { title: 'first' }),
        window.__gofastr.local.set('local.site.draftsx:z', { title: 'neighbour' }),
        window.__gofastr.local.set('local.site.prefs:theme', 'dark'),
        window.__gofastr.local.set('unrelated', 1),
    ])`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}

	var keys []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.keys('local.site.drafts:').then((r) => r.keys)`, &keys, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "local.site.drafts:a" || keys[1] != "local.site.drafts:b" {
		t.Fatalf("keys(prefix) = %v, want exactly the two drafts, sorted — a sibling prefix (draftsx) or another group leaked in", keys)
	}
	var all []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.keys().then((r) => r.keys)`, &all, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("keys() = %v, want all five — the no-prefix form must still name the whole namespace", all)
	}

	var entries []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.entries('local.site.drafts:').then((r) => r.entries)`, &entries, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries(prefix) = %v, want two", entries)
	}
	first, _ := entries[0]["value"].(map[string]any)
	if entries[0]["key"] != "local.site.drafts:a" || first["title"] != "first" {
		t.Fatalf("entries(prefix)[0] = %v, want the a record with its value", entries[0])
	}
	// {"title":"first"} is 17 bytes of JSON.
	if size, _ := entries[0]["size"].(float64); int(size) != 17 {
		t.Fatalf("entries(prefix)[0].size = %v, want 17 (the UTF-8 length of the stored JSON text)", entries[0]["size"])
	}
}

// The fallback engine enumerates by prefix too, so a layer above sees
// one contract whichever engine answered.
func TestLocalPrefixEnumerationOnTheFallback(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	if err := chromedp.Run(ctx, chromedp.Evaluate(`Promise.all([
        window.__gofastr.local.set('g:1', 'é'),
        window.__gofastr.local.set('g:2', 'b'),
        window.__gofastr.local.set('h:1', 'c'),
    ])`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.entries('g:').then((r) => r.entries)`, &entries, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0]["key"] != "g:1" || entries[1]["key"] != "g:2" {
		t.Fatalf("fallback entries('g:') = %v, want g:1 and g:2", entries)
	}
	// "é" is 4 bytes of JSON text: two quotes and a two-byte rune. The
	// size is bytes, not code units.
	if size, _ := entries[0]["size"].(float64); int(size) != 4 {
		t.Fatalf("fallback entries('g:')[0].size = %v, want 4", entries[0]["size"])
	}
}

// watch hears another tab's write of any key under a prefix, and never
// this tab's own: one watcher covers a whole group of records.
func TestLocalWatchCrossesTabsByPrefix(t *testing.T) {
	srv := startLocalServer(t, "")
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, tabA, srv.URL+"/")
	if err := chromedp.Run(tabA, chromedp.Evaluate(`(() => {
        window.__heard = [];
        window.__gofastr.local.watch('grp:', (k) => window.__heard.push(k));
        window.__gofastr.local.set('grp:mine', 1);
    })()`, nil)); err != nil {
		t.Fatal(err)
	}

	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openLocal(t, tabB, srv.URL+"/")
	if err := chromedp.Run(tabB, chromedp.Evaluate(`Promise.all([
        window.__gofastr.local.set('other:x', 1),
        window.__gofastr.local.set('grp:theirs', 2),
    ])`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}

	if !localPollTrue(tabA, `Promise.resolve(window.__heard.indexOf('grp:theirs') >= 0)`) {
		var heard []string
		_ = chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard))
		t.Fatalf("tab A heard %v — the sibling tab's write under the prefix never arrived", heard)
	}
	var heard []string
	if err := chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard)); err != nil {
		t.Fatal(err)
	}
	for _, k := range heard {
		if k == "grp:mine" {
			t.Fatal("tab A heard its own write back")
		}
		if k == "other:x" {
			t.Fatal("a watcher heard a key outside its prefix")
		}
	}
}

// available() names the engine that will answer, and the two
// enumerations settle the way set and remove do.
//
// Two booleans could not say which engine answers, since a caller reading
// { idb: false, ls: true } has to re-derive the rule, and keys/entries
// resolving a bare array made an aborted transaction indistinguishable
// from an empty store, which is how a clear() built on them reported
// success over records that all survived.
func TestLocalNamesItsEngineAndSettlesItsEnumerations(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	openLocal(t, ctx, srv.URL+"/idb")
	var avail map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.available()`, &avail, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if avail["engine"] != "idb" {
		t.Fatalf("available() = %v, want engine \"idb\" — the caller cannot otherwise say which engine answered", avail)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.set('one', 1)`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	var ks map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.keys()`, &ks, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ks["ok"].(bool); !ok {
		t.Fatalf("keys() = %v, want { ok: true, keys: [...] } — an enumeration has to be able to say it failed", ks)
	}
	if list, _ := ks["keys"].([]any); len(list) != 1 || list[0] != "one" {
		t.Fatalf("keys().keys = %v, want [one]", ks["keys"])
	}
	var es map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.entries()`, &es, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := es["ok"].(bool); !ok {
		t.Fatalf("entries() = %v, want { ok: true, entries: [...] }", es)
	}
	if list, _ := es["entries"].([]any); len(list) != 1 {
		t.Fatalf("entries().entries = %v, want the one entry", es["entries"])
	}

	// And the fallback says so by name, not by elimination.
	openLocal(t, ctx, srv.URL+"/")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.available()`, &avail, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if avail["engine"] != "ls" {
		t.Fatalf("available() on the fallback = %v, want engine \"ls\"", avail)
	}
}

// A session that fell back to localStorage leaves entries no IndexedDB
// session can see: invisible to get, invisible to keys, and out of
// reach of any clear: the previous user's records surviving a logout.
// The first session that does open the database adopts them and empties
// the fallback of this namespace.
func TestLocalAdoptsFallbackEntriesWhenIndexedDBReturns(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	// Session one: no IndexedDB, so the entry lands in the fallback.
	openLocal(t, ctx, srv.URL+"/")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.local.set('ghost', 'left-behind')`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`localStorage.getItem('gofastr.state.' + encodeURIComponent('ghost')) || ''`, &stored)); err != nil {
		t.Fatal(err)
	}
	if stored != `"left-behind"` {
		t.Fatalf("the fallback holds %q — this test is not measuring what it says it is", stored)
	}

	// Session two, same origin, IndexedDB available.
	openLocal(t, ctx, srv.URL+"/idb")
	if !localPollTrue(ctx, `window.__gofastr.local.get('ghost').then((v) => v === 'left-behind')`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.get('ghost').then((v) => String(v))`, &got, awaitLocalPromise))
		t.Fatalf("get('ghost') = %q — the fallback entry is a ghost the IndexedDB session cannot see", got)
	}
	var left string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`localStorage.getItem('gofastr.state.' + encodeURIComponent('ghost')) || ''`, &left)); err != nil {
		t.Fatal(err)
	}
	if left != "" {
		t.Fatalf("the fallback still holds %q — adoption must be one-way and leave nothing behind", left)
	}
	// And a remove reaches the fallback too, whichever engine answers.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        localStorage.setItem('gofastr.state.' + encodeURIComponent('ghost'), '"re-planted"');
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.local.remove('ghost')`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`localStorage.getItem('gofastr.state.' + encodeURIComponent('ghost')) || ''`, &left)); err != nil {
		t.Fatal(err)
	}
	if left != "" {
		t.Fatalf("remove left %q in the fallback — a delete has to reach both engines", left)
	}
}

// On the fallback engine a write reaches the other tabs of the origin
// through the native storage event, which this module already listens
// for. Announcing on BroadcastChannel as well delivered the same change
// twice, and every subscriber fired twice for one write.
func TestLocalFallbackDeliversAWriteOnce(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, tabA, srv.URL+"/")
	if err := chromedp.Run(tabA, chromedp.Evaluate(`(() => {
        window.__heard = [];
        window.__gofastr.local.subscribe('once', (v) => window.__heard.push(v));
    })()`, nil)); err != nil {
		t.Fatal(err)
	}

	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openLocal(t, tabB, srv.URL+"/")
	if err := chromedp.Run(tabB, chromedp.Evaluate(
		`window.__gofastr.local.set('once', 'x')`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}

	if !localPollTrue(tabA, `Promise.resolve(window.__heard.length >= 1)`) {
		t.Fatal("tab A never heard the sibling tab's fallback write")
	}
	// Both transports are fast; a second delivery would already be in.
	// Wait past it anyway so the assertion is about the count, not the
	// clock.
	time.Sleep(500 * time.Millisecond)
	var heard []string
	if err := chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard)); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 {
		t.Fatalf("tab A heard %v — one fallback write must deliver once, not once per transport", heard)
	}
}

// remove() drops the key from both engines, and when the fallback held
// it the native storage event already carries the removal to the other
// tabs. Announcing on BroadcastChannel as well delivered the one remove
// twice, the same shape set() had already fixed for writes. Tab B holds
// the key in IndexedDB and in localStorage; tab A hears the remove once.
func TestLocalRemoveDeliversOnceWhenBothEnginesHeldTheKey(t *testing.T) {
	srv := startLocalServer(t, "")
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, tabA, srv.URL+"/")
	// Open tab A's database now, so adoption runs before the fallback
	// entry is planted and cannot move it.
	if err := chromedp.Run(tabA, chromedp.Evaluate(`window.__gofastr.local.available()`, nil, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}

	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openLocal(t, tabB, srv.URL+"/")
	if err := chromedp.Run(tabB,
		chromedp.Evaluate(`window.__gofastr.local.set('both', 'x').then((r) => r.ok)`, nil, awaitLocalPromise),
		chromedp.Evaluate(`localStorage.setItem('gofastr.state.' + encodeURIComponent('both'), '"x"')`, nil),
	); err != nil {
		t.Fatal(err)
	}
	// The plant fired a storage event in tab A; let it settle before
	// counting anything.
	time.Sleep(300 * time.Millisecond)
	if err := chromedp.Run(tabA, chromedp.Evaluate(`(() => {
        window.__heard = [];
        window.__gofastr.local.subscribe('both', (v) => window.__heard.push(v === undefined ? 'gone' : String(v)));
    })()`, nil)); err != nil {
		t.Fatal(err)
	}

	var res map[string]any
	if err := chromedp.Run(tabB, chromedp.Evaluate(`window.__gofastr.local.remove('both')`, &res, awaitLocalPromise)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("remove() = %v, want ok", res)
	}
	if !localPollTrue(tabA, `Promise.resolve(window.__heard.length >= 1)`) {
		t.Fatal("tab A never heard the sibling tab's remove")
	}
	time.Sleep(500 * time.Millisecond)
	var heard []string
	if err := chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard)); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 {
		t.Fatalf("tab A heard %v: one remove must deliver once, not once per transport", heard)
	}
}

// On the fallback engine every throw from setItem used to be 'quota'. A
// SecurityError (the origin's storage is blocked) is the engine being
// gone, and a caller that reads 'quota' would tell the user to free
// room that does not exist. Only a quota error is 'quota'.
func TestLocalFallbackNamesABlockedStoreUnavailable(t *testing.T) {
	srv := startLocalServer(t, noIDB)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	openLocal(t, ctx, srv.URL+"/")

	for _, tc := range []struct{ thrown, want string }{
		{"SecurityError", "unavailable"},
		{"QuotaExceededError", "quota"},
		{"NS_ERROR_DOM_QUOTA_REACHED", "quota"},
	} {
		var res map[string]any
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
            const real = Storage.prototype.setItem;
            Storage.prototype.setItem = function () { throw new DOMException('blocked', %q); };
            return window.__gofastr.local.set('pref', 'dark').finally(() => { Storage.prototype.setItem = real; });
        })()`, tc.thrown), &res, awaitLocalPromise)); err != nil {
			t.Fatal(err)
		}
		if ok, _ := res["ok"].(bool); ok {
			t.Fatalf("%s: set() reported ok while setItem threw", tc.thrown)
		}
		if reason, _ := res["reason"].(string); reason != tc.want {
			t.Errorf("setItem threw %s: reason = %q, want %q", tc.thrown, reason, tc.want)
		}
	}
}
