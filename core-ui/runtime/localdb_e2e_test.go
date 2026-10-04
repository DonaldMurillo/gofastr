package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The Go declarations under test: the manifest the pages carry is
// produced by core-ui/localdb itself, so these tests pin the Go → JS
// contract, not a hand-copied JSON shape.
var (
	ldbTest    = localdb.New("e2e")
	_          = ldbTest.Store("members", localdb.AutoKey(), localdb.Index("by_level", "level"))
	_          = ldbTest.Store("tags", localdb.KeyPath("slug"), localdb.UniqueIndex("by_label", "label"))
	ldbUUIDv7  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	ldbConsole = wsConsoleTap
)

// ldbServer serves the runtime and its modules plus one page per
// entry in pages: path → {manifest JSON, script}. An empty manifest
// uses the live Go declarations.
func ldbServer(t *testing.T, pages map[string][2]string) string {
	t.Helper()
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for path, page := range pages {
		manifest, script := page[0], page[1]
		if manifest == "" {
			manifest = string(localdb.ManifestJSON())
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html><html><head><script type="application/json" id="gofastr-localdb">`+
				manifest+`</script></head><body><span id="ready">ready</span>`+
				`<script src="/__gofastr/runtime.js"></script><script>`+ldbConsole+script+`</script></body></html>`)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// ldbRun navigates, waits for window.__done, and decodes window.__res.
func ldbRun(t *testing.T, ctx context.Context, url string, out any) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__done === true`, nil, chromedp.WithPollingTimeout(15*time.Second)),
	); err != nil {
		t.Fatalf("chromedp %s: %v", url, err)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__res)`, &raw)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	var logs []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__logs || []`, &logs)); err == nil {
		for _, l := range logs {
			if strings.Contains(l, "SECRET-RECORD") {
				t.Fatalf("a record value reached the console: %s", l)
			}
		}
	}
}

// ldbScript wraps body (an async function body with `db` in scope) so
// every result or failure lands in window.__res.
func ldbScript(dbName, body string) string {
	return `
    (async () => {
      const res = {};
      try {
        await __gofastr.loadModule('localdb');
        const L = __gofastr.localdb;
        const code = async (p) => { try { await p; return 'ok'; } catch (e) { return (e && e.code) || String(e); } };
        const db = await L.open('` + dbName + `');
        ` + body + `
      } catch (e) {
        res.fatal = (e && (e.code + ': ' + e.message)) || String(e);
      }
      window.__res = res;
      window.__done = true;
    })();
  `
}

func TestLocalDBCrudOrderAndErrors(t *testing.T) {
	base := ldbServer(t, map[string][2]string{"/": {"", ldbScript("e2e", `
        const a = await db.put('members', { name: 'Pikachu', level: 30, note: 'SECRET-RECORD' });
        const b = await db.put('members', { name: 'Bulbasaur', level: 12 });
        const c = await db.put('members', { name: 'Charizard', level: 55 });
        res.ids = [a, b, c];
        // A burst minted inside one millisecond still sorts in mint order.
        const burst = [];
        for (let i = 0; i < 300; i++) burst.push(L.newID());
        res.burstSorted = burst.every((id, i) => i === 0 || burst[i - 1] < id);
        res.got = await db.get('members', b);
        res.byLevel = (await db.list('members', { index: 'by_level' })).map((r) => r.name);
        res.byLevelDesc = (await db.list('members', { index: 'by_level', direction: 'prev', limit: 2 })).map((r) => r.name);
        res.offset = (await db.list('members', { index: 'by_level', offset: 1, limit: 1 })).map((r) => r.name);
        res.range = (await db.list('members', { index: 'by_level', lower: 20, upper: 40 })).map((r) => r.name);
        res.keys = await db.list('members', { index: 'by_level', only: 55, keys: true });
        res.count = await db.count('members');
        await db.put('members', Object.assign({}, res.got, { level: 13 }));
        res.updated = (await db.get('members', b)).level;
        await db.delete('members', a);
        res.afterDelete = await db.count('members');

        await db.put('tags', { slug: 'fire', label: 'Fire' });
        res.uniqueViolation = await code(db.put('tags', { slug: 'blaze', label: 'Fire' }));
        res.addExisting = await code(db.add('tags', { slug: 'fire', label: 'Other' }));
        res.unknownStore = await code(db.put('nope', { id: 1 }));
        res.unknownIndex = await code(db.list('members', { index: 'by_name' }));
        res.badRecord = await code(db.put('members', 'just a string'));
        res.badDirection = await code(db.list('members', { direction: 'sideways' }));
        res.badLimit = await code(db.list('members', { limit: -1 }));
        res.unknownDB = await code(L.open('nope'));
        res.protoName = await code(L.open('__proto__'));

        // A transaction is all-or-nothing: the put inside a body that
        // then throws must not land.
        res.txAbort = await code(db.tx(['members', 'tags'], 'readwrite', async (t) => {
          await t.put('members', { name: 'Ghost', level: 1 });
          throw new Error('changed my mind');
        }));
        res.afterAbort = await db.count('members');
        res.txOK = await db.tx(['members', 'tags'], 'readwrite', async (t) => {
          await t.put('members', { name: 'Eevee', level: 5 });
          await t.put('tags', { slug: 'normal', label: 'Normal' });
          return 'committed';
        });
        res.afterTx = [await db.count('members'), await db.count('tags')];
        res.kept = (await db.get('members', b)).name;
    `)}})
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var res struct {
		Fatal           string         `json:"fatal"`
		IDs             []string       `json:"ids"`
		BurstSorted     bool           `json:"burstSorted"`
		Got             map[string]any `json:"got"`
		ByLevel         []string       `json:"byLevel"`
		ByLevelDesc     []string       `json:"byLevelDesc"`
		Offset          []string       `json:"offset"`
		Range           []string       `json:"range"`
		Keys            []string       `json:"keys"`
		Count           int            `json:"count"`
		Updated         int            `json:"updated"`
		AfterDelete     int            `json:"afterDelete"`
		UniqueViolation string         `json:"uniqueViolation"`
		AddExisting     string         `json:"addExisting"`
		UnknownStore    string         `json:"unknownStore"`
		UnknownIndex    string         `json:"unknownIndex"`
		BadRecord       string         `json:"badRecord"`
		BadDirection    string         `json:"badDirection"`
		BadLimit        string         `json:"badLimit"`
		UnknownDB       string         `json:"unknownDB"`
		ProtoName       string         `json:"protoName"`
		TxAbort         string         `json:"txAbort"`
		AfterAbort      int            `json:"afterAbort"`
		TxOK            string         `json:"txOK"`
		AfterTx         []int          `json:"afterTx"`
		Kept            string         `json:"kept"`
	}
	ldbRun(t, ctx, base+"/", &res)
	if res.Fatal != "" {
		t.Fatalf("page failed: %s", res.Fatal)
	}
	if len(res.IDs) != 3 {
		t.Fatalf("ids = %v", res.IDs)
	}
	for _, id := range res.IDs {
		if !ldbUUIDv7.MatchString(id) {
			t.Fatalf("AutoKey minted %q, want a UUIDv7", id)
		}
	}
	if !(res.IDs[0] < res.IDs[1] && res.IDs[1] < res.IDs[2]) {
		t.Fatalf("UUIDv7 keys must sort by creation: %v", res.IDs)
	}
	if !res.BurstSorted {
		t.Fatal("UUIDv7 keys minted in one millisecond must sort in mint order")
	}
	if res.Got["name"] != "Bulbasaur" || res.Got["id"] != res.IDs[1] {
		t.Fatalf("get = %v", res.Got)
	}
	want := map[string][2]string{
		"byLevel":     {strings.Join(res.ByLevel, ","), "Bulbasaur,Pikachu,Charizard"},
		"byLevelDesc": {strings.Join(res.ByLevelDesc, ","), "Charizard,Pikachu"},
		"offset":      {strings.Join(res.Offset, ","), "Pikachu"},
		"range":       {strings.Join(res.Range, ","), "Pikachu"},
		"keys":        {strings.Join(res.Keys, ","), res.IDs[2]},
	}
	for name, pair := range want {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
	if res.Count != 3 || res.Updated != 13 || res.AfterDelete != 2 {
		t.Errorf("count %d, updated %d, afterDelete %d", res.Count, res.Updated, res.AfterDelete)
	}
	codes := map[string][2]string{
		"uniqueViolation": {res.UniqueViolation, "constraint"},
		"addExisting":     {res.AddExisting, "constraint"},
		"unknownStore":    {res.UnknownStore, "unknown-store"},
		"unknownIndex":    {res.UnknownIndex, "unknown-index"},
		"badRecord":       {res.BadRecord, "invalid"},
		"badDirection":    {res.BadDirection, "invalid"},
		"badLimit":        {res.BadLimit, "invalid"},
		"unknownDB":       {res.UnknownDB, "unknown-db"},
		"protoName":       {res.ProtoName, "unknown-db"},
		"txAbort":         {res.TxAbort, "failed"},
	}
	for name, pair := range codes {
		if pair[0] != pair[1] {
			t.Errorf("%s code = %q, want %q", name, pair[0], pair[1])
		}
	}
	if res.AfterAbort != 2 {
		t.Errorf("an aborted transaction's write landed: count %d, want 2", res.AfterAbort)
	}
	if res.TxOK != "committed" || len(res.AfterTx) != 2 || res.AfterTx[0] != 3 || res.AfterTx[1] != 2 {
		t.Errorf("tx = %q, counts %v; want committed, [3 2]", res.TxOK, res.AfterTx)
	}
	if res.Kept != "Bulbasaur" {
		t.Errorf("record b = %q after the transactions", res.Kept)
	}
}

// A deploy that adds a store and an index upgrades the database the
// previous deploy created, in place, keeping its records; a tab still
// on the previous deploy keeps working against the newer schema.
func TestLocalDBAdditiveUpgradeAcrossDeploys(t *testing.T) {
	v1 := `{"shop":{"stores":{"items":{"keyPath":"id"}}}}`
	v2 := `{"shop":{"stores":{"items":{"keyPath":"id","indexes":{"by_price":{"keyPath":["price"]}}},"carts":{"keyPath":"id"}}}}`
	broken := `{"shop":{"stores":{"items":{"keyPath":"sku"}}}}`
	// v2's by_price, redefined: rebuilt in place it would ping-pong
	// with every tab still on v2.
	reindexed := `{"shop":{"stores":{"items":{"keyPath":"id","indexes":{"by_price":{"keyPath":["price","id"]}}},"carts":{"keyPath":"id"}}}}`
	base := ldbServer(t, map[string][2]string{
		"/v1": {v1, ldbScript("shop", `
            await db.put('items', { id: 'a', price: 3 });
            await db.put('items', { id: 'b', price: 1 });
            res.count = await db.count('items');
        `)},
		"/v2": {v2, ldbScript("shop", `
            res.byPrice = (await db.list('items', { index: 'by_price' })).map((r) => r.id);
            await db.put('carts', { id: 'c1' });
            res.carts = await db.count('carts');
        `)},
		"/v1-again": {v1, ldbScript("shop", `
            res.count = await db.count('items');
            await db.put('items', { id: 'c', price: 2 });
            res.after = await db.count('items');
        `)},
		"/index-changed": {reindexed, `
            (async () => {
              await __gofastr.loadModule('localdb');
              const res = {};
              try { await __gofastr.localdb.open('shop'); res.code = 'ok'; }
              catch (e) { res.code = e.code; }
              window.__res = res; window.__done = true;
            })();
        `},
		"/keypath-changed": {broken, `
            (async () => {
              await __gofastr.loadModule('localdb');
              const res = {};
              try { await __gofastr.localdb.open('shop'); res.code = 'ok'; }
              catch (e) { res.code = e.code; }
              window.__res = res; window.__done = true;
            })();
        `},
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var r1 struct{ Count int }
	ldbRun(t, ctx, base+"/v1", &r1)
	if r1.Count != 2 {
		t.Fatalf("v1 count = %d", r1.Count)
	}
	var r2 struct {
		Fatal   string   `json:"fatal"`
		ByPrice []string `json:"byPrice"`
		Carts   int      `json:"carts"`
	}
	ldbRun(t, ctx, base+"/v2", &r2)
	if r2.Fatal != "" || strings.Join(r2.ByPrice, ",") != "b,a" || r2.Carts != 1 {
		t.Fatalf("v2 after upgrade = %+v; want the v1 records indexed by price and the new store usable", r2)
	}
	var r3 struct {
		Fatal string `json:"fatal"`
		Count int    `json:"count"`
		After int    `json:"after"`
	}
	ldbRun(t, ctx, base+"/v1-again", &r3)
	if r3.Fatal != "" || r3.Count != 2 || r3.After != 3 {
		t.Fatalf("a v1 page after the v2 upgrade = %+v; want it to read and write the newer database", r3)
	}
	var r5 struct{ Code string }
	ldbRun(t, ctx, base+"/index-changed", &r5)
	if r5.Code != "schema" {
		t.Fatalf("an index whose definition changed opened with %q, want schema (a new shape takes a new name)", r5.Code)
	}
	var r4 struct{ Code string }
	ldbRun(t, ctx, base+"/keypath-changed", &r4)
	if r4.Code != "schema" {
		t.Fatalf("a store whose key path changed opened with %q, want schema", r4.Code)
	}
}

// Two tabs of one origin: a write in one reaches the other's watcher
// as a remote change, the writer's own watcher hears it as local, and
// a forged broadcast naming an undeclared store is dropped. Then the
// second tab's schema upgrade must not wedge the first: its open
// connection steps aside and its next operation reopens.
func TestLocalDBCrossTabWatchAndUpgrade(t *testing.T) {
	watcher := ldbScript("e2e", `
        window.__seen = [];
        db.watch('members', (e) => window.__seen.push(e.origin + ':' + e.changes.map((c) => c.op).join('+')));
        // Forgeries any same-origin script could post: a message that
        // mixes a declared store with an undeclared one, and one with
        // an op the module never sends. Each is dropped whole.
        const forge = new BroadcastChannel('gofastr.localdb.e2e');
        forge.postMessage({ v: 1, changes: [{ store: 'members', op: 'put', key: 1 }, { store: 'evil', op: 'put', key: 2 }] });
        forge.postMessage({ v: 1, changes: [{ store: 'members', op: 'explode', key: 1 }] });
        forge.postMessage({ v: 2, changes: [{ store: 'members', op: 'put', key: 1 }] });
        res.ready = true;
    `)
	writer := ldbScript("e2e", `
        const local = [];
        db.watch('members', (e) => local.push(e.origin));
        res.key = await db.put('members', { name: 'Mew', level: 70 });
        await new Promise((r) => setTimeout(r, 50));
        res.local = local;
    `)
	upgraded := `{"e2e":{"stores":{"members":{"keyPath":"id","autoKey":true,"indexes":{"by_level":{"keyPath":["level"]},"by_name":{"keyPath":["name"]}}},"tags":{"keyPath":"slug","indexes":{"by_label":{"keyPath":["label"],"unique":true}}}}}}`
	base := ldbServer(t, map[string][2]string{
		"/watch":   {"", watcher},
		"/write":   {"", writer},
		"/upgrade": {upgraded, ldbScript("e2e", `res.named = (await db.list('members', { index: 'by_name' })).length;`)},
	})
	tabA := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var ra struct{ Ready bool }
	ldbRun(t, tabA, base+"/watch", &ra)

	tabB, cancel := chromedp.NewContext(tabA)
	defer cancel()
	var rb struct {
		Fatal string   `json:"fatal"`
		Key   string   `json:"key"`
		Local []string `json:"local"`
	}
	ldbRun(t, tabB, base+"/write", &rb)
	if rb.Fatal != "" || strings.Join(rb.Local, ",") != "local" {
		t.Fatalf("writer tab = %+v; want exactly one local event", rb)
	}

	var seen []string
	if err := chromedp.Run(tabA,
		chromedp.Poll(`window.__seen.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__seen`, &seen),
	); err != nil {
		t.Fatalf("watcher tab never heard the write: %v", err)
	}
	if strings.Join(seen, ",") != "remote:put" {
		t.Fatalf("watcher saw %v, want [remote:put] (and never the forged store)", seen)
	}

	// Tab B moves to a deploy with one more index while tab A still
	// holds its connection open.
	var ru struct {
		Fatal string `json:"fatal"`
		Named int    `json:"named"`
	}
	ldbRun(t, tabB, base+"/upgrade", &ru)
	if ru.Fatal != "" || ru.Named < 1 {
		t.Fatalf("upgrade tab = %+v; want the new index usable while another tab is open", ru)
	}
	var count int
	if err := chromedp.Run(tabA, chromedp.Evaluate(
		`__gofastr.localdb.open('e2e').then((db) => db.count('members'))`, &count,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) },
	)); err != nil {
		t.Fatalf("tab A after the upgrade: %v", err)
	}
	if count < 1 {
		t.Fatalf("tab A count after the other tab upgraded = %d", count)
	}
}
