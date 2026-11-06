package runtime

// Browser tests for the navigation-guard contract (src/navguard.js):
// one ordered list of guards that every managed departure asks before
// anything the move discards is lost. The rig is a hand-rolled server
// in the beforenavigate shape: four pages under one layout, each with
// an input the guard checkpoints, a durable /checkpoint endpoint that
// answers late on purpose, and an order log the tests read to prove the
// checkpoint finished before the destination was fetched.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// ngSource is the runtime the rig serves: the core bundle under "", a
// split module by name.
var ngSource = func(name string) (string, bool) {
	if name == "" {
		js, err := RuntimeJS()
		return js, err == nil
	}
	return Module(name)
}

func ngServeRuntime(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	if _, ok := ngSource(""); !ok {
		t.Fatal("runtime did not compose")
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		js, _ := ngSource("")
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		src, ok := ngSource(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
}

type ngRig struct {
	srv      *httptest.Server
	envelope bool
	pick     string
	ckDelay  atomic.Int64
	mu       sync.Mutex
	log      []string
}

func (r *ngRig) note(s string) {
	r.mu.Lock()
	r.log = append(r.log, s)
	r.mu.Unlock()
}

func (r *ngRig) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

// indexOf is the position of the first log line with the prefix, -1
// when none.
func (r *ngRig) indexOf(prefix string) int {
	for i, e := range r.events() {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

const ngRoutes = `[{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]},` +
	`{"path":"/c","layouts":["l:site"]},{"path":"/d","layouts":["l:site"]},` +
	`{"path":"/moved","layouts":["l:site"]},{"path":"/result","layouts":["l:site"]},` +
	`{"path":"/doc","layouts":["l:site"],"docScripts":["/doc.js"]}]`

// ngInner is a page's own content: its landmark section, the input a
// guard checkpoints, and enough height to scroll.
func ngInner(path string) string {
	n := strings.Trim(path, "/")
	return `<section id="page-` + n + `"><h1>` + strings.ToUpper(n) + `</h1>` +
		`<input id="note-` + n + `" aria-label="note ` + n + `">` +
		`<form id="find" method="get" action="/result" data-cui-spa><input name="q" value="files" aria-label="query"><button id="find-go">Find</button></form>` +
		`<form id="post" method="post" action="/post" data-cui-spa><button id="post-go">Post</button></form>` +
		`<div style="block-size:3000px"></div><p id="end-` + n + `">end</p></section>`
}

func newNGRig(t *testing.T, envelope bool) *ngRig {
	t.Helper()
	r := &ngRig{envelope: envelope}
	r.ckDelay.Store(600)
	outlet := ""
	if envelope {
		outlet = `<aside data-cui-outlet="l:site#aside" id="aside">ASIDE</aside>`
	}
	doc := func(path string) string {
		return `<!doctype html><html lang="en" data-cui-vt-kinds="fade slide"><head><title>guards</title>` +
			`<script type="application/json" id="gofastr-routes">` + ngRoutes + `</script></head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="toA" href="/a">A</a> <a id="toB" href="/b">B</a> <a id="toC" href="/c">C</a> ` +
			`<a id="toD" href="/d">D</a> <a id="toMoved" href="/moved">moved</a> ` +
			`<a id="toOff" href="/c" data-cui-nav="off">C, full load</a></nav>` + outlet +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + ngInner(path) + `</main>` +
			`</div><span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	ngServeRuntime(t, mux)
	mux.HandleFunc("/checkpoint", func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		time.Sleep(time.Duration(r.ckDelay.Load()) * time.Millisecond)
		r.note("checkpoint:" + string(b))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/post", func(w http.ResponseWriter, req *http.Request) {
		r.note("post")
		http.Redirect(w, req, "/c", http.StatusSeeOther)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		if path == "/" {
			http.Redirect(w, req, "/a", http.StatusFound)
			return
		}
		if path == "/moved" {
			r.note("moved")
			http.Redirect(w, req, "/b", http.StatusSeeOther)
			return
		}
		if req.Header.Get("X-Gofastr-Navigate") != "1" {
			r.note("full " + path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, doc(path))
			return
		}
		r.note("partial " + req.URL.RequestURI())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", strings.ToUpper(strings.Trim(path, "/")))
		w.Header().Set("X-Gofastr-Swap", "l:site")
		if r.pick != "" {
			w.Header().Set("X-Gofastr-Transition", r.pick)
		}
		if r.envelope {
			w.Header().Set("X-Gofastr-Envelope", "2")
			fmt.Fprintf(w, `<template data-cui-fill="l:site">%s</template><template data-cui-fill="l:site#aside">ASIDE %s</template>`, ngInner(path), path)
			return
		}
		fmt.Fprint(w, ngInner(path))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// ngSetupJS loads the module and registers one guard whose behaviour
// window.__ngMode picks:
//
//	checkpoint  POST the leaving page's draft to /checkpoint, allow once stored
//	allow       allow synchronously
//	later       allow after 150 ms
//	micro       allow on a microtask (async function)
//	deny        deny after 100 ms
//	throw       throw synchronously
//	reject      reject after 50 ms
//	hold        park the decision in __ng.held, in order, for the test to settle
//
// Every call is logged with the attempt's fields; gofastr:navigateblocked
// and gofastr:navigate are logged too.
const ngSetupJS = `(async () => {
  window.__ng = { calls: [], blocked: [], landed: [], held: [], atCheckpoint: [], stamp: 'alive' };
  window.__ngMode = window.__ngMode || 'checkpoint';
  addEventListener('gofastr:navigateblocked', (e) => __ng.blocked.push({
    reason: e.detail.reason, id: e.detail.attempt.id, intent: e.detail.attempt.intent,
    error: e.detail.error ? String(e.detail.error.message || e.detail.error) : '',
  }));
  addEventListener('gofastr:navigate', () => __ng.landed.push(location.pathname + location.search));
  await __gofastr.loadModule('navguard');
  const draft = () => document.querySelector('main input')?.value ?? '';
  window.__stop = __gofastr.guardNavigation((nav) => {
    __ng.calls.push({
      id: nav.id, intent: nav.intent, from: new URL(nav.from.url).pathname, to: new URL(nav.to.url).pathname + new URL(nav.to.url).search,
      fromEntry: nav.from.entry, toEntry: nav.to.entry, delta: nav.delta, frozen: Object.isFrozen(nav),
      aborted: nav.signal.aborted,
      affectsNote: !!document.getElementById('rec-note') && nav.affects(document.getElementById('rec-note')),
      affectsList: !!document.getElementById('list-main') && nav.affects(document.getElementById('list-main')),
    });
    switch (__ngMode) {
      case 'allow': return true;
      case 'later': return new Promise((r) => setTimeout(() => r(true), 150));
      case 'micro': return (async () => true)();
      case 'deny': return new Promise((r) => setTimeout(() => r(false), 100));
      case 'throw': throw new Error('guard broke');
      case 'reject': return new Promise((_, j) => setTimeout(() => j(new Error('guard rejected')), 50));
      case 'hold': return new Promise((r) => { __ng.held.push({ r, nav }); });
    }
    const left = { url: location.pathname, page: document.querySelector('main section')?.id || '', draft: draft() };
    return fetch('/checkpoint', { method: 'POST', body: draft() }).then((res) => {
      left.pageAfter = document.querySelector('main section')?.id || '';
      left.draftAfter = draft();
      __ng.atCheckpoint.push(left);
      return res.ok;
    });
  });
  return true;
})()`

// ngStateJS is what the retention assertions read.
const ngStateJS = `JSON.stringify({
  url: location.pathname + location.search + location.hash,
  page: document.querySelector('main section')?.id || '',
  note: document.querySelector('main input')?.value ?? '',
  focus: document.activeElement?.id || '',
  len: history.length,
  key: window.navigation?.currentEntry?.key || '',
  index: window.navigation?.currentEntry?.index ?? -1,
  stamp: window.__ng?.stamp || 'gone',
  scrollY: Math.round(scrollY),
})`

type ngState struct {
	URL     string `json:"url"`
	Page    string `json:"page"`
	Note    string `json:"note"`
	Focus   string `json:"focus"`
	Len     int    `json:"len"`
	Key     string `json:"key"`
	Index   int    `json:"index"`
	Stamp   string `json:"stamp"`
	ScrollY int    `json:"scrollY"`
}

type ngCall struct {
	ID        int     `json:"id"`
	Intent    string  `json:"intent"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	FromEntry *string `json:"fromEntry"`
	ToEntry   *string `json:"toEntry"`
	Delta     *int    `json:"delta"`
	Frozen    bool    `json:"frozen"`
	Aborted   bool    `json:"aborted"`
}

type ngBlocked struct {
	Reason string `json:"reason"`
	ID     int    `json:"id"`
	Intent string `json:"intent"`
	Error  string `json:"error"`
}

func ngRun(t *testing.T, ctx context.Context, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
}

func ngEval(t *testing.T, ctx context.Context, js string, out any) {
	t.Helper()
	ngRun(t, ctx, chromedp.Evaluate(js, out, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

func ngJSON[T any](t *testing.T, ctx context.Context, js string) T {
	t.Helper()
	var raw string
	ngEval(t, ctx, `JSON.stringify(`+js+`)`, &raw)
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode %s: %v (%s)", js, err, raw)
	}
	return v
}

func ngRead(t *testing.T, ctx context.Context) ngState {
	t.Helper()
	var raw string
	ngEval(t, ctx, ngStateJS, &raw)
	var s ngState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("state: %v (%s)", err, raw)
	}
	return s
}

func ngCalls(t *testing.T, ctx context.Context) []ngCall {
	t.Helper()
	return ngJSON[[]ngCall](t, ctx, `__ng.calls`)
}

func ngBlocks(t *testing.T, ctx context.Context) []ngBlocked {
	t.Helper()
	return ngJSON[[]ngBlocked](t, ctx, `__ng.blocked`)
}

// ngWait polls a boolean expression until it holds, failing with what
// it was waiting for.
func ngWait(t *testing.T, ctx context.Context, js, what string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err == nil && ok {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (%s); state %+v", what, js, ngRead(t, ctx))
}

// ngQuiet waits for the router to settle: no busy marker and the URL
// and the page agreeing.
func ngQuiet(t *testing.T, ctx context.Context) {
	t.Helper()
	ngWait(t, ctx, `!document.documentElement.hasAttribute('aria-busy') && document.querySelector('main section')?.id === 'page-' + location.pathname.slice(1)`, "a settled page")
}

// ngOpen loads /a, waits for the runtime, and installs the guard rig
// in mode.
func ngOpen(t *testing.T, r *ngRig, mode string) context.Context {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	ngRun(t, ctx,
		chromedp.Navigate(r.srv.URL+"/a"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	)
	ngInstall(t, ctx, mode)
	return ctx
}

func ngInstall(t *testing.T, ctx context.Context, mode string) {
	t.Helper()
	var ok bool
	ngEval(t, ctx, `window.__ngMode = '`+mode+`'; `+ngSetupJS, &ok)
	if !ok {
		t.Fatal("guard rig did not install")
	}
}

func ngMode(t *testing.T, ctx context.Context, mode string) {
	t.Helper()
	ngEval(t, ctx, `window.__ngMode = '`+mode+`'`, nil)
}

// ngGo navigates through the router with no guard in the way and
// waits for the page.
func ngGo(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	ngEval(t, ctx, `__gofastr.navigate('`+path+`')`, nil)
	ngWait(t, ctx, `location.pathname === '`+path+`'`, "the URL "+path)
	ngQuiet(t, ctx)
}

func ngType(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	ngRun(t, ctx, chromedp.Focus(`main input[aria-label^="note"]`, chromedp.ByQuery), chromedp.SendKeys(`main input[aria-label^="note"]`, text, chromedp.ByQuery))
}

func ngKey(t *testing.T, ctx context.Context) string {
	t.Helper()
	var k string
	ngEval(t, ctx, `navigation.currentEntry.key`, &k)
	return k
}

func strp(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func intp(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprint(*p)
}

// TestNavGuardCheckpointBeforeLeaving: a guard that stores the leaving
// page's draft durably (a POST answered 600 ms late) holds every kind
// of departure until the store is done. Meanwhile the page, the draft,
// the focus, the URL and the history stay the leaving page's; the
// destination is not fetched; and once allowed the move lands exactly
// where it was going, asked once.
func TestNavGuardCheckpointBeforeLeaving(t *testing.T) {
	type move struct {
		name   string
		prep   func(t *testing.T, ctx context.Context) // reach the leaving page
		act    string                                  // the departure
		leave  string
		dest   string
		intent string
		delta  *int
		push   bool // the move adds an entry
	}
	minus1, plus1 := -1, 1
	moves := []move{
		{name: "link", act: `document.getElementById('toB').click()`, leave: "/a", dest: "/b", intent: "push", push: true},
		{name: "navigate", act: `__gofastr.navigate('/b')`, leave: "/a", dest: "/b", intent: "push", push: true},
		{name: "back", prep: func(t *testing.T, ctx context.Context) { ngGo(t, ctx, "/b") },
			act: `history.back()`, leave: "/b", dest: "/a", intent: "traverse", delta: &minus1},
		{name: "forward", prep: func(t *testing.T, ctx context.Context) {
			ngGo(t, ctx, "/b")
			ngEval(t, ctx, `history.back()`, nil)
			ngWait(t, ctx, `location.pathname === '/a'`, "the way back to /a")
			ngQuiet(t, ctx)
		}, act: `history.forward()`, leave: "/a", dest: "/b", intent: "traverse", delta: &plus1},
	}
	for _, envelope := range []bool{false, true} {
		for _, m := range moves {
			t.Run(map[bool]string{false: "plain", true: "envelope"}[envelope]+"/"+m.name, func(t *testing.T) {
				r := newNGRig(t, envelope)
				ctx := ngOpen(t, r, "allow")
				if m.prep != nil {
					m.prep(t, ctx)
				}
				// The destination entry, for a traversal: the one the
				// browser holds at that distance.
				want := ""
				if m.delta != nil {
					ngEval(t, ctx, fmt.Sprintf(`navigation.entries()[navigation.currentEntry.index + %d].key`, *m.delta), &want)
				}
				ngMode(t, ctx, "checkpoint")
				ngType(t, ctx, "draft")
				before := ngRead(t, ctx)
				mark := len(r.events())
				asked := len(ngCalls(t, ctx))
				ngEval(t, ctx, m.act, nil)
				time.Sleep(300 * time.Millisecond)
				mid := ngRead(t, ctx)
				if mid.URL != m.leave || mid.Page != "page-"+strings.Trim(m.leave, "/") || mid.Note != "draft" || mid.Len != before.Len || mid.Key != before.Key || mid.Stamp != "alive" {
					t.Fatalf("while the checkpoint is pending the leaving page must stay as it was: before %+v, during %+v", before, mid)
				}
				if m.name != "link" && mid.Focus != before.Focus {
					t.Errorf("focus moved while pending: %q -> %q", before.Focus, mid.Focus)
				}
				for _, e := range r.events()[mark:] {
					if strings.HasPrefix(e, "partial "+m.dest) {
						t.Fatalf("destination fetched before the checkpoint finished: %v", r.events()[mark:])
					}
				}
				ngWait(t, ctx, `document.querySelector('main section')?.id === 'page-`+strings.Trim(m.dest, "/")+`'`, "the destination page")
				ngQuiet(t, ctx)
				after := ngRead(t, ctx)
				if after.URL != m.dest || after.Stamp != "alive" {
					t.Fatalf("after the allow: %+v, want %s without a reload", after, m.dest)
				}
				if m.push && after.Len != before.Len+1 {
					t.Errorf("a push adds one entry: history %d -> %d", before.Len, after.Len)
				}
				if !m.push && (after.Len != before.Len || after.Key != want) {
					t.Errorf("a traversal lands on the entry it was going to: key %q (want %q), history %d -> %d", after.Key, want, before.Len, after.Len)
				}
				ck := r.indexOf("checkpoint:draft")
				if ck < 0 {
					t.Fatalf("the draft was never checkpointed: %v", r.events())
				}
				for i, e := range r.events() {
					if i >= mark && i < ck && strings.HasPrefix(e, "partial "+m.dest) {
						t.Fatalf("destination fetched before the checkpoint: %v", r.events())
					}
				}
				at := ngJSON[[]map[string]string](t, ctx, `__ng.atCheckpoint`)
				if len(at) != 1 || at[0]["page"] != "page-"+strings.Trim(m.leave, "/") || at[0]["pageAfter"] != at[0]["page"] || at[0]["draftAfter"] != "draft" {
					t.Errorf("the leaving page must still be there when the checkpoint is stored: %v", at)
				}
				calls := ngCalls(t, ctx)[asked:]
				if len(calls) != 1 {
					t.Fatalf("one departure asks once, got %d calls: %+v", len(calls), calls)
				}
				c := calls[0]
				if c.Intent != m.intent || c.From != m.leave || c.To != m.dest || !c.Frozen || c.Aborted {
					t.Errorf("attempt = %+v, want intent %s from %s to %s, frozen, live", c, m.intent, m.leave, m.dest)
				}
				if strp(c.FromEntry) != before.Key {
					t.Errorf("attempt.from.entry = %s, want the leaving entry %s", strp(c.FromEntry), before.Key)
				}
				if m.delta != nil && (strp(c.ToEntry) != want || intp(c.Delta) != fmt.Sprint(*m.delta)) {
					t.Errorf("traversal attempt to.entry=%s delta=%s, want %s and %d", strp(c.ToEntry), intp(c.Delta), want, *m.delta)
				}
				if m.delta == nil && (c.ToEntry != nil || c.Delta != nil) {
					t.Errorf("a push names no destination entry or distance yet: %+v", c)
				}
				if b := ngBlocks(t, ctx); len(b) != 0 {
					t.Errorf("an allowed move reported blocked: %+v", b)
				}
			})
		}
	}
}

// TestNavGuardDenialKeepsTheEditor: a denial or a failing guard keeps
// the page, the draft, the focus, the URL and the history exactly, says
// why on gofastr:navigateblocked, leaves the input editable, and the
// same move made again once allowed goes where it always would.
func TestNavGuardDenialKeepsTheEditor(t *testing.T) {
	cases := []struct {
		name, mode, act, reason, err string
		back                         bool
	}{
		{name: "link-denied", mode: "deny", act: `document.getElementById('toC').click()`, reason: "denied"},
		{name: "navigate-throws", mode: "throw", act: `__gofastr.navigate('/c')`, reason: "error", err: "guard broke"},
		{name: "back-denied", mode: "deny", act: `history.back()`, reason: "denied", back: true},
		{name: "back-rejects", mode: "reject", act: `history.back()`, reason: "error", err: "guard rejected", back: true},
		{name: "forward-throws", mode: "throw", act: `history.forward()`, reason: "error", err: "guard broke", back: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newNGRig(t, false)
			ctx := ngOpen(t, r, "allow")
			ngGo(t, ctx, "/b")
			if strings.HasPrefix(tc.name, "forward") {
				ngGo(t, ctx, "/c")
				ngEval(t, ctx, `history.back()`, nil)
				ngWait(t, ctx, `location.pathname === '/b'`, "back on /b")
				ngQuiet(t, ctx)
			}
			entries := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
			ngMode(t, ctx, tc.mode)
			ngType(t, ctx, "draft")
			before := ngRead(t, ctx)
			mark := len(r.events())
			ngEval(t, ctx, tc.act, nil)
			ngWait(t, ctx, `__ng.blocked.length === 1`, "the blocked report")
			time.Sleep(250 * time.Millisecond)
			after := ngRead(t, ctx)
			if after.URL != "/b" || after.Page != "page-b" || after.Note != "draft" || after.Len != before.Len || after.Key != before.Key || after.Stamp != "alive" {
				t.Fatalf("a %s move must keep the page: before %+v, after %+v", tc.reason, before, after)
			}
			if !strings.HasPrefix(tc.name, "link") && after.Focus != "note-b" {
				t.Errorf("focus = %q, want it still in the editor", after.Focus)
			}
			if got := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`); strings.Join(got, ",") != strings.Join(entries, ",") {
				t.Errorf("history entries changed: %v -> %v", entries, got)
			}
			for _, e := range r.events()[mark:] {
				if strings.HasPrefix(e, "partial") {
					t.Errorf("a blocked move fetched: %v", r.events()[mark:])
				}
			}
			b := ngBlocks(t, ctx)
			if len(b) != 1 || b[0].Reason != tc.reason || b[0].Error != tc.err {
				t.Errorf("blocked = %+v, want one %q (error %q)", b, tc.reason, tc.err)
			}
			// Still an editor.
			ngRun(t, ctx, chromedp.Focus(`#note-b`, chromedp.ByID), chromedp.SendKeys(`#note-b`, "x", chromedp.ByID))
			if s := ngRead(t, ctx); s.Note != "draftx" {
				t.Errorf("the editor is not usable after the denial: note %q", s.Note)
			}
			// The same move, allowed, still goes where it was going.
			ngMode(t, ctx, "later")
			ngEval(t, ctx, tc.act, nil)
			dest := "/c"
			if strings.HasPrefix(tc.name, "back") {
				dest = "/a"
			}
			ngWait(t, ctx, `location.pathname === '`+dest+`' && document.querySelector('main section')?.id === 'page-`+dest[1:]+`'`, "the allowed retry")
			ngQuiet(t, ctx)
			if tc.back {
				if s := ngRead(t, ctx); s.Len != before.Len || s.Key != entries[s.Index] {
					t.Errorf("the allowed traversal moved within the same entries: %+v (entries %v)", s, entries)
				}
			}
		})
	}
}

// TestNavGuardSupersededDecisionCannotAuthorize: a newer move replaces
// a pending one; the old decision's signal aborts, and its late allow
// moves nothing (a link and a traversal alike).
func TestNavGuardSupersededDecisionCannotAuthorize(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		r := newNGRig(t, false)
		ctx := ngOpen(t, r, "hold")
		ngRun(t, ctx, chromedp.Click(`#toC`, chromedp.ByID))
		ngWait(t, ctx, `!!__ng.held[0]`, "the first decision")
		ngRun(t, ctx, chromedp.Click(`#toD`, chromedp.ByID))
		ngWait(t, ctx, `!!__ng.held[1]`, "the second decision")
		var aborted string
		ngEval(t, ctx, `__ng.held[0].nav.signal.aborted + ':' + __ng.held[0].nav.signal.reason + ':' + __ng.held[1].nav.signal.aborted`, &aborted)
		if aborted != "true:superseded:false" {
			t.Errorf("signals = %s, want the first aborted as superseded, the second live", aborted)
		}
		ngEval(t, ctx, `__ng.held[0].r(true)`, nil)
		time.Sleep(400 * time.Millisecond)
		if s := ngRead(t, ctx); s.URL != "/a" || s.Page != "page-a" {
			t.Fatalf("a superseded allow moved the page: %+v", s)
		}
		if r.indexOf("partial /c") >= 0 {
			t.Fatalf("a superseded allow fetched its destination: %v", r.events())
		}
		ngEval(t, ctx, `__ng.held[1].r(true)`, nil)
		ngWait(t, ctx, `location.pathname === '/d'`, "the live decision's move")
		ngQuiet(t, ctx)
		if b := ngBlocks(t, ctx); len(b) != 0 {
			t.Errorf("a superseded move reports nothing: %+v", b)
		}
	})
	t.Run("traversal", func(t *testing.T) {
		r := newNGRig(t, false)
		ctx := ngOpen(t, r, "allow")
		ngGo(t, ctx, "/b")
		kb := ngKey(t, ctx)
		ngMode(t, ctx, "hold")
		ngEval(t, ctx, `history.back()`, nil)
		ngWait(t, ctx, `!!__ng.held[0]`, "the traversal's decision")
		// The undo lands first: the page shows /b again.
		ngWait(t, ctx, `navigation.currentEntry.key === '`+kb+`'`, "the undo landing")
		ngEval(t, ctx, `__gofastr.navigate('/c')`, nil)
		ngWait(t, ctx, `!!__ng.held[1]`, "the navigate decision")
		ngEval(t, ctx, `__ng.held[0].r(true)`, nil)
		time.Sleep(400 * time.Millisecond)
		if s := ngRead(t, ctx); s.URL != "/b" || s.Key != kb || s.Page != "page-b" {
			t.Fatalf("a superseded traversal allow moved the page: %+v", s)
		}
		ngEval(t, ctx, `__ng.held[1].r(true)`, nil)
		ngWait(t, ctx, `location.pathname === '/c'`, "the navigate")
		ngQuiet(t, ctx)
		// The /b entry the traversal left is still the one behind /c.
		var behind string
		ngEval(t, ctx, `navigation.entries()[navigation.currentEntry.index - 1].key`, &behind)
		if behind != kb {
			t.Errorf("the entry behind /c = %q, want /b's own %q", behind, kb)
		}
	})
}

// ngCoherent asserts the page, the URL and the entry agree, the entries
// are the ones there were, and the document never reloaded.
func ngCoherent(t *testing.T, ctx context.Context, entries []string) ngState {
	t.Helper()
	s := ngRead(t, ctx)
	if s.Page != "page-"+strings.TrimPrefix(s.URL, "/") || s.Stamp != "alive" {
		t.Fatalf("page and URL disagree, or the document reloaded: %+v", s)
	}
	if got := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`); strings.Join(got, ",") != strings.Join(entries, ",") {
		t.Fatalf("history entries changed: %v -> %v", entries, got)
	}
	return s
}

// TestNavGuardRapidTraversals: traversals made faster than the guard
// answers (two Backs in one tick, then a jump and a Forward 20 ms
// apart) end on a page that matches its URL and entry, with the
// entries unchanged and no reload.
func TestNavGuardRapidTraversals(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "allow")
	for _, p := range []string{"/b", "/c", "/d"} {
		ngGo(t, ctx, p)
	}
	entries := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
	asked := len(ngCalls(t, ctx))
	ngEval(t, ctx, `
window.__rapid = { attempts: [], nativeDone: false, nativeErrors: [] };
__gofastr.guardNavigation((nav) => {
  const record = { nav, answered: false, landed: false };
  __rapid.attempts.push(record);
  return new Promise((resolve) => setTimeout(() => { record.answered = true; resolve(true); }, 2200));
});
addEventListener('gofastr:navigate', () => {
  for (const record of __rapid.attempts) {
    if (record.answered && !record.nav.signal.aborted && record.nav.to.entry === navigation.currentEntry.key) record.landed = true;
  }
});
window.__rapidComplete = (moves) => Promise.allSettled(moves).then((results) => {
  __rapid.nativeErrors = results.filter((result) => result.status === 'rejected' && result.reason?.name !== 'AbortError').map((result) => String(result.reason));
  __rapid.nativeDone = true;
});
__rapidComplete([navigation.back().finished, navigation.back().finished]);
true`, nil)
	waitTraversal := func() {
		t.Helper()
		ngWait(t, ctx, `__rapid.nativeDone && __rapid.attempts.length > __rapidStart && __rapid.attempts.slice(__rapidStart).every((record) => record.nav.signal.aborted || (record.answered && record.landed))`, "native traversal completion or supersession, guard answers and router landings")
		if failures := ngJSON[[]string](t, ctx, `__rapid.nativeErrors`); len(failures) > 0 {
			t.Fatalf("native traversal failures: %v", failures)
		}
		ngQuiet(t, ctx)
		state := ngCoherent(t, ctx, entries)
		var destination string
		ngEval(t, ctx, `__rapid.attempts.findLast((record) => !record.nav.signal.aborted).nav.to.entry`, &destination)
		if state.Key != destination || state.Key != entries[state.Index] {
			t.Fatalf("entry %q at index %d, want decided destination %q in %v", state.Key, state.Index, destination, entries)
		}
	}
	ngEval(t, ctx, `window.__rapidStart = 0`, nil)
	waitTraversal()
	ngEval(t, ctx, `
__rapidStart = __rapid.attempts.length;
__rapid.nativeDone = false;
const jump = navigation.traverseTo(navigation.entries()[Math.max(1, navigation.currentEntry.index - 2)].key).finished;
const forward = new Promise((resolve, reject) => setTimeout(() => navigation.forward().finished.then(resolve, reject), 20));
__rapidComplete([jump, forward]);
true`, nil)
	waitTraversal()
	for _, c := range ngCalls(t, ctx)[asked:] {
		found := false
		for _, k := range entries {
			found = found || strp(c.ToEntry) == k
		}
		if c.Intent != "traverse" || !found {
			t.Errorf("a traversal attempt names an entry outside the history: %+v", c)
		}
	}
}

// TestNavGuardRepeatedURLsAreDistinctEntries: with /a and /b each in
// the history twice, the attempt names the exact entry, a denial
// returns to the exact entry the page shows (not an earlier one with
// the same URL), and an allow lands on the exact entry chosen.
func TestNavGuardRepeatedURLsAreDistinctEntries(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "allow")
	ngGo(t, ctx, "/b")
	ngGo(t, ctx, "/a")
	ngGo(t, ctx, "/b")
	k := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
	n := len(k)
	ngMode(t, ctx, "hold")
	ngEval(t, ctx, `history.go(-3)`, nil)
	ngWait(t, ctx, `!!__ng.held[0]`, "the jump's decision")
	c := ngCalls(t, ctx)[3]
	if strp(c.ToEntry) != k[n-4] || strp(c.FromEntry) != k[n-1] || intp(c.Delta) != "-3" {
		t.Fatalf("attempt %+v, want from %s to %s by -3", c, k[n-1], k[n-4])
	}
	ngWait(t, ctx, `navigation.currentEntry.key === '`+k[n-1]+`'`, "the undo landing on the exact entry")
	ngEval(t, ctx, `__ng.held[0].r(false)`, nil)
	ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
	time.Sleep(200 * time.Millisecond)
	if s := ngCoherent(t, ctx, k); s.Key != k[n-1] || s.URL != "/b" {
		t.Fatalf("the denial must stay on entry %s, at %+v", k[n-1], s)
	}
	ngEval(t, ctx, `history.back()`, nil)
	ngWait(t, ctx, `!!__ng.held[1]`, "the Back's decision")
	if c := ngCalls(t, ctx)[4]; strp(c.ToEntry) != k[n-2] {
		t.Fatalf("Back names %s, want the nearer /a entry %s", strp(c.ToEntry), k[n-2])
	}
	ngEval(t, ctx, `__ng.held[1].r(true)`, nil)
	ngWait(t, ctx, `location.pathname === '/a'`, "the allowed Back")
	ngQuiet(t, ctx)
	if s := ngCoherent(t, ctx, k); s.Key != k[n-2] {
		t.Fatalf("the allowed Back landed on %s, want %s (same URL, other entry: %s)", s.Key, k[n-2], k[n-4])
	}
}

// TestNavGuardCompositionAndDisposal: guards run in registration order
// and the first denial ends the decision; synchronous allows keep the
// move in the same tick; disposal is idempotent; disposing the guard a
// move waits on cancels the move.
func TestNavGuardCompositionAndDisposal(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "allow")
	var order string
	ngEval(t, ctx, `(() => {
  window.__order = []; __ng.pending = {};
  window.__g1 = 'allow'; window.__g2 = 'allow';
  const pick = (m, nav) => m === 'deny' ? false : m === 'hold' ? new Promise((res) => { __ng.pending['g' + nav.id] = { r: res, nav }; }) : m === 'slow' ? new Promise((res) => setTimeout(() => res(true), 200)) : true;
  window.__stop1 = __gofastr.guardNavigation((nav) => { __order.push('g1:' + nav.id); return pick(__g1, nav); });
  window.__stop2 = __gofastr.guardNavigation((nav) => { __order.push('g2:' + nav.id); return pick(__g2, nav); });
  document.getElementById('toB').click();
  return true;
})()`, nil)
	// The rig's own guard registered first.
	ngEval(t, ctx, `location.pathname + ' ' + __ng.calls.map((c) => 'rig:' + c.id).concat(__order).join(',')`, &order)
	if order != "/b rig:1,g1:1,g2:1" {
		t.Fatalf("synchronous allows must move in the same tick, in order: %q", order)
	}
	ngQuiet(t, ctx)
	// The first denial ends the decision.
	ngEval(t, ctx, `__g1 = 'deny'; __order = []; __gofastr.navigate('/c'); __order.join(',') + ' ' + location.pathname`, &order)
	if order != "g1:2 /b" {
		t.Fatalf("a denial must stop the later guards: %q", order)
	}
	// Disposal is idempotent and takes the guard out.
	ngEval(t, ctx, `__stop1(); __stop1(); __order = []; __gofastr.navigate('/c'); __order.join(',') + ' ' + location.pathname`, &order)
	if order != "g2:3 /c" {
		t.Fatalf("after disposal only the others ask: %q", order)
	}
	ngQuiet(t, ctx)
	// Disposing the guard a move waits on cancels the move.
	ngEval(t, ctx, `__g2 = 'hold'; __gofastr.navigate('/d'); !!__ng.pending.g4`, nil)
	ngWait(t, ctx, `!!__ng.pending.g4`, "the held decision")
	var sig string
	ngEval(t, ctx, `__stop2(); __stop2(); const s = __ng.pending.g4.nav.signal; s.aborted + ':' + s.reason`, &sig)
	if sig != "true:disposed" {
		t.Errorf("signal after disposal = %s, want aborted as disposed", sig)
	}
	ngEval(t, ctx, `__ng.pending.g4.r(true)`, nil)
	time.Sleep(300 * time.Millisecond)
	if s := ngRead(t, ctx); s.URL != "/c" {
		t.Fatalf("a disposed guard's late allow moved the page: %+v", s)
	}
	if b := ngBlocks(t, ctx); len(b) != 2 || b[0].Reason != "denied" || b[1].Reason != "disposed" {
		t.Errorf("blocked = %+v, want the denial then the disposal", b)
	}
	// A guard disposed before its turn is skipped.
	ngEval(t, ctx, `__stop2 = __gofastr.guardNavigation(() => { __order.push('late'); return true; });
  window.__stop3 = __gofastr.guardNavigation(() => { __order.push('after'); return true; });
  __order = []; __ngMode = 'later'; __gofastr.navigate('/d'); __stop3(); true`, nil)
	ngWait(t, ctx, `location.pathname === '/d'`, "the move past a disposed guard")
	ngEval(t, ctx, `__order.join(',')`, &order)
	if order != "late" {
		t.Errorf("guards asked = %q, want the disposed one skipped", order)
	}
	var threw string
	ngEval(t, ctx, `(() => { try { __gofastr.guardNavigation('x'); return 'no'; } catch (e) { return e.name; } })()`, &threw)
	if threw != "TypeError" {
		t.Errorf("a non-function guard must throw a TypeError, got %q", threw)
	}
}

// TestNavGuardNoGuardCompatibility: with the module loaded and no guard
// registered, links, navigate() and Back run in the same tick as
// before; the legacy synchronous gofastr:beforenavigate cancel still
// claims a click before any guard is asked; navigate() still refuses
// another origin before asking; a fragment-only move is not a
// departure.
func TestNavGuardNoGuardCompatibility(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "allow")
	var got string
	ngEval(t, ctx, `__stop(); __stop(); document.getElementById('toB').click(); location.pathname`, &got)
	if got != "/b" {
		t.Fatalf("a link with no guard must push in the same tick, at %q", got)
	}
	ngQuiet(t, ctx)
	ngEval(t, ctx, `__gofastr.navigate('/c'); location.pathname`, &got)
	if got != "/c" {
		t.Fatalf("navigate() with no guard must push in the same tick, at %q", got)
	}
	ngQuiet(t, ctx)
	ngEval(t, ctx, `history.back()`, nil)
	ngWait(t, ctx, `location.pathname === '/b'`, "Back")
	ngQuiet(t, ctx)
	if b := ngBlocks(t, ctx); len(b) != 0 {
		t.Fatalf("no guard, no blocks: %+v", b)
	}
	// A guard again: the legacy cancel runs first and claims the click.
	ngInstall(t, ctx, "allow")
	ngEval(t, ctx, `document.addEventListener('gofastr:beforenavigate', (e) => { if (window.__cancel) e.preventDefault(); });
  window.__cancel = true; document.getElementById('toD').click(); location.pathname`, &got)
	if got != "/b" || len(ngCalls(t, ctx)) != 0 {
		t.Fatalf("a cancelled beforenavigate must stop the router before any guard: at %q, calls %+v", got, ngCalls(t, ctx))
	}
	ngEval(t, ctx, `window.__cancel = false; __gofastr.navigate('https://elsewhere.invalid/x'); __gofastr.navigate('javascript:alert(1)'); location.pathname`, &got)
	if got != "/b" || len(ngCalls(t, ctx)) != 0 {
		t.Fatalf("navigate() must refuse another origin before asking: at %q, calls %+v", got, ngCalls(t, ctx))
	}
	ngEval(t, ctx, `location.hash = 'end-b'; history.back(); true`, nil)
	time.Sleep(300 * time.Millisecond)
	if s := ngRead(t, ctx); s.URL != "/b" || len(ngCalls(t, ctx)) != 0 {
		t.Fatalf("a fragment move discards nothing and is not asked: %+v, calls %+v", s, ngCalls(t, ctx))
	}
}

// TestNavGuardFormsAndRedirects pins the managed form and redirect
// coverage: a GET data-cui-spa form goes through navigate() and asks
// once; a link whose page the server redirects asks once, about the
// link, and the redirect leg is not asked again; a POST form whose
// answer redirects is a document load after the write, which the
// guards do not see (the documented limit: beforeunload only).
func TestNavGuardFormsAndRedirects(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "later")
	ngRun(t, ctx, chromedp.Click(`#find-go`, chromedp.ByID))
	ngWait(t, ctx, `location.pathname === '/result'`, "the GET form's navigation")
	ngQuiet(t, ctx)
	calls := ngCalls(t, ctx)
	if len(calls) != 1 || calls[0].Intent != "push" || calls[0].To != "/result?q=files" {
		t.Fatalf("GET form attempts = %+v, want one push to /result?q=files", calls)
	}
	ngRun(t, ctx, chromedp.Click(`#toMoved`, chromedp.ByID))
	ngWait(t, ctx, `location.pathname === '/b'`, "the redirected link")
	ngQuiet(t, ctx)
	calls = ngCalls(t, ctx)
	if len(calls) != 2 || calls[1].To != "/moved" {
		t.Fatalf("a redirected link asks once, about the link: %+v", calls)
	}
	ngMode(t, ctx, "deny")
	ngEval(t, ctx, `sessionStorage.setItem('ngCalls', '0'); __gofastr.guardNavigation(() => { sessionStorage.setItem('ngCalls', String(+sessionStorage.getItem('ngCalls') + 1)); return true; }); true`, nil)
	ngRun(t, ctx, chromedp.Click(`#post-go`, chromedp.ByID))
	ngWait(t, ctx, `location.pathname === '/c' && !window.__ng`, "the POST redirect's document load")
	var n string
	ngEval(t, ctx, `sessionStorage.getItem('ngCalls')`, &n)
	if n != "0" || r.indexOf("post") < 0 {
		t.Errorf("a POST form's followed redirect is a document load the guards do not see: guard calls %s, log %v", n, r.events())
	}
}

// TestNavGuardLimits pins what the guards see around document loads.
// A reload is a hard unload (beforeunload's alone), yet Chromium keeps
// the entries the router pushed before it in the reloaded document, so
// Back onto them is asked about as usual. A link the router does not
// take (data-cui-nav="off") is a document load, and the entries before
// it then belong to another document: Back onto them is a document
// load the guards do not see. A history write made around the router
// is not asked about; moving onto its entry later is an ordinary
// traversal that is.
func TestNavGuardLimits(t *testing.T) {
	r := newNGRig(t, false)
	ctx := ngOpen(t, r, "allow")
	ngGo(t, ctx, "/b")
	count := `window.__doc = 1; sessionStorage.setItem('ngCalls', '0'); __gofastr.guardNavigation(() => { sessionStorage.setItem('ngCalls', String(+sessionStorage.getItem('ngCalls') + 1)); return false; }); true`
	calls := func() string {
		var n string
		ngEval(t, ctx, `sessionStorage.getItem('ngCalls')`, &n)
		return n
	}
	ngEval(t, ctx, count, nil)
	ngRun(t, ctx, chromedp.Reload(), chromedp.WaitVisible(`#ready`, chromedp.ByID))
	if n := calls(); n != "0" {
		t.Errorf("a reload is a hard unload the guards do not see: %s calls", n)
	}
	ngInstall(t, ctx, "deny")
	var same bool
	ngEval(t, ctx, `navigation.entries()[0].sameDocument`, &same)
	ngEval(t, ctx, `window.__doc = 1; history.back(); true`, nil)
	ngWait(t, ctx, `__ng.blocked.length === 1`, "the guarded Back after the reload")
	if s := ngRead(t, ctx); !same || s.URL != "/b" || s.Page != "page-b" || s.Stamp != "alive" {
		t.Fatalf("Back onto a pre-reload entry (same document: %v) is asked and denied in place: %+v", same, s)
	}
	ngEval(t, ctx, count, nil)
	ngRun(t, ctx, chromedp.Click(`#toOff`, chromedp.ByID))
	ngWait(t, ctx, `location.pathname === '/c' && !window.__doc && document.readyState === 'complete' && !!document.getElementById('ready')`, "the full load")
	if n := calls(); n != "0" {
		t.Errorf("a link the router does not take is not asked about: %s calls", n)
	}
	ngInstall(t, ctx, "allow")
	ngEval(t, ctx, count+`; window.__doc = 'c'`, nil)
	ngEval(t, ctx, `navigation.entries()[navigation.currentEntry.index - 1].sameDocument`, &same)
	ngEval(t, ctx, `history.back()`, nil)
	// The earlier document may come back whole from the back/forward
	// cache: either way it is not this one.
	ngWait(t, ctx, `location.pathname === '/b' && window.__doc !== 'c' && document.readyState === 'complete' && !!document.getElementById('ready')`, "the cross-document Back")
	if n := calls(); same || n != "0" {
		t.Errorf("Back onto another document's entry (same document: %v) is a document load the guards do not see: %s calls", same, n)
	}
	// A write around the router, in a fresh document.
	ngRun(t, ctx, chromedp.Navigate(r.srv.URL+"/b"), chromedp.WaitVisible(`#ready`, chromedp.ByID))
	ngInstall(t, ctx, "deny")
	ngEval(t, ctx, `history.pushState(null, '', '/d'); true`, nil)
	if c := ngCalls(t, ctx); len(c) != 0 {
		t.Fatalf("a history write is not a departure: %+v", c)
	}
	ngEval(t, ctx, `history.back()`, nil)
	ngWait(t, ctx, `location.pathname === '/b'`, "Back onto the router's own entry")
	time.Sleep(200 * time.Millisecond)
	if c := ngCalls(t, ctx); len(c) != 0 {
		t.Fatalf("Back onto the page the router shows discards nothing: %+v", c)
	}
	ngEval(t, ctx, `history.forward()`, nil)
	ngWait(t, ctx, `__ng.blocked.length === 1`, "the guarded move onto the foreign entry")
	if c := ngCalls(t, ctx); len(c) != 1 || c[0].Intent != "traverse" || c[0].To != "/d" {
		t.Fatalf("moving onto a foreign entry is an ordinary traversal: %+v", c)
	}
	time.Sleep(200 * time.Millisecond)
	if s := ngRead(t, ctx); s.URL != "/b" || s.Page != "page-b" || s.Stamp != "alive" {
		t.Fatalf("the denied move stays: %+v", s)
	}
}

// TestNavGuardTransitionAndScroll: a guarded Back keeps the router's
// own bookkeeping honest: the destination's scroll position comes
// back, the view transition reads the move as backward (the engine's
// undo and replay are invisible to it), and reduced motion still
// animates nothing.
func TestNavGuardTransitionAndScroll(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(map[bool]string{false: "motion", true: "reduced"}[reduce], func(t *testing.T) {
			r := newNGRig(t, false)
			r.pick = "slide"
			pref := "no-preference"
			if reduce {
				pref = "reduce"
			}
			ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
			ngRun(t, ctx,
				emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: pref}}),
				chromedp.Navigate(r.srv.URL+"/a"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.transition`, nil),
			)
			ngInstall(t, ctx, "allow")
			ngEval(t, ctx, `scrollTo(0, 800); true`, nil)
			time.Sleep(200 * time.Millisecond)
			// A script click: chromedp's would scroll the link into view.
			ngEval(t, ctx, `document.getElementById('toB').click(); true`, nil)
			ngWait(t, ctx, `document.querySelector('main section')?.id === 'page-b'`, "/b")
			ngQuiet(t, ctx)
			readTypes := captureTypes(ctx)
			ngMode(t, ctx, "checkpoint")
			ngEval(t, ctx, `history.back()`, nil)
			time.Sleep(250 * time.Millisecond)
			if s := ngRead(t, ctx); s.Page != "page-b" || s.URL != "/b" {
				t.Fatalf("pending: %+v", s)
			}
			ngWait(t, ctx, `document.querySelector('main section')?.id === 'page-a'`, "/a")
			ngQuiet(t, ctx)
			ngWait(t, ctx, `Math.abs(scrollY - 800) < 4`, "the restored scroll position")
			types := readTypes()
			if reduce && len(types) != 0 {
				t.Errorf("reduced motion animated: %v", types)
			}
			if !reduce && (len(types) != 1 || strings.Join(types[0], ",") != "back,slide") {
				t.Errorf("transition types = %v, want one back,slide", types)
			}
		})
	}
}

// --- The intercept stack ---

// ngStackServer is the intercept stack rig (intercept_stack_e2e_test.go)
// served through ngSource, with the headless leave guard at /lg.js.
func ngStackServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	ngServeRuntime(t, mux)
	nhServeLeaveGuard(t, mux)
	mux.HandleFunc("/css/intercept-probe.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `[data-cui-comp="intercept-probe"]{display:block}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		key := r.URL.RequestURI()
		switch {
		case r.Header.Get("X-Gofastr-Intercept") != "":
			w.Header().Set("X-Gofastr-Overlay", interceptOverlayAs(r.URL.Path))
			fmt.Fprint(w, interceptOverlayBody(r.URL.Path, r.URL.RawQuery))
		case r.Header.Get("X-Gofastr-Navigate") == "1" && key == "/list":
			// The list reached by a client navigation: the same links,
			// so a stack can open over a page with an entry before it.
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, `<a id="to-a" href="/rec/a">rec a</a>LIST`)
		case r.Header.Get("X-Gofastr-Navigate") == "1":
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprintf(w, `<p id="plain-screen">plain %s</p>`, key)
		default:
			fmt.Fprint(w, interceptFullPage(key))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// ngStack opens /list, installs the rig in mode with the overlay
// fields logged too, and opens the record layer (a layer opening over
// the page discards nothing: not asked).
func ngStack(t *testing.T, mode string) context.Context {
	t.Helper()
	srv := ngStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	ngRun(t, ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	)
	ngInstall(t, ctx, mode)
	ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
	ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
	if c := ngCalls(t, ctx); len(c) != 0 {
		t.Fatalf("opening a layer discards nothing and is not asked: %+v", c)
	}
	ngRun(t, ctx, chromedp.Focus(`#rec-note`, chromedp.ByID), chromedp.SendKeys(`#rec-note`, "draft", chromedp.ByID))
	return ctx
}

type ngStackCall struct {
	ngCall
	AffectsNote bool `json:"affectsNote"`
	AffectsList bool `json:"affectsList"`
}

const ngLayerJS = `JSON.stringify({
  url: location.pathname + location.search,
  layers: document.getElementById('cui-intercept')?.children.length || 0,
  note: document.getElementById('rec-note')?.value ?? null,
  focus: document.activeElement?.id || '',
  len: history.length,
  key: ` + nhKeyJS + `,
})`

type ngLayer struct {
	URL    string  `json:"url"`
	Layers int     `json:"layers"`
	Note   *string `json:"note"`
	Focus  string  `json:"focus"`
	Len    int     `json:"len"`
	Key    string  `json:"key"`
}

func ngReadLayer(t *testing.T, ctx context.Context) ngLayer {
	t.Helper()
	var raw string
	ngEval(t, ctx, ngLayerJS, &raw)
	var l ngLayer
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatalf("layer state: %v (%s)", err, raw)
	}
	return l
}

// TestNavGuardOverlayDepartures: the intercept stack's departures ask
// the same guards, scoped to what they discard. Esc waits on the guard
// with the pane and its draft in place; a Back the guard has not
// allowed is undone to the exact entry; an allow that arrives before
// the undo lands is not asked again; a page link out of the stack asks
// once.
func TestNavGuardOverlayDepartures(t *testing.T) {
	t.Run("esc", func(t *testing.T) {
		ctx := ngStack(t, "hold")
		before := ngReadLayer(t, ctx)
		ngEval(t, ctx, `document.getElementById('rec-note').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); true`, nil)
		ngWait(t, ctx, `!!__ng.held[0]`, "Esc's decision")
		if l := ngReadLayer(t, ctx); l.Layers != 1 || l.Note == nil || *l.Note != "draft" || l.URL != "/rec/a" || l.Focus != "rec-note" || l.Key != before.Key {
			t.Fatalf("pending Esc must keep the pane: %+v", l)
		}
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)[0]
		if c.Intent != "overlay" || c.To != "/list" || intp(c.Delta) != "-1" || !c.AffectsNote || c.AffectsList {
			t.Errorf("Esc attempt = %+v, want an overlay move to /list by -1 that discards the pane only", c)
		}
		ngEval(t, ctx, `__ng.held[0].r(true)`, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept') && location.pathname === '/list'`, "the closed layer")
		if c := ngCalls(t, ctx); len(c) != 1 {
			t.Errorf("an allowed Esc asks once: %+v", c)
		}
	})
	t.Run("back-denied", func(t *testing.T) {
		ctx := ngStack(t, "hold")
		before := ngReadLayer(t, ctx)
		ngEval(t, ctx, `history.back()`, nil)
		ngWait(t, ctx, `!!__ng.held[0]`, "Back's decision")
		ngWait(t, ctx, `navigation.currentEntry.key === '`+before.Key+`'`, "the stack's undo")
		if l := ngReadLayer(t, ctx); l.Layers != 1 || l.Note == nil || *l.Note != "draft" || l.Len != before.Len {
			t.Fatalf("pending Back must keep the pane: %+v", l)
		}
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)[0]
		if c.Intent != "overlay" || c.To != "/list" || intp(c.Delta) != "-1" || !c.AffectsNote || c.AffectsList || c.ToEntry == nil {
			t.Errorf("Back attempt = %+v", c)
		}
		ngEval(t, ctx, `__ng.held[0].r(false)`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
		time.Sleep(200 * time.Millisecond)
		if l := ngReadLayer(t, ctx); l.Layers != 1 || *l.Note != "draft" || l.Key != before.Key || l.Len != before.Len {
			t.Fatalf("a denied Back keeps the pane on its entry: %+v", l)
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back()`, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept') && location.pathname === '/list'`, "the allowed Back")
		time.Sleep(200 * time.Millisecond)
		if c := ngCalls(t, ctx); len(c) != 2 {
			t.Errorf("the allowed Back's landing must not ask again: %+v", c)
		}
	})
	t.Run("back-allowed-before-undo-lands", func(t *testing.T) {
		ctx := ngStack(t, "micro")
		ngEval(t, ctx, `history.back()`, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept') && location.pathname === '/list'`, "the closed layer")
		time.Sleep(400 * time.Millisecond)
		if c := ngCalls(t, ctx); len(c) != 1 {
			t.Fatalf("an allow that beats the undo must be honoured once, not asked again: %+v", c)
		}
		if l := ngReadLayer(t, ctx); l.Layers != 0 || l.URL != "/list" {
			t.Fatalf("settled at %+v", l)
		}
	})
	t.Run("page-link", func(t *testing.T) {
		ctx := ngStack(t, "later")
		ngRun(t, ctx, chromedp.Click(`#a-page`, chromedp.ByID))
		ngWait(t, ctx, `!!document.getElementById('plain-screen') && location.pathname === '/rec/a'`, "the record as a page")
		time.Sleep(200 * time.Millisecond)
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)
		if len(c) != 1 || c[0].Intent != "push" || c[0].To != "/rec/a" || !c[0].AffectsNote || !c[0].AffectsList {
			t.Fatalf("a page link out of the stack asks once, about the whole page: %+v", c)
		}
	})
	t.Run("afield", func(t *testing.T) {
		// /pre, then /list by the router, then the record layer over it:
		// two entries back from the layer is a page the stack does not
		// know the distance to, and its entry is still the router's.
		srv := ngStackServer(t)
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		ngRun(t, ctx,
			chromedp.Navigate(srv.URL+"/pre"),
			chromedp.WaitVisible(`#ready`, chromedp.ByID),
		)
		ngInstall(t, ctx, "allow")
		ngEval(t, ctx, `__gofastr.navigate('/list')`, nil)
		ngWait(t, ctx, `!!document.getElementById('to-a') && location.pathname === '/list'`, "the list")
		ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
		ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
		ngRun(t, ctx, chromedp.Focus(`#rec-note`, chromedp.ByID), chromedp.SendKeys(`#rec-note`, "draft", chromedp.ByID))
		ngEval(t, ctx, `__ng.calls.length = 0; __ngMode = 'hold'`, nil)
		before := ngReadLayer(t, ctx)
		entries := `navigation.entries().map((e) => e.key).join()`
		var all string
		ngEval(t, ctx, entries, &all)

		ngEval(t, ctx, `history.go(-2)`, nil)
		ngWait(t, ctx, `!!__ng.held[0]`, "the decision")
		ngWait(t, ctx, `navigation.currentEntry.key === '`+before.Key+`'`, "the undo onto the layer's own entry")
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)
		if len(c) != 1 || c[0].Intent != "traverse" || c[0].To != "/pre" || c[0].ToEntry == nil || !c[0].AffectsNote {
			t.Fatalf("a move off the stack asks about the whole page: %+v", c)
		}
		ngEval(t, ctx, `__ng.held[0].r(false)`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
		time.Sleep(200 * time.Millisecond)
		var now string
		ngEval(t, ctx, entries, &now)
		if l := ngReadLayer(t, ctx); l.Layers != 1 || l.Note == nil || *l.Note != "draft" || l.URL != "/rec/a" || l.Key != before.Key || l.Len != before.Len || now != all {
			t.Fatalf("a denied move off the stack keeps the layer on its own entry, no entry added or cut: %+v (entries %s, were %s)", l, now, all)
		}

		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.go(-2)`, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept') && !document.getElementById('to-a') && /FULL \/pre|plain \/pre/.test(document.querySelector('main').textContent) && location.pathname === '/pre'`, "the allowed move")
		time.Sleep(200 * time.Millisecond)
		ngEval(t, ctx, entries, &now)
		var index int
		ngEval(t, ctx, `navigation.currentEntry.index`, &index)
		if now != all || index != 0 {
			t.Fatalf("an allowed move off the stack lands on the entry the reader chose: index %d, entries %s, were %s", index, now, all)
		}
		if c := ngCalls(t, ctx); len(c) != 2 {
			t.Errorf("the allowed move's landing must not ask again: %+v", c)
		}
	})
	t.Run("pane-query", func(t *testing.T) {
		ctx := ngStack(t, "deny")
		ngRun(t, ctx, chromedp.Click(`#pg-2`, chromedp.ByID))
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied query move")
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)
		if len(c) != 1 || c[0].Intent != "overlay" || c[0].To != "/rec/a?page=2" || !c[0].AffectsNote || c[0].AffectsList {
			t.Fatalf("a pane's query link asks about the pane: %+v", c)
		}
		if l := ngReadLayer(t, ctx); l.URL != "/rec/a" || l.Layers != 1 || *l.Note != "draft" {
			t.Fatalf("a denied query move keeps the pane: %+v", l)
		}
	})
}
