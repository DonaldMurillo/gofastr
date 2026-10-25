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

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// fetch follows a 3xx itself, so a runtime request whose answer was a
// redirect resolves with a 200 for some other URL; resp.redirected is the
// only sign (#577). Every test here has the server answer one runtime
// caller's request with a 303 to a same-origin URL whose body that caller
// could otherwise read as its own answer, and pins the caller's rule: a
// redirected answer is no answer (passive and background fetches), or the
// action failed and the reader goes to the target through the navigator
// (user actions). The navigators, prefetch and the intercept overlay pin
// the same rule in their own tests.

// raSite serves the full runtime, every demand module, the page at "/",
// a sign-in page at /login (the target an expired session redirects to),
// and per-test routes, counting requests per path.
type raSite struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func (s *raSite) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// waitHits blocks until path has been requested at least n times.
func (s *raSite) waitHits(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for s.count(path) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%s requested %d times, want at least %d", path, s.count(path), n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const raRoutes = `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/login"}]</script>`

// raDoc is a whole document on the full runtime. head lands before the
// runtime script, so a test can set globals the runtime reads at boot.
func raDoc(title, head, main string) string {
	return `<!doctype html><html><head><title>` + title + `</title>` + raRoutes + head +
		`</head><body><main role="main" tabindex="-1">` + main + `</main>` + toastStackHTML() +
		`<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`
}

func raSeeOther(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

func raJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

func raHTML(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
}

func newRASite(t *testing.T, js, page string, routes map[string]http.HandlerFunc) *raSite {
	t.Helper()
	s := &raSite{hits: map[string]int{}}
	counted := func(path string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.hits[path]++
			s.mu.Unlock()
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	if _, ok := routes["/login"]; !ok {
		mux.HandleFunc("/login", counted("/login", raHTML(raDoc("Sign in", "", `<h1 id="login">Sign in</h1>`))))
	}
	for path, h := range routes {
		mux.HandleFunc(path, counted(path, h))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		raHTML(page)(w, r)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func raFullJS(t *testing.T) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func raAwait(expr string, out any) chromedp.Action {
	return chromedp.Evaluate(expr, out, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})
}

// raCapture records toasts and the page's identity: __marker survives a
// client-side navigation and dies with a document load.
const raCapture = `window.__marker = 'kept';
window.__toasts = [];
window.__gofastr._toastOrFallback = (o) => { window.__toasts.push(o); };`

func raOpen(t *testing.T, s *raSite) (ctx context.Context) {
	t.Helper()
	ctx = chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadModule)`, nil),
		chromedp.Evaluate(raCapture, nil),
	); err != nil {
		t.Fatalf("open: %v", err)
	}
	return ctx
}

type raState struct {
	Path    string   `json:"path"`
	Marker  string   `json:"marker"`
	Toasts  []string `json:"toasts"`
	Login   bool     `json:"login"`
	Results []bool   `json:"results"`
	Note    string   `json:"note"`
	Signal  string   `json:"signal"`
	Events  []string `json:"events"`
}

const raStateExpr = `JSON.stringify({
	path: location.pathname,
	marker: String(window.__marker),
	toasts: (window.__toasts || []).map((o) => String(o && o.title)),
	login: !!document.getElementById('login'),
	results: window.__results || [],
	note: document.getElementById('note') ? document.getElementById('note').value : '',
	signal: JSON.stringify(window.__gofastr._signals && window.__gofastr._signals.out ? window.__gofastr._signals.out.value : null) || '',
	events: window.__ev || [],
})`

func raRead(t *testing.T, ctx context.Context) raState {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(raStateExpr, &raw)); err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st raState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("state %q: %v", raw, err)
	}
	return st
}

// An RPC form whose session expired: the action endpoint redirects to the
// sign-in page. The save did not happen, so nothing a success does may run:
// the form keeps what the reader typed (no data-cui-rpc-reset), no success
// toast, gofastr:formresult reports ok:false (a leave guard puts the dirt
// back), and the reader is told; the form stays on the page with its input.
func TestRedirectedRPCFormKeepsInput(t *testing.T) {
	page := raDoc("Home", "", `<form id="f" data-cui-rpc="/api/save" data-cui-rpc-reset data-cui-rpc-success-toast="Saved">`+
		`<input id="note" name="note"><button id="go" type="submit">Save</button></form>`)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{"/api/save": raSeeOther("/login")})
	ctx := raOpen(t, s)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__results = [];
document.addEventListener('gofastr:formresult', (e) => window.__results.push(e.detail.ok));
document.getElementById('note').value = 'half a novel';
document.getElementById('go').click();`, nil),
		chromedp.Poll(`window.__results.length > 0`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	st := raRead(t, ctx)
	if s.count("/login") != 1 {
		t.Errorf("/login requested %d times, want 1 (the followed redirect)", s.count("/login"))
	}
	if len(st.Results) != 1 || st.Results[0] {
		t.Errorf("gofastr:formresult = %v, want [false]: a redirected answer is not a committed save", st.Results)
	}
	if st.Note != "half a novel" {
		t.Errorf("the reader's input = %q after a redirected save, want it kept", st.Note)
	}
	for _, title := range st.Toasts {
		if title == "Saved" {
			t.Errorf("toasts = %q: the success toast fired for a redirected save", st.Toasts)
		}
	}
	if len(st.Toasts) == 0 {
		t.Error("no toast: a redirected save must tell the reader it did not complete")
	}
	if st.Path != "/" || st.Marker != "kept" {
		t.Errorf("path %q marker %q: the form must stay on its page", st.Path, st.Marker)
	}
}

// An RPC button (no form) whose endpoint redirects: the action failed,
// so no success toast and the response signal does not take the sign-in
// page's HTML; the reader is handed to the redirect target through the
// client navigator (no document load: __marker survives).
func TestRedirectedRPCButtonHandsOffToNavigator(t *testing.T) {
	page := raDoc("Home", "", `<button id="b" type="button" data-cui-rpc="/api/act" data-cui-rpc-signal="out" data-cui-rpc-success-toast="Done">Act</button>`)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{"/api/act": raSeeOther("/login")})
	ctx := raOpen(t, s)
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('b').click()`, nil),
		chromedp.Poll(`location.pathname === '/login' && !!document.getElementById('login')`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
	)
	st := raRead(t, ctx)
	if err != nil {
		t.Fatalf("the reader never reached /login through the navigator: %v (state %+v)", err, st)
	}
	if st.Marker != "kept" {
		t.Errorf("marker %q: the hand-off loaded a document instead of navigating client-side", st.Marker)
	}
	for _, title := range st.Toasts {
		if title == "Done" {
			t.Errorf("toasts = %q: the success toast fired for a redirected action", st.Toasts)
		}
	}
	if strings.Contains(st.Signal, "Sign in") {
		t.Errorf("signal out = %s: the redirect target's body became the action's answer", st.Signal)
	}
}

// An optimistic action (action.js, behind OptimisticAction/ToggleAction)
// whose endpoint redirects rolls back, never commits, and hands the reader
// to the redirect target.
func TestRedirectedOptimisticActionRollsBack(t *testing.T) {
	page := raDoc("Home", "", `<button id="t" type="button"><span id="idle">Follow</span><span id="done" hidden>Following</span></button>`)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{"/api/toggle": raSeeOther("/login")})
	ctx := raOpen(t, s)
	if err := chromedp.Run(ctx,
		raAwait(`window.__gofastr.loadModule('action').then(() => {
	const b = document.getElementById('t');
	window.__ev = [];
	for (const n of ['action:committed', 'action:rolled-back']) b.addEventListener(n, () => window.__ev.push(n));
	window.__gofastr.action.bind(b, { endpoint: '/api/toggle', idle: document.getElementById('idle'), done: document.getElementById('done') });
	b.click();
	return true;
})`, nil),
		chromedp.Poll(`window.__ev.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("click: %v", err)
	}
	st := raRead(t, ctx)
	if len(st.Events) != 1 || st.Events[0] != "action:rolled-back" {
		t.Fatalf("events = %v, want [action:rolled-back]: a redirected answer is not a commit", st.Events)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`location.pathname === '/login' && !!document.getElementById('login')`, nil,
		chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("the reader was not handed to /login: %v (state %+v)", err, raRead(t, ctx))
	}
	if st := raRead(t, ctx); st.Marker != "kept" {
		t.Errorf("marker %q: the hand-off loaded a document instead of navigating client-side", st.Marker)
	}
}

// A data-cui-poll region whose source redirects keeps its content: the
// redirect target's body is never swapped in.
func TestRedirectedPollSwapsNothing(t *testing.T) {
	page := raDoc("Home", "", `<div id="region" data-cui-poll="5s" data-cui-poll-src="/fresh">stale</div>`)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{
		"/fresh":     raSeeOther("/elsewhere"),
		"/elsewhere": raHTML(`<span>REDIRECTED</span>`),
	})
	ctx := raOpen(t, s)
	s.waitHits(t, "/elsewhere", 1)
	time.Sleep(500 * time.Millisecond)
	if got := readText(t, ctx, `#region`); got != "stale" {
		t.Errorf("polled region = %q after a redirected poll, want it untouched", got)
	}
}

// raWidget is a catalog entry for one SSR-inlined widget bound to the
// count signal.
func raWidgetCatalog(pollMs int) string {
	poll := ""
	if pollMs > 0 {
		poll = fmt.Sprintf(`,"pollMs":%d`, pollMs)
	}
	return `[{"hidden":false,"cfg":{"name":"ticker","position":"bottom-right","stylePath":"/w/ticker.css",` +
		`"chromePath":"/w/ticker","statePath":"/w/state"` + poll + `}}]`
}

const raWidgetMarkup = `<div class="cui-widget cui-pos-bottom-right" data-cui-widget="ticker" role="status"><span id="count" data-cui-signal="count">0</span></div>`

const raCountExpr = `String(window.__gofastr._signals && window.__gofastr._signals.count ? window.__gofastr._signals.count.value : '')`

// A widget's mount hydration whose state endpoint redirects sets no
// signal from the redirect target's JSON.
func TestRedirectedWidgetStateSetsNothing(t *testing.T) {
	page := raDoc("Home", "", raWidgetMarkup)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": raJSON(raWidgetCatalog(0)),
		"/w/state":           raSeeOther("/w/other"),
		"/w/other":           raJSON(`{"count":99}`),
	})
	ctx := raOpen(t, s)
	s.waitHits(t, "/w/other", 1)
	time.Sleep(500 * time.Millisecond)
	var count string
	if err := chromedp.Run(ctx, chromedp.Evaluate(raCountExpr, &count)); err != nil {
		t.Fatal(err)
	}
	if count == "99" {
		t.Errorf("count signal = %s: mount hydration took the redirect target's state", count)
	}
}

// A widget poll (Builder.Poll) whose state endpoint starts redirecting
// keeps the last real state: the redirect target's JSON never overwrites.
func TestRedirectedWidgetPollSetsNothing(t *testing.T) {
	page := raDoc("Home", "", raWidgetMarkup)
	var mu sync.Mutex
	first := true
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": raJSON(raWidgetCatalog(1000)),
		"/w/state": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			real := first
			first = false
			mu.Unlock()
			if real {
				raJSON(`{"count":1}`)(w, r)
				return
			}
			raSeeOther("/w/other")(w, r)
		},
		"/w/other": raJSON(`{"count":99}`),
	})
	ctx := raOpen(t, s)
	s.waitHits(t, "/w/other", 2)
	time.Sleep(300 * time.Millisecond)
	var count string
	if err := chromedp.Run(ctx, chromedp.Evaluate(raCountExpr, &count)); err != nil {
		t.Fatal(err)
	}
	if count != "1" {
		t.Errorf("count signal = %q after redirected polls, want the last real state 1", count)
	}
}

// A widget whose chrome endpoint redirects does not open with the
// redirect target's markup; the open fails the way a failed chrome fetch
// does (the panel_failed toast).
func TestRedirectedWidgetChromeMountsNothing(t *testing.T) {
	page := raDoc("Home", "", `<button id="open" type="button" data-cui-open="panel">Open</button>`)
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": raJSON(`[{"hidden":true,"cfg":{"name":"panel","position":"center","stylePath":"/w/panel.css","chromePath":"/w/chrome"}}]`),
		"/w/chrome":          raSeeOther("/w/chrome2"),
		"/w/chrome2":         raHTML(`<div class="cui-widget cui-pos-center" data-cui-widget="panel" role="dialog"><p id="stolen">STOLEN</p></div>`),
	})
	ctx := raOpen(t, s)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!window.__gofastr._widgetCatalog && !!window.__gofastr._widgetCatalog.panel`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('open').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	s.waitHits(t, "/w/chrome2", 1)
	time.Sleep(500 * time.Millisecond)
	var stolen bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.getElementById('stolen')`, &stolen)); err != nil {
		t.Fatal(err)
	}
	if stolen {
		t.Error("the widget opened with the redirect target's chrome")
	}
	if st := raRead(t, ctx); len(st.Toasts) == 0 {
		t.Error("no toast: a redirected chrome fetch is a failed open and says so")
	}
}

// raDiscoveryRoutes answers the catalog request with a redirect to a
// catalog naming one auto-mounting widget per page (?page=), and counts
// chrome fetches of those widgets.
func raDiscoveryRoutes(catalogPath string) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		catalogPath: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/w/catalog?page="+r.URL.Query().Get("page"), http.StatusSeeOther)
		},
		"/w/catalog": func(w http.ResponseWriter, r *http.Request) {
			name := "boot"
			if r.URL.Query().Get("page") == "/next" {
				name = "nav"
			}
			raJSON(`[{"hidden":false,"cfg":{"name":"intruder-`+name+`","position":"bottom-right",`+
				`"stylePath":"/w/intruder.css","chromePath":"/w/intruder-`+name+`"}}]`)(w, r)
		},
		"/w/intruder-boot": raHTML(`<div class="cui-widget" data-cui-widget="intruder-boot" role="status">X</div>`),
		"/w/intruder-nav":  raHTML(`<div class="cui-widget" data-cui-widget="intruder-nav" role="status">X</div>`),
	}
}

// Widget discovery (widgets-boot) at boot and on gofastr:navigate: a
// redirected catalog mounts none of the widgets the redirect target lists.
func TestRedirectedWidgetDiscoveryMountsNothing(t *testing.T) {
	s := newRASite(t, raFullJS(t), raDoc("Home", "", ""), raDiscoveryRoutes("/__gofastr/widgets"))
	ctx := raOpen(t, s)
	s.waitHits(t, "/w/catalog", 1)
	time.Sleep(700 * time.Millisecond)
	if n := s.count("/w/intruder-boot"); n != 0 {
		t.Errorf("boot discovery mounted the redirect target's widget (%d chrome fetches)", n)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: '/next' } }))`, nil)); err != nil {
		t.Fatal(err)
	}
	s.waitHits(t, "/w/catalog", 2)
	time.Sleep(700 * time.Millisecond)
	if n := s.count("/w/intruder-nav"); n != 0 {
		t.Errorf("navigate re-discovery mounted the redirect target's widget (%d chrome fetches)", n)
	}
}

// The static composition's discovery (widgets-boot-static) reads the
// dumped catalog; a redirected one mounts nothing either.
func TestRedirectedStaticWidgetDiscoveryMountsNothing(t *testing.T) {
	js, err := StaticJS()
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(raDoc("Home", "", ""), "<html>", "<html data-cui-static>", 1)
	s := newRASite(t, js, page, raDiscoveryRoutes("/__gofastr/widgets.json"))
	raOpen(t, s)
	s.waitHits(t, "/w/catalog", 1)
	time.Sleep(700 * time.Millisecond)
	if n := s.count("/w/intruder-boot"); n != 0 {
		t.Errorf("static discovery mounted the redirect target's widget (%d chrome fetches)", n)
	}
}

// Kernel fetchPage: a redirected partial is not injected and not returned.
func TestRedirectedFetchPageInjectsNothing(t *testing.T) {
	s := newRASite(t, raFullJS(t), raDoc("Home", "", `<div id="slot">original</div>`), map[string]http.HandlerFunc{
		"/frag": raSeeOther("/login"),
	})
	ctx := raOpen(t, s)
	var got string
	if err := chromedp.Run(ctx, raAwait(`window.__gofastr.fetchPage('/frag', '#slot')`, &got)); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("fetchPage returned %d bytes of the redirect target, want ''", len(got))
	}
	if slot := readText(t, ctx, `#slot`); slot != "original" {
		t.Errorf("#slot = %q, want it untouched", slot)
	}
}

// Kernel server actions: a redirected /__gofastr/action answer is a
// failed action (null), never the redirect target's JSON.
func TestRedirectedServerActionIsNull(t *testing.T) {
	s := newRASite(t, raFullJS(t), raDoc("Home", "", ""), map[string]http.HandlerFunc{
		"/__gofastr/action": raSeeOther("/w/action2"),
		"/w/action2":        raJSON(`{"ok":true,"data":"STOLEN"}`),
	})
	ctx := raOpen(t, s)
	var got string
	if err := chromedp.Run(ctx, raAwait(`window.__gofastr._serverActionFor('c1', 'save', {}).then((r) => JSON.stringify(r))`, &got)); err != nil {
		t.Fatal(err)
	}
	if got != "null" {
		t.Errorf("server action result = %s, want null for a redirected answer", got)
	}
}

// A deferred part whose request redirects applies nothing to its region
// and is not cached: the redirect target here (/c, answering the same
// part header) renders a valid fill for the same address.
func TestRedirectedPartAppliesNothing(t *testing.T) {
	s := newPartsSite(t, 10000)
	s.setPartResp("/a|aside", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/c", http.StatusSeeOther)
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
	deadline := time.Now().Add(10 * time.Second)
	for s.count("part", "/c|aside") < 1 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-C-ASIDE") {
		t.Fatalf("aside = %q: the redirected part applied the target's fill", got)
	}
	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	time.Sleep(300 * time.Millisecond)
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-C-ASIDE") {
		t.Errorf("aside on return = %q: the redirected part was cached under /a", got)
	}
	if n := s.count("part", "/a|aside"); n != 2 {
		t.Errorf("aside part requests = %d, want 2 (a redirected part is not cached)", n)
	}
}

// The SSE module's session re-mint (POST /__gofastr/session, the bus's
// bootstrap of a fresh stream id) ignores a redirected answer: the
// target's JSON never becomes the stream id.
func TestRedirectedSessionRemintIgnored(t *testing.T) {
	mux := map[string]http.HandlerFunc{
		"/__gofastr/sse": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unknown session", http.StatusUnauthorized)
		},
		"/__gofastr/session": raSeeOther("/w/session2"),
		"/w/session2":        raJSON(`{"sessionId":"sess-STOLEN"}`),
	}
	page := `<!doctype html><html><head><meta name="gofastr-sse" content="/__gofastr/sse?session=sess-DEAD"></head>` +
		`<body><div data-island="recover"></div><span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`
	s := newRASite(t, raFullJS(t), page, mux)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	s.waitHits(t, "/w/session2", 1)
	time.Sleep(500 * time.Millisecond)
	var meta string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]').getAttribute('content')`, &meta)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(meta, "STOLEN") {
		t.Errorf("stream meta = %q: the re-mint took the redirect target's session id", meta)
	}
}

// The desktop bridge's call rejects a redirected answer instead of
// resolving with the redirect target's result.
func TestRedirectedDesktopCallRejects(t *testing.T) {
	page := raDoc("Home", `<script>window.__gofastr_desktop = {};</script>`, "")
	s := newRASite(t, raFullJS(t), page, map[string]http.HandlerFunc{
		"/__gofastr/desktop/call/clip/read": raSeeOther("/w/desk2"),
		"/w/desk2":                          raJSON(`{"ok":true,"result":"STOLEN"}`),
	})
	ctx := raOpen(t, s)
	var got string
	if err := chromedp.Run(ctx, raAwait(`window.__gofastr.loadModule('desktop')
	.then(() => window.__gofastr.desktop.call('clip', 'read', {}))
	.then((v) => 'resolved:' + JSON.stringify(v), (e) => 'rejected:' + e.code)`, &got)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "rejected:") {
		t.Errorf("desktop call = %s, want a rejection for a redirected answer", got)
	}
}

// The embed frame's handshake: a redirected exchange grants nothing and a
// redirected content request injects nothing; both fail the panel.
func TestRedirectedEmbedHandshakeFails(t *testing.T) {
	embedJS, err := EmbedJS()
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, routes map[string]http.HandlerFunc) (*raSite, string) {
		routes["/e.js"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, embedJS)
		}
		routes["/frame"] = raHTML(`<!doctype html><html><head>` +
			`<meta name="gofastr-embed" content='{"surface":"s","exchange":"/x","content":"/c"}'></head>` +
			`<body><div id="gofastr-embed-root" data-cui-embed-state="loading"></div><script src="/e.js"></script></body></html>`)
		s := newRASite(t, raFullJS(t), `<!doctype html><html><body><iframe id="fr" src="/frame"></iframe></body></html>`, routes)
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		var state string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.Poll(`(() => { const d = document.getElementById('fr').contentDocument; return !!d && !!d.getElementById('gofastr-embed-root') && !!d.defaultView.__gofastr; })()`, nil,
				chromedp.WithPollingTimeout(10*time.Second)),
			chromedp.Evaluate(`document.getElementById('fr').contentWindow.postMessage({ proto: 'gofastr-embed/1', type: 'token', token: 'nonce' }, '*')`, nil),
			chromedp.Poll(`document.getElementById('fr').contentDocument.getElementById('gofastr-embed-root').getAttribute('data-cui-embed-state') !== 'loading'`, nil,
				chromedp.WithPollingTimeout(20*time.Second)),
			chromedp.Evaluate(`document.getElementById('fr').contentDocument.getElementById('gofastr-embed-root').getAttribute('data-cui-embed-state')`, &state),
		); err != nil {
			t.Fatal(err)
		}
		return s, state
	}
	t.Run("exchange", func(t *testing.T) {
		s, state := run(t, map[string]http.HandlerFunc{
			"/x":  raSeeOther("/x2"),
			"/x2": raJSON(`{"grant":"G-STOLEN","expires_in_ms":600000}`),
			"/c":  raHTML(`<p>CONTENT</p>`),
		})
		if state != "error" || s.count("/c") != 0 {
			t.Errorf("state %q with %d content requests: a redirected exchange must grant nothing", state, s.count("/c"))
		}
	})
	t.Run("content", func(t *testing.T) {
		_, state := run(t, map[string]http.HandlerFunc{
			"/x":  raJSON(`{}`),
			"/c":  raSeeOther("/c2"),
			"/c2": raHTML(`<p id="stolen">STOLEN</p>`),
		})
		if state != "error" {
			t.Errorf("state %q: a redirected content request must fail the panel, not inject the target", state)
		}
	})
}
