package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"

	uiruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// Browser coverage for the local-store module and its three bridges,
// against the real registration and the real runtime: an httptest
// server serves runtime.js, every module by name, the inline
// #gofastr-behaviors block (the shape an export ships), the store's
// manifest script on the extra-script rail, one page, and one RPC
// endpoint wrapped by Upload.Wrap that echoes what it read and writes
// a record back. Mirrors framework/headless/behavior_e2e_test.go.

type e2eDraft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	Body  string `json:"body,omitempty"`
}

type e2ePrefs struct {
	Theme string `json:"theme"`
}

// e2eStore declares one store per test process. The app id is fixed
// because the module's storage prefix is what the tests read back;
// New refuses a duplicate, so the declaration is shared through once.
var (
	e2eOnce   sync.Once
	e2eSite   *Store
	e2eDrafts *Collection[e2eDraft]
	e2ePref   *Collection[e2ePrefs]
	e2eSeed   *SeededSignal[e2eDraft]
	// Collections no marker on the page touches, so a test can plant a
	// stored version and watch the FIRST migration of a collection run.
	// The page's own markers migrate drafts and prefs at scan time.
	e2eNotes  *Collection[e2eDraft]
	e2eTagged *Collection[e2eTag]
)

type e2eTag struct {
	Colour string `json:"colour,omitempty"`
	Theme  string `json:"theme,omitempty"`
}

func e2eDeclare(t *testing.T) {
	t.Helper()
	e2eOnce.Do(func() {
		resetForTest()
		e2eSite = New("e2e")
		e2eDrafts = Define[e2eDraft](e2eSite, "drafts", CollectionConfig{
			Version: 2, KeyField: "id", MaxRecordBytes: 512, MaxRecords: 3, MaxBytes: 1024,
			Migrations: []Migration{{Version: 2, Steps: []Step{Rename("body", "text"), Func("drafts-v2")}}},
		})
		e2ePref = Define[e2ePrefs](e2eSite, "prefs", CollectionConfig{Version: 1, Mirror: true})
		e2eNotes = Define[e2eDraft](e2eSite, "notes", CollectionConfig{
			Version: 2, KeyField: "id",
			Migrations: []Migration{{Version: 2, Steps: []Step{Rename("body", "text")}}},
		})
		e2eTagged = Define[e2eTag](e2eSite, "tagged", CollectionConfig{
			Version: 2, Mirror: true,
			Migrations: []Migration{{Version: 2, Steps: []Step{Rename("colour", "theme")}}},
		})
		e2eSeed = SeedSignal(e2eDrafts, "current", store.JSON[e2eDraft](store.New("e2elocal"), "current", e2eDraft{Title: "server default"}))
	})
}

// e2eServer is the page and the endpoint.
type e2eServer struct {
	srv *httptest.Server
	mu  sync.Mutex
	// what the wrapped handler saw, per request
	seen []e2eSeen
}

type e2eSeen struct {
	Body    string
	Drafts  []Record[e2eDraft]
	Current e2eDraft
	Found   Source
	Theme   string
	Err     error
	// Note is the form field beside the records, as the wrapped handler
	// sees it after a form body was parsed; LeakedField says whether the
	// reserved field survived the strip.
	Note        string
	LeakedField bool
}

func startE2E(t *testing.T, tls ...bool) *e2eServer {
	t.Helper()
	e2eDeclare(t)
	js, err := uiruntime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := uiruntime.Module(BehaviorName); !ok {
		t.Fatalf("%s is not served by runtime.Module", BehaviorName)
	}
	block := uiruntime.BehaviorsJSON()
	e := &e2eServer{}
	up := Send(e2eDrafts.Key("current"), e2ePref)
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := uiruntime.Module(name); ok {
			_, _ = w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle(e2eSite.ScriptPath(), e2eSite.ScriptHandler())
	// The app's own script on the rail: the func migration, and a
	// counter the tests read.
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`window.__migrated = 0;
(window.__gofastr._localMigrations = window.__gofastr._localMigrations || {})['drafts-v2'] = (rec) => { window.__migrated++; rec.title = (rec.title || '') + ' (v2)'; return rec; };
window.__errors = []; window.addEventListener('gofastr:local-error', (e) => window.__errors.push(e.detail));
window.__migrations = []; window.addEventListener('gofastr:local-migrated', (e) => window.__migrations.push(e.detail));`))
	})
	mux.Handle("/upload", up.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var s e2eSeen
		s.Body = string(b)
		s.Note = r.PostForm.Get("note")
		_, s.LeakedField = r.PostForm[uploadField]
		s.Drafts, s.Err = List(r.Context(), e2eDrafts)
		s.Current, s.Found, _ = Get(r.Context(), e2eDrafts, "current")
		if p, src, _ := Get(r.Context(), e2ePref, "theme"); src.Found() {
			s.Theme = p.Theme
		}
		e.mu.Lock()
		e.seen = append(e.seen, s)
		e.mu.Unlock()
		if s.Found.Found() {
			// Download: the server pushes a record back and a second one.
			_ = Put(w, e2eDrafts, "current", e2eDraft{ID: "current", Title: s.Current.Title + " (server)"})
			_ = Put(w, e2eDrafts, "from-server", e2eDraft{ID: "from-server", Title: "pushed"})
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		ClearOnNextLoad(w, r, e2eSite)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	// The manifest, no store marker: the page a full-navigation logout
	// can land on. Nothing here opens a store on its own.
	mux.HandleFunc("/nomarker", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>nomarker</title>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`<main role="main"><span id="ready">ready</span></main>`+
			`<script src="/__gofastr/runtime.js"></script><script src="%s"></script>`+
			`<script src="/app.js"></script></body></html>`, block, e2eSite.scriptURL())
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>plain</title>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`<main role="main"><span id="ready">ready</span></main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`, block)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		ctx := context.Background()
		seedEl := e2eSeed.Bind(ctx, "p", map[string]string{"id": "seeded"})
		form := `<form id="up" data-fui-rpc="/upload" data-fui-rpc-signal="up-result"` + attrString(up.Attrs()) + `><input name="note" value="hi"><button id="send" type="submit">send</button></form>`
		// The same declaration on a multipart form: a file input makes the
		// runtime post FormData, and the bridge appends the field to it.
		form += `<form id="up-multi" enctype="multipart/form-data" data-fui-rpc="/upload" data-fui-rpc-signal="up-result"` + attrString(up.Attrs()) + `><input name="note" value="multi"><input type="file" name="attachment"><button id="send-multi" type="submit">send multipart</button></form>`
		fmt.Fprintf(w, `<!doctype html><html><head><title>local</title>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`<main role="main"><span id="ready">ready</span>%s%s<span id="result" data-fui-signal="up-result"></span>`+
			`<a id="away" href="/other">other</a></main>`+
			`<script src="/__gofastr/runtime.js"></script><script src="%s"></script><script src="/app.js"></script></body></html>`,
			block, seedEl, form, e2eSite.scriptURL())
	})
	if len(tls) > 0 && tls[0] {
		e.srv = httptest.NewTLSServer(mux)
	} else {
		e.srv = httptest.NewServer(mux)
	}
	t.Cleanup(e.srv.Close)
	return e
}

func attrString(m map[string]string) string {
	var sb strings.Builder
	for _, k := range sortedKeys(m) {
		sb.WriteString(" " + k + `="` + m[k] + `"`)
	}
	return sb.String()
}

func (e *e2eServer) last(t *testing.T) e2eSeen {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.seen) == 0 {
		t.Fatal("the upload endpoint was never called")
	}
	return e.seen[len(e.seen)-1]
}

func awaitP(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }

// openPage navigates, waits for the page and loads the module the way
// an application script does.
func openPage(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.loadModule('local-store')`, nil, awaitP),
	); err != nil {
		t.Fatalf("loading local-store: %v", err)
	}
}

func pollTrue(ctx context.Context, js string) bool {
	for range 60 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v, awaitP)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func evalJSON(t *testing.T, ctx context.Context, js string, into any) {
	t.Helper()
	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw, awaitP)); err != nil {
		t.Fatalf("evaluate %s: %v", js, err)
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("decode %s = %s: %v", js, raw, err)
		}
	}
}

const draftsJS = `window.__gofastr.localStore('e2e').collection('drafts')`
const prefsJS = `window.__gofastr.localStore('e2e').collection('prefs')`

// A record put on one page load is back on the next, under the
// namespaced primitive key; a second browser never sees it.
func TestE2E_RecordSurvivesReloadAndSessionsAreIsolated(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({id: 'a', title: 'first'})`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}
	var keys []string
	evalJSON(t, ctx, `window.__gofastr.local.keys('local.e2e.drafts:').then((r) => r.keys)`, &keys)
	if len(keys) != 1 || keys[0] != "local.e2e.drafts:a" {
		t.Fatalf("primitive keys = %v: the record must live under local.<app>.<collection>:<key>", keys)
	}

	openPage(t, ctx, e.srv.URL+"/")
	var back map[string]any
	evalJSON(t, ctx, draftsJS+`.get('a')`, &back)
	if back["title"] != "first" {
		t.Fatalf("after reload get('a') = %v", back)
	}
	var n int
	evalJSON(t, ctx, draftsJS+`.count()`, &n)
	if n != 1 {
		t.Fatalf("count = %d", n)
	}

	other := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, other, e.srv.URL+"/")
	evalJSON(t, other, draftsJS+`.count()`, &n)
	if n != 0 {
		t.Fatalf("a second browser session sees %d records — storage leaked across sessions", n)
	}
}

// The caps refuse with the documented reason and never throw: a
// record over MaxRecordBytes says 'size', one more than MaxRecords
// says 'full', and each raises gofastr:local-error.
func TestE2E_CapsRefuseWithAReasonNotAnException(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({id: 'big', title: 'x'.repeat(600)})`, &res)
	if res["ok"] != false || res["reason"] != "size" {
		t.Fatalf("over the record cap: %v, want {ok:false, reason:'size'}", res)
	}
	var allOK bool
	evalJSON(t, ctx, `Promise.all([`+draftsJS+`.put({id:'1',title:'a'}),`+draftsJS+`.put({id:'2',title:'b'}),`+draftsJS+`.put({id:'3',title:'c'})]).then(rs => rs.every(r => r.ok))`, &allOK)
	if !allOK {
		t.Fatal("three records within the caps were refused")
	}
	evalJSON(t, ctx, draftsJS+`.put({id: '4', title: 'd'})`, &res)
	if res["ok"] != false || res["reason"] != "full" {
		t.Fatalf("over MaxRecords: %v, want {ok:false, reason:'full'}", res)
	}
	// Replacing an existing record is not a fourth record.
	evalJSON(t, ctx, draftsJS+`.put({id: '3', title: 'c2'})`, &res)
	if res["ok"] != true {
		t.Fatalf("replacing a record must fit: %v", res)
	}
	evalJSON(t, ctx, draftsJS+`.put({title: 'no key'})`, &res)
	if res["ok"] != false || res["reason"] != "key" {
		t.Fatalf("a record without its key field: %v", res)
	}
	evalJSON(t, ctx, `window.__gofastr.localStore('e2e').collection('nope')`, &res)
	if res != nil {
		t.Fatalf("an undeclared collection must be null, got %v", res)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `window.__errors`, &errs)
	if len(errs) != 3 || errs[0]["reason"] != "size" || errs[1]["reason"] != "full" || errs[2]["reason"] != "key" {
		t.Fatalf("gofastr:local-error events = %v", errs)
	}
	if max, _ := errs[0]["max"].(float64); int(max) != 512 {
		t.Fatalf("the size event must carry the cap: %v", errs[0])
	}
	// list with an order.
	var rows []map[string]any
	evalJSON(t, ctx, draftsJS+`.list({orderBy: 'title', desc: true})`, &rows)
	if len(rows) != 3 || rows[0]["key"] != "3" || rows[1]["key"] != "2" || rows[2]["key"] != "1" {
		t.Fatalf("list(orderBy title desc) = %v", rows)
	}
}

// A collection written at schema v1 is migrated to v2 exactly once:
// the declared rename runs, the host's func runs per record, the
// version is stamped, and a reload runs nothing again.
func TestE2E_MigrationRunsOnce(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	// Plant v1 data through the primitive, the way an older build of
	// the app would have left it, from a page with NO marker: on the
	// real page the kernel loads the module at boot and the store's
	// migration would race the plant.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(e.srv.URL+"/plain"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.loadModule('local').then(() => Promise.all([
            window.__gofastr.local.set('local.e2e.drafts:old', {id: 'old', title: 'legacy', body: 'text from v1'}),
            window.__gofastr.local.set('local.e2e.drafts', {v: 1}),
        ]))`, nil, awaitP),
	); err != nil {
		t.Fatal(err)
	}
	openPage(t, ctx, e.srv.URL+"/")
	var rec map[string]any
	evalJSON(t, ctx, draftsJS+`.get('old')`, &rec)
	if rec["text"] != "text from v1" || rec["body"] != nil || rec["title"] != "legacy (v2)" {
		t.Fatalf("after migration get('old') = %v: want body renamed to text and the func applied", rec)
	}
	var meta map[string]any
	evalJSON(t, ctx, `window.__gofastr.local.get('local.e2e.drafts')`, &meta)
	if v, _ := meta["v"].(float64); int(v) != 2 {
		t.Fatalf("stored version = %v, want 2", meta)
	}
	var n int
	evalJSON(t, ctx, `window.__migrated`, &n)
	if n != 1 {
		t.Fatalf("the func migration ran %d times on one record", n)
	}
	var ev []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__migrations.filter((d) => d.collection === 'drafts'))`, &ev)
	if len(ev) != 1 || ev[0]["from"] != float64(1) || ev[0]["to"] != float64(2) {
		t.Fatalf("gofastr:local-migrated = %v", ev)
	}

	// Reload: nothing runs again.
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, draftsJS+`.get('old')`, &rec)
	evalJSON(t, ctx, `window.__migrated`, &n)
	if n != 0 || rec["title"] != "legacy (v2)" {
		t.Fatalf("second load: migrated %d times, title %q — a migration must run once per browser", n, rec["title"])
	}
}

// The seed bridge: SSR paints the server's default, the runtime
// patches the record in after hydration, a change to the signal
// writes back, and the value survives a client-side navigation away
// and back through the route cache.
func TestE2E_SeededSignalRoundTripsAndSurvivesNavigation(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	// Nothing stored yet: the default stays.
	var text string
	if err := chromedp.Run(ctx, chromedp.Text(`#seeded`, &text, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "server default") {
		t.Fatalf("first paint = %q, want the server default", text)
	}
	// Set the signal: the record is written.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('e2elocal.current', {id: 'current', title: 'typed by the user'})`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, draftsJS+`.get('current').then(v => !!v && v.title === 'typed by the user')`) {
		t.Fatal("a signal change never reached the record")
	}
	// Reload: SSR paints the default, then the record wins.
	openPage(t, ctx, e.srv.URL+"/")
	if !pollTrue(ctx, `Promise.resolve(document.getElementById('seeded').textContent.indexOf('typed by the user') >= 0)`) {
		_ = chromedp.Run(ctx, chromedp.Text(`#seeded`, &text, chromedp.ByID))
		t.Fatalf("after reload the seeded element shows %q — the record was not patched in", text)
	}
	// Navigate away and back through the runtime's router, then check
	// the restored page still shows the record and the signal.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#away`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve(location.pathname === '/other')`) {
		t.Fatal("navigation to /other never happened")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve(location.pathname === '/' && !!document.getElementById('seeded') && document.getElementById('seeded').textContent.indexOf('typed by the user') >= 0)`) {
		_ = chromedp.Run(ctx, chromedp.Text(`#seeded`, &text, chromedp.ByID))
		t.Fatalf("after back-navigation the seeded element shows %q", text)
	}
	var sig map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__gofastr.getSignal('e2elocal.current'))`, &sig)
	if sig["title"] != "typed by the user" {
		t.Fatalf("signal after back = %v", sig)
	}
}

// The upload bridge delivers exactly the declared collection and key
// to the wrapped Go handler, and not the undeclared records, and
// the download bridge writes the handler's records back.
func TestE2E_UploadDeliversOnlyTheDeclaredAndDownloadWritesBack(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, `Promise.all([
        `+draftsJS+`.put({id: 'current', title: 'mine'}),
        `+draftsJS+`.put({id: 'private', title: 'never sent'}),
        `+prefsJS+`.put('theme', {theme: 'dark'}),
    ])`, nil)
	if err := chromedp.Run(ctx, chromedp.Click(`#send`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve((document.getElementById('result').textContent || '').indexOf('ok') >= 0)`) {
		t.Fatal("the upload RPC never answered")
	}
	seen := e.last(t)
	if seen.Body != `{"note":"hi"}` {
		t.Fatalf("the handler read body %q: the reserved field must be stripped and the form field kept", seen.Body)
	}
	if seen.Found != SourceUpload || seen.Current.Title != "mine" {
		t.Fatalf("Get(current) = %+v found=%v", seen.Current, seen.Found)
	}
	if seen.Theme != "dark" {
		t.Fatalf("the mirrored prefs record did not arrive: theme=%q", seen.Theme)
	}
	for _, d := range seen.Drafts {
		if d.Key == "private" {
			t.Fatal("an undeclared record was uploaded")
		}
	}
	if len(seen.Drafts) != 1 {
		t.Fatalf("List = %+v, want only the declared key", seen.Drafts)
	}
	// Download: the response wrote two records.
	if !pollTrue(ctx, draftsJS+`.get('from-server').then(v => !!v && v.title === 'pushed')`) {
		t.Fatal("the record the response pushed never landed")
	}
	var cur map[string]any
	evalJSON(t, ctx, draftsJS+`.get('current')`, &cur)
	if cur["title"] != "mine (server)" {
		t.Fatalf("the response's rewrite of current = %v", cur)
	}
	// The mirror cookie is what a screen reads at first paint.
	var cookie string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &cookie)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cookie, "gofastr.local.e2e.prefs.theme=") {
		t.Fatalf("no mirror cookie in %q", cookie)
	}
}

// Two real tabs: a subscriber in one hears the other's write with
// source 'tab', and its own with source 'local'.
func TestE2E_SecondTabSeesTheWrite(t *testing.T) {
	e := startE2E(t)
	tabA := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, tabA, e.srv.URL+"/")
	if err := chromedp.Run(tabA, chromedp.Evaluate(`(() => {
        window.__heard = [];
        `+draftsJS+`.subscribe((ev) => window.__heard.push(ev.source + ':' + ev.key));
        `+draftsJS+`.put({id: 'mine', title: 'a'});
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openPage(t, tabB, e.srv.URL+"/")
	evalJSON(t, tabB, draftsJS+`.put({id: 'theirs', title: 'b'})`, nil)
	if !pollTrue(tabA, `Promise.resolve(window.__heard.indexOf('tab:theirs') >= 0 && window.__heard.indexOf('local:mine') >= 0)`) {
		var heard []string
		_ = chromedp.Run(tabA, chromedp.Evaluate(`window.__heard`, &heard))
		t.Fatalf("tab A heard %v, want both local:mine and tab:theirs", heard)
	}
	if !pollTrue(tabA, draftsJS+`.get('theirs').then(v => !!v && v.title === 'b')`) {
		t.Fatal("tab A cannot read tab B's record")
	}
}

// Logout by full navigation: ClearOnNextLoad plants the bit, the next
// page load clears every collection and drops the cookie.
func TestE2E_ClearOnNextLoad(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, `Promise.all([`+draftsJS+`.put({id: 'x', title: 'a'}), `+prefsJS+`.put('theme', {theme: 'dark'})])`, nil)
	if err := chromedp.Run(ctx, chromedp.Navigate(e.srv.URL+"/logout"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	openPage(t, ctx, e.srv.URL+"/")
	if !pollTrue(ctx, draftsJS+`.count().then(n => n === 0)`) {
		t.Fatal("drafts survived the clear bit")
	}
	if !pollTrue(ctx, prefsJS+`.count().then(n => n === 0)`) {
		t.Fatal("prefs survived the clear bit")
	}
	var cookie string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &cookie)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cookie, "gofastr.local.clear.e2e=") || strings.Contains(cookie, "gofastr.local.e2e.prefs.theme=") {
		t.Fatalf("cookies after clear: %q", cookie)
	}
}

// A logout that could not read the store must not report success.
//
// The primitive's enumerations settle {ok, reason, …} so a
// caller can tell an aborted transaction from an empty collection.
// clear() built on the bare array could not: it reported {ok:true} for
// a store whose records, and whose mirror cookies, all survived,
// which on the logout path is the previous user's data left in the
// browser with the UI saying it is gone.
func TestE2E_ClearReportsAnEnumerationItCouldNotRead(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({id: 'a', title: 'kept'})`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}
	// Fault injection at the primitive's seam: the enumeration fails the
	// way an aborted IndexedDB transaction fails. Reads still work, so
	// the assertion below is about what clear reported, not about a
	// browser that stopped answering.
	evalJSON(t, ctx, `Promise.resolve((() => {
        window.__gofastr.local.keys = () => Promise.resolve({ ok: false, reason: 'unavailable', keys: [] });
        return true;
    })())`, nil)

	evalJSON(t, ctx, draftsJS+`.clear()`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("clear() = %v — an enumeration that failed is not an empty collection", res)
	}
	if res["reason"] != "unavailable" {
		t.Fatalf("clear() = %v, want reason \"unavailable\"", res)
	}
	evalJSON(t, ctx, `window.__gofastr.localStore('e2e').clear()`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("store.clear() = %v — a logout has to report the collection it could not clear", res)
	}
	// The record is still there, which is why the report matters.
	var kept e2eDraft
	evalJSON(t, ctx, draftsJS+`.get('a').then((v) => v || null)`, &kept)
	if kept.Title != "kept" {
		t.Fatalf("the record is %v — this test is not measuring what it says it is", kept)
	}
	// And the page heard about it.
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	found := false
	for _, d := range errs {
		if d["reason"] == "unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gofastr:local-error never carried the refusal: %v", errs)
	}
}

const notesJS = `window.__gofastr.localStore('e2e').collection('notes')`
const taggedJS = `window.__gofastr.localStore('e2e').collection('tagged')`

// plantV1 writes a record and a stored schema version straight through
// the primitive, the way a browser that ran an older build would hold
// them, so the next touch of the collection runs its first migration.
func plantV1(t *testing.T, ctx context.Context, coll, key, json string, version int) {
	t.Helper()
	evalJSON(t, ctx, fmt.Sprintf(`Promise.all([
        window.__gofastr.local.set('local.e2e.%s:%s', %s),
        window.__gofastr.local.set('local.e2e.%s', { v: %d }),
    ]).then(() => true)`, coll, key, json, coll, version), nil)
}

// A migration that only half happened must not be recorded as done, and
// the collection it left behind must not answer.
//
// P.set settles {ok:false} on a quota refusal or an aborted transaction
// rather than rejecting, so Promise.all resolved and the new version was
// stamped over records that were still on the old schema, a corruption
// that never re-runs. And whenReady's answer was discarded by every
// method, so the records were served anyway.
func TestE2E_AFailedMigrationNeitherStampsNorAnswers(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	plantV1(t, ctx, "notes", "n1", `{ id: 'n1', body: 'old shape' }`, 1)

	// Fault injection at the primitive's seam: every write from here on
	// refuses the way a full quota refuses.
	evalJSON(t, ctx, `Promise.resolve((() => {
        window.__gofastr.local.set = () => Promise.resolve({ ok: false, reason: 'quota' });
        return true;
    })())`, nil)

	// The first touch runs the migration.
	var res map[string]any
	evalJSON(t, ctx, notesJS+`.put({ id: 'n2', title: 'new' })`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("put = %v — a collection whose migration failed must refuse", res)
	}
	if res["reason"] != "migration" {
		t.Fatalf("put = %v, want reason \"migration\"", res)
	}
	evalJSON(t, ctx, notesJS+`.delete('n1')`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("delete = %v — every method is gated, not just put", res)
	}
	evalJSON(t, ctx, notesJS+`.clear()`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("clear = %v — every method is gated", res)
	}
	var n int
	evalJSON(t, ctx, notesJS+`.count()`, &n)
	if n != 0 {
		t.Fatalf("count = %d — a gated collection answers nothing", n)
	}
	var list []map[string]any
	evalJSON(t, ctx, notesJS+`.list()`, &list)
	if len(list) != 0 {
		t.Fatalf("list = %v — a gated collection must not serve records on the old schema", list)
	}

	// And the stored version is untouched, so the next load re-runs it.
	var meta map[string]any
	evalJSON(t, ctx, `window.__gofastr.local.get('local.e2e.notes').then((v) => v || null)`, &meta)
	if v, _ := meta["v"].(float64); int(v) != 1 {
		t.Fatalf("the stored version is %v — a half-failed migration must never be stamped as done", meta)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	found := false
	for _, d := range errs {
		if d["reason"] == "migration" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gofastr:local-error never said \"migration\": %v", errs)
	}
}

// A stored version ABOVE the declared one is a rollback: the build that
// wrote those records is newer than this one, whose steps cannot undo
// it. Accepting it silently (have >= target) served records of an
// unknown schema as if they were this one.
func TestE2E_AVersionDowngradeIsRefusedNotAccepted(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	plantV1(t, ctx, "notes", "n1", `{ id: 'n1', title: 'from the future', extra: 1 }`, 7)

	var res map[string]any
	evalJSON(t, ctx, notesJS+`.put({ id: 'n2', title: 'new' })`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("put = %v — a collection a newer build wrote must be refused", res)
	}
	var got map[string]any
	evalJSON(t, ctx, notesJS+`.get('n1').then((v) => v || null)`, &got)
	if got != nil {
		t.Fatalf("get = %v — a rolled-back build must not read records it cannot understand", got)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	for _, d := range errs {
		if d["reason"] == "version" {
			return
		}
	}
	t.Fatalf("gofastr:local-error never said \"version\": %v", errs)
}

// The mirror cookie carries the MIGRATED record. Re-encoding the
// entry's pre-migration value handed the server the old schema on every
// request, for as long as nothing wrote the record again, and the
// server validates the cookie against the NEW type, so the record the
// browser holds and the record the server reads disagree.
func TestE2E_TheMirrorCarriesTheMigratedRecord(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	plantV1(t, ctx, "tagged", "t1", `{ colour: 'dark' }`, 1)

	var n int
	evalJSON(t, ctx, taggedJS+`.count()`, &n)
	if n != 1 {
		t.Fatalf("count = %d, want the planted record migrated in place", n)
	}
	var cookie string
	evalJSON(t, ctx, `Promise.resolve(decodeURIComponent(
        (('; ' + document.cookie).split('; gofastr.local.' + encodeURIComponent('e2e.tagged.t1') + '=')[1] || '').split(';')[0]))`, &cookie)
	if cookie != `{"theme":"dark"}` {
		t.Fatalf("the mirror cookie holds %q, want the migrated record — the server would otherwise read the pre-migration shape", cookie)
	}
}

// On https the mirror cookie is Secure. Without it a single plain-http
// request to the origin, a typo, a stripped link, a captive portal,
// carries every mirrored record in clear text, and the Go side has set
// Secure from the request scheme since the first commit.
func TestE2E_TheMirrorCookieIsSecureOnHTTPS(t *testing.T) {
	e := startE2E(t, true)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second),
		chromedptest.AllocatorOptions(chromedp.Flag("ignore-certificate-errors", true)))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	evalJSON(t, ctx, prefsJS+`.put('theme', { theme: 'dark' })`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}
	// document.cookie never reveals the attributes, so read the
	// browser's own cookie store.
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = storage.GetCookies().Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	want := "gofastr.local." + url.PathEscape("e2e.prefs.theme")
	for _, c := range cookies {
		if c.Name != want {
			continue
		}
		if !c.Secure {
			t.Fatalf("%s is not Secure — a mirrored record must not ride a plain-http request", c.Name)
		}
		return
	}
	t.Fatalf("the mirror cookie %q was never written: %v", want, cookies)
}

// Every mirrored collection rides the Cookie header on EVERY request.
// The Go declaration bounds what a store may ask for; the browser
// bounds what it holds, because component encoding is not
// free. Past the budget the origin starts answering 431 and the app is
// unreachable from that browser until the user clears cookies by hand,
// so the mirror write is refused, loudly and without losing the
// record.
func TestE2E_TheMirrorRefusesToOverfillTheCookieHeader(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	// Spaces encode to %20, so each record costs the header about three
	// times what it costs the store: four of them cannot fit 4 KiB.
	var res map[string]any
	var refusedAt = -1
	for i := range 4 {
		evalJSON(t, ctx, fmt.Sprintf(prefsJS+`.put('k%d', { theme: ' '.repeat(400) })`, i), &res)
		if ok, _ := res["ok"].(bool); !ok {
			t.Fatalf("put %d = %v — the record fits the collection's caps; only its cookie does not", i, res)
		}
		var have bool
		evalJSON(t, ctx, fmt.Sprintf(
			`Promise.resolve(('; ' + document.cookie).indexOf('; gofastr.local.' + encodeURIComponent('e2e.prefs.k%d') + '=') >= 0)`, i), &have)
		if !have && refusedAt < 0 {
			refusedAt = i
		}
	}
	if refusedAt < 0 {
		var all string
		evalJSON(t, ctx, `Promise.resolve(document.cookie.length + '')`, &all)
		t.Fatalf("every mirror cookie was written (%s bytes of Cookie header) — nothing bounds the header", all)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	for _, d := range errs {
		if d["reason"] == "mirror" {
			// And the record itself survived: only the shortcut to first
			// paint was refused.
			var got e2ePrefs
			evalJSON(t, ctx, fmt.Sprintf(prefsJS+`.get('k%d').then((v) => v || null)`, refusedAt), &got)
			if len(got.Theme) != 400 {
				t.Fatalf("the record is %q — a refused mirror must not lose the record", got.Theme)
			}
			return
		}
	}
	t.Fatalf("gofastr:local-error never said \"mirror\": %v", errs)
}

// A logout by full navigation lands on whatever page the app redirects
// to, and that page need not carry a store marker. The clear bit was
// honoured only inside openStore, so such a page dropped it on the
// floor, and the bit expired five minutes later, with the previous
// user's records still in the browser.
func TestE2E_ClearOnNextLoadIsHonouredWithoutAStoreMarker(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({ id: 'a', title: 'the previous user' })`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}

	// Log out, then land on a page that declares the store but renders
	// no marker for it.
	if err := chromedp.Run(ctx, chromedp.Navigate(e.srv.URL+"/logout")); err != nil {
		t.Fatal(err)
	}
	openPage(t, ctx, e.srv.URL+"/nomarker")

	if !pollTrue(ctx, `window.__gofastr.local.get('local.e2e.drafts:a').then((v) => v === undefined)`) {
		var got e2eDraft
		evalJSON(t, ctx, `window.__gofastr.local.get('local.e2e.drafts:a').then((v) => v || null)`, &got)
		t.Fatalf("the record is still there (%v) — a logout must not depend on the next page carrying a marker", got)
	}
}

// The upload bridge fails closed.
//
// A trigger that declares records is promising the handler those
// records. When they cannot be attached, because the store is not
// declared, the body is not one the field can ride on, the gather
// failed, or they are past the bound the declaration itself implies,
// the request is not sent at all. Sending it anyway gave the handler
// something that looks complete and is not, and the page never heard.
func TestE2E_TheUploadRefusesRatherThanSendWithoutItsRecords(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({ id: 'current', title: 'a draft the handler is promised' })`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}
	// Squeeze the declared bound the trigger carries: the records no
	// longer fit the upload the declaration allows.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        window.__refused = [];
        window.addEventListener('gofastr:rpc-refused', (ev) => window.__refused.push(ev.detail));
        document.getElementById('up').setAttribute('data-local-max', '8');
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	before := len(e.seen)
	if err := chromedp.Run(ctx, chromedp.Click(`#send`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve(window.__refused.length === 1)`) {
		t.Fatal("the oversized upload was sent anyway — the browser must refuse before the fetch")
	}
	e.mu.Lock()
	after := len(e.seen)
	e.mu.Unlock()
	if after != before {
		t.Fatalf("the endpoint was called %d times — a refused upload must not reach the server", after-before)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	for _, d := range errs {
		if d["reason"] == "size" {
			return
		}
	}
	t.Fatalf("gofastr:local-error never said \"size\": %v — a bare 413 is not something a page can act on", errs)
}

// A download op the browser refused is not silence. The response has
// already been written when the header is read, so the server believes
// it wrote what the browser would not take; the bridge is advisory by
// construction, which is why it has to say so.
func TestE2E_ARefusedDownloadOpIsReported(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	// drafts caps at three records. Fill it, including the one the
	// handler reads, so the record the response pushes back is a fourth.
	for _, id := range []string{"current", "b", "c"} {
		var res map[string]any
		evalJSON(t, ctx, fmt.Sprintf(draftsJS+`.put({ id: %q, title: 'x' })`, id), &res)
		if ok, _ := res["ok"].(bool); !ok {
			t.Fatalf("put %s = %v", id, res)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#send`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve(window.__errors.some((d) => d.reason === 'download'))`) {
		var errs []map[string]any
		evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
		t.Fatalf("the response wrote a record the browser refused and nothing said so: %v", errs)
	}
}

// The browser half of the same pair: a key the Go validator refuses
// must be a key the browser refuses, in the same unit.
func TestE2E_TheBrowserCountsKeyBytesLikeGoDoes(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	var res map[string]any
	// 200 accented characters: 200 UTF-16 units, 400 UTF-8 bytes.
	evalJSON(t, ctx, draftsJS+`.put('é'.repeat(200), { title: 'x' })`, &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("put = %v — a key the server will not read must not be written here either", res)
	}
	if res["reason"] != "key" {
		t.Fatalf("put = %v, want reason \"key\"", res)
	}
	// And a key at the cap in bytes still works.
	evalJSON(t, ctx, draftsJS+`.put('a'.repeat(256), { title: 'x' })`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put at the cap = %v — the bound is 256 bytes, not fewer", res)
	}
}

// Two puts racing each other cannot push a collection past its cap.
//
// Cap enforcement is read-then-write across two IndexedDB transactions:
// count what is stored, then add one. Fired together, both reads saw
// the same total, both decided they fit and both landed, and a page
// that writes on every keystroke fires them together all the time. The
// declared cap is then not a cap.
func TestE2E_RacingPutsCannotPassTheCollectionCap(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")

	// drafts caps at 1024 bytes across the collection; three records of
	// ~400 bytes cannot all fit. They are issued in one turn, so nothing
	// but the store's own ordering separates them.
	var results []map[string]any
	evalJSON(t, ctx, `Promise.all([
        `+draftsJS+`.put({ id: 'r1', title: 'x'.repeat(380) }),
        `+draftsJS+`.put({ id: 'r2', title: 'x'.repeat(380) }),
        `+draftsJS+`.put({ id: 'r3', title: 'x'.repeat(380) }),
    ])`, &results)

	accepted := 0
	for _, r := range results {
		if ok, _ := r["ok"].(bool); ok {
			accepted++
		}
	}
	var stored []map[string]any
	evalJSON(t, ctx, draftsJS+`.list()`, &stored)
	if len(stored) != accepted {
		t.Fatalf("%d puts said ok and %d records are stored", accepted, len(stored))
	}
	if len(stored) > 2 {
		t.Fatalf("%d records of ~400 bytes are stored in a collection capped at 1024 — the cap is read-then-write and the reads raced", len(stored))
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors)`, &errs)
	for _, d := range errs {
		if d["reason"] == "full" {
			return
		}
	}
	t.Fatalf("nothing was refused with reason \"full\": %v", errs)
}

// Forgetting the manifest route used to fail silently: localStore(app)
// answered null and the page script died on a null read with nothing
// naming the missing line. /plain serves the runtime and the modules and
// no manifest at all, the shape of an app that registered the extra
// script and not the route, or neither.
func TestE2E_AStoreWithNoManifestSaysWhichURLItWanted(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/plain")

	var out struct {
		Null   bool     `json:"null"`
		Warned []string `json:"warned"`
	}
	evalJSON(t, ctx, `(() => {
	  const seen = [];
	  const orig = console.warn;
	  console.warn = function () { seen.push(Array.prototype.map.call(arguments, String).join(' ')); };
	  const s = window.__gofastr.localStore('e2e');
	  console.warn = orig;
	  return { null: s === null, warned: seen };
	})()`, &out)

	if !out.Null {
		t.Fatal("localStore answered a store on a page with no manifest")
	}
	if len(out.Warned) != 1 {
		t.Fatalf("console.warn calls = %v, want exactly one", out.Warned)
	}
	if !strings.Contains(out.Warned[0], "e2e") || !strings.Contains(out.Warned[0], e2eSite.ScriptPath()) {
		t.Fatalf("the warning is %q — it must name the app and the manifest URL (%s)", out.Warned[0], e2eSite.ScriptPath())
	}
}

// A tab left open across a deploy: another tab has migrated and stamped
// the collection at a newer version. This tab's cached readiness is
// dropped when the version entry changes, so its next write re-checks
// the stored version, sees one above its own, and refuses instead of
// landing an old-schema record nothing would ever migrate again.
func TestE2E_ATabOpenAcrossADeployDoesNotWriteTheOldSchema(t *testing.T) {
	e := startE2E(t)
	tabA := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, tabA, e.srv.URL+"/")
	var res map[string]any
	evalJSON(t, tabA, draftsJS+`.put({id: 'before', title: 'this build'})`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("a put before the deploy = %v", res)
	}
	// The newer build, in another tab, stamps the collection past what
	// this build declares.
	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openPage(t, tabB, e.srv.URL+"/plain")
	evalJSON(t, tabB, `window.__gofastr.loadModule('local').then(() => window.__gofastr.local.set('local.e2e.drafts', {v: 3}))`, nil)
	// This tab's next write must refuse. The version write reaches it
	// through the primitive's watch, so poll rather than assert once.
	if !pollTrue(tabA, draftsJS+`.put({id: 'after', title: 'old schema'}).then(r => !r.ok)`) {
		t.Fatal("a put after a newer tab stamped the collection landed anyway: the cached readiness was never re-checked")
	}
	if !pollTrue(tabA, `window.__gofastr.local.get('local.e2e.drafts:after').then(v => v === undefined)`) {
		t.Fatal("the old-schema record is stored under the newer version")
	}
}

// A put racing a clear commits after the enumeration that never saw it
// unless both wait on the same chain. The logout path reports success
// only when the records are gone, so the chain has to cover clear and
// delete, not only put.
func TestE2E_AClearIsNotOutrunByAPut(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	for round := 0; round < 4; round++ {
		var results []map[string]any
		evalJSON(t, ctx, `Promise.all([
            `+draftsJS+`.put({id: 'racer', title: 'survives?'}),
            `+draftsJS+`.clear(),
        ])`, &results)
		if ok, _ := results[1]["ok"].(bool); !ok {
			t.Fatalf("round %d: clear = %v", round, results[1])
		}
		var n float64
		evalJSON(t, ctx, draftsJS+`.count()`, &n)
		if n != 0 {
			t.Fatalf("round %d: clear reported ok and %v record(s) survived it", round, n)
		}
	}
	var results []map[string]any
	evalJSON(t, ctx, `Promise.all([
        `+draftsJS+`.put({id: 'racer', title: 'survives?'}),
        `+draftsJS+`.delete('racer'),
    ])`, &results)
	if !pollTrue(ctx, draftsJS+`.get('racer').then(v => v === undefined)`) {
		t.Fatal("delete reported ok and the record it raced survived")
	}
}

// The bound on the trigger is bytes, the unit the server counts. A CJK
// draft is three bytes per character, so counting UTF-16 units let a
// body past the pre-flight that the server then answered with a bare
// 413.
func TestE2E_TheUploadPreFlightCountsBytesNotUnits(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	// 120 CJK characters: 120 UTF-16 units, 360 UTF-8 bytes.
	var res map[string]any
	evalJSON(t, ctx, draftsJS+`.put({ id: 'current', title: '漢'.repeat(120) })`, &res)
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("put = %v", res)
	}
	// A bound the units fit under and the bytes do not.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        window.__refused = [];
        window.addEventListener('gofastr:rpc-refused', (ev) => window.__refused.push(ev.detail));
        document.getElementById('up').setAttribute('data-local-max', '260');
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#send`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve(window.__refused.length === 1)`) {
		t.Fatal("a 360-byte payload passed a 260-byte bound: the pre-flight counted UTF-16 units")
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors.filter(d => d.reason === 'size'))`, &errs)
	if len(errs) != 1 {
		t.Fatalf("size refusals = %v", errs)
	}
	if size, _ := errs[0]["size"].(float64); size < 360 {
		t.Fatalf("the size event reports %v, want the byte count (at least 360)", size)
	}
}

// An enumeration that failed is not an empty collection. list and
// count say so the way clear and put already do.
func TestE2E_ListAndCountReportAFailedEnumeration(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, draftsJS+`.put({id: 'a', title: 'one'})`, nil)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
        window.__errors = [];
        window.__gofastr.local.entries = () => Promise.resolve({ ok: false, reason: 'unavailable', entries: [] });
        window.__gofastr.local.keys = () => Promise.resolve({ ok: false, reason: 'unavailable', keys: [] });
    })()`, nil)); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	evalJSON(t, ctx, draftsJS+`.list()`, &rows)
	var n float64
	evalJSON(t, ctx, draftsJS+`.count()`, &n)
	if len(rows) != 0 || n != 0 {
		t.Fatalf("list = %v, count = %v: a failed read still resolves empty", rows, n)
	}
	var errs []map[string]any
	evalJSON(t, ctx, `Promise.resolve(window.__errors.filter(d => d.reason === 'unavailable'))`, &errs)
	if len(errs) != 2 {
		t.Fatalf("gofastr:local-error fired %d times for two failed enumerations, want 2: %v", len(errs), errs)
	}
}

// The seed restore lands after hydration. If the user typed while the
// read was in flight, the typed value is the newer one and has already
// been written back; the read is stale and must not repaint over it.
func TestE2E_TheSeedKeepsWhatTheUserTypedWhileTheReadWasInFlight(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, draftsJS+`.put({id: 'current', title: 'stored yesterday'})`, nil)
	// The seed restores on a scan, so ask for one the way a navigation
	// would.
	evalJSON(t, ctx, `Promise.resolve(window.__gofastr._moduleScanners['local-bridge'](document))`, nil)
	if !pollTrue(ctx, `Promise.resolve(document.getElementById('seeded').textContent.indexOf('stored yesterday') >= 0)`) {
		t.Fatal("the seed never restored the stored record")
	}
	// Slow the record read, re-scan so the seed reads again, and type
	// while the read is still in flight.
	var title string
	evalJSON(t, ctx, `(() => {
        const c = `+draftsJS+`;
        // The read happens now and its answer lands later: a stale
        // read, the way a slow disk delivers one.
        const real = c.get.bind(c);
        c.get = (k) => real(k).then((v) => new Promise((resolve) => setTimeout(() => resolve(v), 400)));
        window.__gofastr._moduleScanners['local-bridge'](document);
        return new Promise((resolve) => setTimeout(() => {
            window.__gofastr.setSignal('e2elocal.current', {id: 'current', title: 'typed now'});
            setTimeout(() => resolve(window.__gofastr._signals['e2elocal.current'].value.title), 700);
        }, 100));
    })()`, &title)
	if title != "typed now" {
		t.Fatalf("after the stale read landed the signal says %q, want the value the user typed", title)
	}
	if !pollTrue(ctx, draftsJS+`.get('current').then(v => !!v && v.title === 'typed now')`) {
		t.Fatal("the typed value was not written back to the record")
	}
}

// Two tabs writing at once: the per-document chain cannot see the other
// tab, so the write chain also holds a Web Lock on the collection. Six
// puts of ~310 bytes from two tabs into a collection capped at three
// records and 1024 bytes cannot all land.
func TestE2E_TwoTabsCannotPushACollectionPastItsCap(t *testing.T) {
	e := startE2E(t)
	tabA := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, tabA, e.srv.URL+"/")
	tabB, cancelB := chromedp.NewContext(tabA)
	t.Cleanup(cancelB)
	openPage(t, tabB, e.srv.URL+"/")
	// Both tabs fire at one wall-clock instant, so the two bursts
	// overlap instead of running one after the other.
	fire := func(tab context.Context, prefix string, at int64) {
		if err := chromedp.Run(tab, chromedp.Evaluate(`window.__burst = new Promise((resolve) => setTimeout(() => resolve(Promise.all(
            Array.from({length: 8}, (_, i) => `+draftsJS+`.put({ id: '`+prefix+`' + i, title: 'x'.repeat(290) }))
        )), Math.max(0, `+strconv.FormatInt(at, 10)+` - Date.now())))`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	// A race is a probability, so run it several times and refuse any
	// overrun.
	for round := 0; round < 10; round++ {
		evalJSON(t, tabA, draftsJS+`.clear()`, nil)
		at := time.Now().Add(400 * time.Millisecond).UnixMilli()
		fire(tabA, "a", at)
		fire(tabB, "b", at)
		evalJSON(t, tabA, `window.__burst`, nil)
		evalJSON(t, tabB, `window.__burst`, nil)
		var n float64
		evalJSON(t, tabA, draftsJS+`.count()`, &n)
		if n > 3 {
			t.Fatalf("round %d: %v records stored in a collection capped at 3: two tabs raced the cap", round, n)
		}
		var total float64
		evalJSON(t, tabA, `window.__gofastr.local.entries('local.e2e.drafts:').then(er => er.entries.reduce((s, e) => s + e.size, 0))`, &total)
		if total > 1024 {
			t.Fatalf("round %d: %v bytes stored in a collection capped at 1024", round, total)
		}
	}
}

// The FormData arm of the upload bridge: a multipart form (a file input
// on it) makes the runtime post FormData rather than JSON, the bridge
// appends __local as a form field, and the wrapper lifts it out of the
// parsed form so the handler sees the record through local.Get and the
// form's own fields with the reserved one gone.
func TestE2E_TheUploadRidesAMultipartForm(t *testing.T) {
	e := startE2E(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	openPage(t, ctx, e.srv.URL+"/")
	evalJSON(t, ctx, `Promise.all([
        `+draftsJS+`.put({id: 'current', title: 'multipart draft'}),
        `+prefsJS+`.put('theme', {theme: 'dark'}),
    ])`, nil)
	before := len(e.seen)
	if err := chromedp.Run(ctx, chromedp.Click(`#send-multi`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Promise.resolve((document.getElementById('result').textContent || '').indexOf('ok') >= 0)`) {
		t.Fatal("the multipart upload RPC never answered")
	}
	e.mu.Lock()
	after := len(e.seen)
	e.mu.Unlock()
	if after != before+1 {
		t.Fatalf("the endpoint was called %d times", after-before)
	}
	seen := e.last(t)
	if seen.Found != SourceUpload || seen.Current.Title != "multipart draft" {
		t.Fatalf("Get(current) = %+v found=%v; the record must arrive through the FormData field", seen.Current, seen.Found)
	}
	if seen.Note != "multi" {
		t.Fatalf("the form's own field reached the handler as %q", seen.Note)
	}
	if seen.LeakedField {
		t.Fatal("the reserved field survived the strip on a multipart body")
	}
	if seen.Theme != "dark" {
		t.Fatalf("the mirrored prefs record did not arrive: theme=%q", seen.Theme)
	}
}
