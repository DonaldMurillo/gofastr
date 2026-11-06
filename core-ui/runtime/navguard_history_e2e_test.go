package runtime

// Browser tests for the navigation guards' history contract
// (src/navguard.js over core's entry tags, frag/nav.js): a Back or
// Forward the guards do not allow at once is undone and, once allowed,
// replayed to the exact entry, by the Navigation API's keys where the
// browser has it and by the tags core writes on every entry where it
// does not. History no router wrote is refused without a push. The
// witness is the browser's own session history, read over CDP, not
// history.length: a push after a truncation keeps the length and
// still loses the entries ahead.
//
// "no-api" runs hide the Navigation API before any script of any
// document runs (reloads included), as on a browser without it. They
// are Chromium with the API removed, not Safari or Firefox.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// nhKeyJS names the entry the browser is on the way an attempt's entry
// does: the Navigation API's key, else core's tag (numbering, position
// and id); empty on an entry no router wrote.
const nhKeyJS = `(() => { const n = window.navigation; if (n && n.currentEntry) return n.currentEntry.key; const s = history.state, p = s && s.__fup; return p && s.__fui ? p[0] + ':' + p[1] + ':' + s.__fui : ''; })()`

// nhCtx is a browser; noAPI hides the Navigation API from every
// document before its own scripts run.
func nhCtx(t *testing.T, noAPI bool) context.Context {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if noAPI {
		ngRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`Object.defineProperty(window, 'navigation', { value: undefined, configurable: true });`).Do(ctx)
			return err
		}))
	}
	return ctx
}

// nhLoad loads url and checks the API is there or not, as asked.
func nhLoad(t *testing.T, ctx context.Context, url string, noAPI bool) {
	t.Helper()
	ngRun(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(`#ready`, chromedp.ByID))
	var has bool
	ngEval(t, ctx, `!!(window.navigation && window.navigation.currentEntry)`, &has)
	if has == noAPI {
		t.Fatalf("Navigation API present = %v, want %v", has, !noAPI)
	}
}

// nhOpenRig loads /a of the guard rig and installs the guard in mode
// (none for "").
func nhOpenRig(t *testing.T, r *ngRig, noAPI bool, mode string) context.Context {
	t.Helper()
	ctx := nhCtx(t, noAPI)
	nhLoad(t, ctx, r.srv.URL+"/a", noAPI)
	if mode != "" {
		ngInstall(t, ctx, mode)
	}
	return ctx
}

// nhModes runs fn with the Navigation API and without it.
func nhModes(t *testing.T, fn func(t *testing.T, noAPI bool)) {
	for _, noAPI := range []bool{false, true} {
		t.Run(map[bool]string{false: "api", true: "no-api"}[noAPI], func(t *testing.T) { fn(t, noAPI) })
	}
}

// nhHist is the browser's session history (CDP): paths, "@" on the
// current entry.
func nhHist(t *testing.T, ctx context.Context) string {
	t.Helper()
	var out []string
	ngRun(t, ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cur, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		for i, e := range entries {
			u := e.URL
			if k := strings.Index(u, "://"); k >= 0 && !strings.HasPrefix(u, "about:") {
				if s := strings.Index(u[k+3:], "/"); s >= 0 {
					u = u[k+3+s:]
				}
			}
			if int64(i) == cur {
				u += "@"
			}
			out = append(out, u)
		}
		return nil
	}))
	return strings.Join(out, " ")
}

func nhKey(t *testing.T, ctx context.Context) string {
	t.Helper()
	var k string
	ngEval(t, ctx, nhKeyJS, &k)
	return k
}

// nhUntil polls a boolean expression; false once d passes without it.
// The scenarios the mutation test reruns wait with it, so a broken
// engine reports what broke instead of stopping the run.
func nhUntil(ctx context.Context, js string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err == nil && ok {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

// nhCheck collects contract violations.
type nhCheck struct{ bad []string }

func (c *nhCheck) want(ok bool, format string, args ...any) {
	if !ok {
		c.bad = append(c.bad, fmt.Sprintf(format, args...))
	}
}

func nhReport(t *testing.T, bad []string) {
	t.Helper()
	for _, b := range bad {
		t.Error(b)
	}
}

// nhSettled is a router at rest on the page its URL names.
const nhSettled = `!document.documentElement.hasAttribute('aria-busy') && document.querySelector('main section')?.id === 'page-' + location.pathname.slice(1)`

// nhOn waits for the router to rest on path.
func nhOn(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	ngWait(t, ctx, `location.pathname === '`+path+`' && `+nhSettled, "the page "+path)
}

// nhServeLeaveGuard serves the real headless leave guard at /lg.js.
func nhServeLeaveGuard(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	js, err := os.ReadFile("../../framework/headless/leaveguard.js")
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/lg.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(js)
	})
}

// --- Undo and replay by identity ---

// nhDeniedBack: /a, /b, /c, then Back to /b. A Back the guard holds is
// undone to /b's own entry with the page, the draft and the focus in
// place; the attempt names both entries and the distance; the denial
// leaves the history exactly as it was, so Forward still reaches /c.
func nhDeniedBack(t *testing.T, noAPI bool) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, noAPI, "allow")
	kA := nhKey(t, ctx)
	ngGo(t, ctx, "/b")
	kB := nhKey(t, ctx)
	ngGo(t, ctx, "/c")
	kC := nhKey(t, ctx)
	ngEval(t, ctx, `history.back(); true`, nil)
	nhOn(t, ctx, "/b")
	h0 := nhHist(t, ctx)
	c.want(h0 == "about:blank /a /b@ /c", "setup history %s", h0)
	c.want(kA != "" && kA != kB && kB != kC && kA != kC, "entry keys not distinct: %q %q %q", kA, kB, kC)
	ngMode(t, ctx, "hold")
	ngType(t, ctx, "draft")
	ngEval(t, ctx, `__ng.calls.length = 0; history.back(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[0]`, 4*time.Second) {
		return append(c.bad, "the held Back was never asked about")
	}
	c.want(nhUntil(ctx, nhKeyJS+` === '`+kB+`'`, 4*time.Second), "the undo did not land on /b's own entry: at %q", nhKey(t, ctx))
	mid := ngRead(t, ctx)
	c.want(mid.URL == "/b" && mid.Page == "page-b" && mid.Note == "draft" && mid.Focus == "note-b" && mid.Stamp == "alive", "pending Back: the page must stay on /b with its draft and focus: %+v", mid)
	c.want(nhHist(t, ctx) == h0, "pending Back changed the history: %s -> %s", h0, nhHist(t, ctx))
	calls := ngCalls(t, ctx)
	c.want(len(calls) == 1 && calls[0].Intent == "traverse" && calls[0].From == "/b" && calls[0].To == "/a" &&
		intp(calls[0].Delta) == "-1" && strp(calls[0].FromEntry) == kB && strp(calls[0].ToEntry) == kA,
		"attempt = %+v, want a traverse /b -> /a by -1 from %q to %q", calls, kB, kA)
	ngEval(t, ctx, `__ng.held[0].r(false); true`, nil)
	c.want(nhUntil(ctx, `__ng.blocked.length === 1`, 4*time.Second), "the denial was not reported")
	time.Sleep(300 * time.Millisecond)
	if b := ngBlocks(t, ctx); len(b) != 1 || b[0].Reason != "denied" {
		c.want(false, "blocked = %+v, want one 'denied'", b)
	}
	s := ngRead(t, ctx)
	c.want(s.URL == "/b" && s.Page == "page-b" && s.Note == "draft" && s.Focus == "note-b" && s.Stamp == "alive", "denied Back: %+v", s)
	h1 := nhHist(t, ctx)
	c.want(h1 == h0 && nhKey(t, ctx) == kB, "denied Back changed the history: %s -> %s (key %q, want %q)", h0, h1, nhKey(t, ctx), kB)
	ngMode(t, ctx, "allow")
	ngEval(t, ctx, `history.forward(); true`, nil)
	c.want(nhUntil(ctx, `location.pathname === '/c' && `+nhSettled, 4*time.Second), "Forward after the denial did not reach /c: %+v", ngRead(t, ctx))
	h2 := nhHist(t, ctx)
	c.want(h2 == "about:blank /a /b /c@" && nhKey(t, ctx) == kC, "Forward after the denial: %s (key %q, want %q)", h2, nhKey(t, ctx), kC)
	return c.bad
}

func TestNavGuardHistoryDeniedBackKeepsForward(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) { nhReport(t, nhDeniedBack(t, noAPI)) })
}

// TestNavGuardHistoryDeniedForward: a denied Forward keeps the entries
// on both sides, so Back from the page still reaches /a, and an
// allowed traversal lands on the entry the reader chose.
func TestNavGuardHistoryDeniedForward(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		kA := nhKey(t, ctx)
		ngGo(t, ctx, "/b")
		ngGo(t, ctx, "/c")
		kC := nhKey(t, ctx)
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/b")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "deny")
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `__ng.calls.length = 0; history.forward(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied Forward")
		time.Sleep(300 * time.Millisecond)
		c := ngCalls(t, ctx)
		if len(c) != 1 || c[0].To != "/c" || intp(c[0].Delta) != "1" || strp(c[0].ToEntry) != kC {
			t.Errorf("Forward attempt = %+v, want /c by +1 to %q", c, kC)
		}
		if s := ngRead(t, ctx); s.URL != "/b" || s.Page != "page-b" || s.Note != "draft" || s.Focus != "note-b" {
			t.Errorf("denied Forward: %+v", s)
		}
		if h := nhHist(t, ctx); h != h0 {
			t.Errorf("denied Forward changed the history: %s -> %s", h0, h)
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		if h := nhHist(t, ctx); h != "about:blank /a@ /b /c" || nhKey(t, ctx) != kA {
			t.Errorf("Back after the denied Forward: %s, key %q want %q", h, nhKey(t, ctx), kA)
		}
		if s := ngRead(t, ctx); s.Focus != "main" || s.Stamp != "alive" {
			t.Errorf("the allowed Back lands like the router's own: %+v", s)
		}
	})
}

// nhSecondBack: a second Back while the first is pending replaces it.
// The first decision's signal aborts and its late allow moves nothing;
// the second's denial leaves the history as it was.
func nhSecondBack(t *testing.T, noAPI bool) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, noAPI, "allow")
	ngGo(t, ctx, "/b")
	ngGo(t, ctx, "/c")
	kC := nhKey(t, ctx)
	h0 := nhHist(t, ctx)
	ngMode(t, ctx, "hold")
	ngType(t, ctx, "draft")
	ngEval(t, ctx, `history.back(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[0]`, 4*time.Second) {
		return append(c.bad, "the first Back was never asked about")
	}
	c.want(nhUntil(ctx, nhKeyJS+` === '`+kC+`'`, 4*time.Second), "the first undo did not land")
	ngEval(t, ctx, `history.back(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[1]`, 4*time.Second) {
		return append(c.bad, "the second Back was never asked about")
	}
	var aborted bool
	ngEval(t, ctx, `__ng.held[0].nav.signal.aborted`, &aborted)
	c.want(aborted, "the replaced decision's signal did not abort")
	c.want(nhUntil(ctx, nhKeyJS+` === '`+kC+`'`, 4*time.Second), "the second undo did not land")
	ngEval(t, ctx, `__ng.held[1].r(false); true`, nil)
	c.want(nhUntil(ctx, `__ng.blocked.length === 1`, 4*time.Second), "the second denial was not reported")
	ngEval(t, ctx, `__ng.held[0].r(true); true`, nil)
	time.Sleep(700 * time.Millisecond)
	s := ngRead(t, ctx)
	c.want(s.URL == "/c" && s.Page == "page-c" && s.Note == "draft", "after the denial and the replaced allow: %+v", s)
	b := ngBlocks(t, ctx)
	c.want(len(b) == 1 && b[0].Reason == "denied", "blocked = %+v, want the second Back's denial only", b)
	h := nhHist(t, ctx)
	c.want(h == h0 && nhKey(t, ctx) == kC, "history changed: %s -> %s", h0, h)
	return c.bad
}

func TestNavGuardHistorySecondBackWhilePending(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) { nhReport(t, nhSecondBack(t, noAPI)) })
}

// nhTwoBacks: two Backs in one task. The browser runs both, so a
// landing arrives that is not the undo's: only the landing on the
// entry the undo heads for counts. Every decision denied, the page
// and the browser end on /c with the history unchanged.
func nhTwoBacks(t *testing.T, noAPI bool) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, noAPI, "allow")
	ngGo(t, ctx, "/b")
	ngGo(t, ctx, "/c")
	kC := nhKey(t, ctx)
	h0 := nhHist(t, ctx)
	ngMode(t, ctx, "hold")
	ngType(t, ctx, "draft")
	ngEval(t, ctx, `history.back(); history.back(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[0]`, 4*time.Second) {
		return append(c.bad, "the Backs were never asked about")
	}
	for i := 0; i < 4; i++ {
		time.Sleep(900 * time.Millisecond)
		ngEval(t, ctx, `__ng.held.forEach((h) => h.r(false)); true`, nil)
	}
	time.Sleep(600 * time.Millisecond)
	s := ngRead(t, ctx)
	c.want(s.URL == "/c" && s.Page == "page-c" && s.Note == "draft", "after two denied Backs: %+v", s)
	h := nhHist(t, ctx)
	c.want(h == h0 && nhKey(t, ctx) == kC, "history changed: %s -> %s (key %q, want %q)", h0, h, nhKey(t, ctx), kC)
	var held int
	ngEval(t, ctx, `__ng.held.length`, &held)
	b := ngBlocks(t, ctx)
	c.want(len(b) >= 1 && b[len(b)-1].Reason == "denied", "blocked = %+v (held %d)", b, held)
	return c.bad
}

func TestNavGuardHistoryTwoBacksInOneTask(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) { nhReport(t, nhTwoBacks(t, noAPI)) })
}

// TestNavGuardHistoryRepeatedURLs: /a, /b, /a, /b are four entries.
// go(-3) names the first /a, not the second; denied, nothing moves;
// allowed, it lands on that first /a.
func TestNavGuardHistoryRepeatedURLs(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		k := []string{nhKey(t, ctx)}
		for _, p := range []string{"/b", "/a", "/b"} {
			ngGo(t, ctx, p)
			k = append(k, nhKey(t, ctx))
		}
		if k[0] == k[2] || k[1] == k[3] {
			t.Fatalf("one URL twice must be two entries: %v", k)
		}
		h0 := nhHist(t, ctx)
		if h0 != "about:blank /a /b /a /b@" {
			t.Fatalf("setup history %s", h0)
		}
		ngMode(t, ctx, "deny")
		ngEval(t, ctx, `__ng.calls.length = 0; history.go(-3); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied go(-3)")
		time.Sleep(300 * time.Millisecond)
		c := ngCalls(t, ctx)
		if len(c) != 1 || intp(c[0].Delta) != "-3" || strp(c[0].ToEntry) != k[0] || strp(c[0].FromEntry) != k[3] {
			t.Errorf("go(-3) attempt = %+v, want -3 from %q to the first /a %q", c, k[3], k[0])
		}
		if h := nhHist(t, ctx); h != h0 || nhKey(t, ctx) != k[3] {
			t.Errorf("denied go(-3): %s, key %q", h, nhKey(t, ctx))
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.go(-3); true`, nil)
		nhOn(t, ctx, "/a")
		time.Sleep(300 * time.Millisecond)
		if h := nhHist(t, ctx); h != "about:blank /a@ /b /a /b" || nhKey(t, ctx) != k[0] {
			t.Errorf("allowed go(-3) lands on %s, key %q, want the first /a %q", h, nhKey(t, ctx), k[0])
		}
	})
}

// TestNavGuardHistoryReload: the tags live in history.state, so they
// survive a reload; Back from the reloaded page is undone and replayed
// by them like any other.
func TestNavGuardHistoryReload(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		ngGo(t, ctx, "/b")
		kB := nhKey(t, ctx)
		ngGo(t, ctx, "/c")
		kC := nhKey(t, ctx)
		ngRun(t, ctx, chromedp.Reload(), chromedp.WaitVisible(`#ready`, chromedp.ByID))
		if k := nhKey(t, ctx); k != kC {
			t.Fatalf("the reloaded entry is %q, was %q", k, kC)
		}
		ngInstall(t, ctx, "deny")
		h0 := nhHist(t, ctx)
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `window.__doc = 1; history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied Back after the reload")
		time.Sleep(300 * time.Millisecond)
		c := ngCalls(t, ctx)
		if len(c) != 1 || intp(c[0].Delta) != "-1" || strp(c[0].ToEntry) != kB {
			t.Errorf("Back after a reload = %+v, want -1 to %q", c, kB)
		}
		var doc int
		ngEval(t, ctx, `window.__doc || 0`, &doc)
		if s := ngRead(t, ctx); s.URL != "/c" || s.Page != "page-c" || s.Note != "draft" || doc != 1 {
			t.Errorf("denied Back after a reload: %+v (same document: %v)", s, doc == 1)
		}
		if h := nhHist(t, ctx); h != h0 {
			t.Errorf("denied Back after a reload changed the history: %s -> %s", h0, h)
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/b")
		if k := nhKey(t, ctx); k != kB || nhHist(t, ctx) != "about:blank /a /b@ /c" {
			t.Errorf("allowed Back after a reload: %s key %q want %q", nhHist(t, ctx), k, kB)
		}
	})
}

// TestNavGuardHistoryReplaceAndBranch: a replace keeps the entry's
// identity; a push after Back drops the entries ahead and takes the
// dropped entry's place with a new identity, so the place alone never
// names an entry.
func TestNavGuardHistoryReplaceAndBranch(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		ngGo(t, ctx, "/b")
		kB := nhKey(t, ctx)
		ngEval(t, ctx, `__gofastr.navigate('/d', { replace: true }); true`, nil)
		nhOn(t, ctx, "/d")
		if k := nhKey(t, ctx); k != kB || nhHist(t, ctx) != "about:blank /a /d@" {
			t.Fatalf("a replace keeps the entry: %s key %q want %q", nhHist(t, ctx), k, kB)
		}
		ngGo(t, ctx, "/c")
		kC := nhKey(t, ctx)
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/d")
		ngGo(t, ctx, "/b")
		kB2 := nhKey(t, ctx)
		if kB2 == kC || kB2 == kB {
			t.Fatalf("the branch's entry must be new: %q (dropped /c was %q)", kB2, kC)
		}
		h0 := nhHist(t, ctx)
		if h0 != "about:blank /a /d /b@" {
			t.Fatalf("branch history %s", h0)
		}
		ngMode(t, ctx, "deny")
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `__ng.calls.length = 0; history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied Back")
		time.Sleep(300 * time.Millisecond)
		if c := ngCalls(t, ctx); len(c) != 1 || strp(c[0].ToEntry) != kB || strp(c[0].FromEntry) != kB2 || intp(c[0].Delta) != "-1" {
			t.Errorf("Back from the branch = %+v, want -1 from %q to %q", c, kB2, kB)
		}
		if h := nhHist(t, ctx); h != h0 || nhKey(t, ctx) != kB2 {
			t.Errorf("denied Back from the branch: %s", h)
		}
		if s := ngRead(t, ctx); s.URL != "/b" || s.Note != "draft" {
			t.Errorf("denied Back from the branch: %+v", s)
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/d")
		if h := nhHist(t, ctx); h != "about:blank /a /d@ /b" || nhKey(t, ctx) != kB {
			t.Errorf("allowed Back from the branch: %s", h)
		}
	})
}

// TestNavGuardHistoryLateModule: core tags every entry it writes
// before any module exists, so a guard module loaded late still knows
// the entries the router wrote earlier.
func TestNavGuardHistoryLateModule(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "")
		for _, p := range []string{"/b", "/c"} {
			ngEval(t, ctx, `__gofastr.navigate('`+p+`')`, nil)
			nhOn(t, ctx, p)
		}
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/b")
		kB := nhKey(t, ctx)
		ngGo(t, ctx, "/d")
		var loaded bool
		ngEval(t, ctx, `!!__gofastr.loadedModules?.navguard`, &loaded)
		if loaded {
			t.Fatal("the guard module loaded before the test asked for it")
		}
		ngInstall(t, ctx, "deny")
		h0 := nhHist(t, ctx)
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied Back")
		time.Sleep(300 * time.Millisecond)
		if c := ngCalls(t, ctx); len(c) != 1 || intp(c[0].Delta) != "-1" || strp(c[0].ToEntry) != kB {
			t.Errorf("Back onto an entry written before the module = %+v, want -1 to %q", c, kB)
		}
		if h := nhHist(t, ctx); h != h0 || h != "about:blank /a /b /d@" {
			t.Errorf("denied Back: %s -> %s", h0, h)
		}
	})
}

// nhForeign: without the API, an entry no router wrote has no tag, so
// the way back from it is unknown. A Back onto it is never answered
// with a push: denied, that landing alone takes the page's URL and a
// fresh identity of its own, keeping whatever state its writer put on
// it; reason 'unknown'.
func nhForeign(t *testing.T) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, true, "allow")
	ngGo(t, ctx, "/b")
	ngEval(t, ctx, `history.pushState({ mine: 1 }, '', '/c'); true`, nil)
	ngGo(t, ctx, "/d")
	kD := nhKey(t, ctx)
	h0 := nhHist(t, ctx)
	c.want(h0 == "about:blank /a /b /c /d@", "setup history %s", h0)
	ngMode(t, ctx, "deny")
	ngType(t, ctx, "draft")
	ngEval(t, ctx, `__ng.calls.length = 0; history.back(); true`, nil)
	if !nhUntil(ctx, `__ng.blocked.length === 1`, 4*time.Second) {
		return append(c.bad, "the Back onto the foreign entry was not refused")
	}
	time.Sleep(300 * time.Millisecond)
	calls := ngCalls(t, ctx)
	c.want(len(calls) == 1 && calls[0].To == "/c" && calls[0].Delta == nil && calls[0].ToEntry == nil && strp(calls[0].FromEntry) == kD,
		"attempt = %+v, want /c with no distance or entry, from %q", calls, kD)
	b := ngBlocks(t, ctx)
	c.want(len(b) == 1 && b[0].Reason == "unknown", "blocked = %+v, want 'unknown'", b)
	h := nhHist(t, ctx)
	c.want(h == "about:blank /a /b /d@ /d", "a refused unknown Back may only rename its landing: %s -> %s", h0, h)
	s := ngRead(t, ctx)
	c.want(s.URL == "/d" && s.Page == "page-d" && s.Note == "draft" && s.Focus == "note-d", "refused unknown Back: %+v", s)
	var mine int
	ngEval(t, ctx, `history.state && history.state.mine || 0`, &mine)
	k := nhKey(t, ctx)
	c.want(mine == 1 && k != "" && k != kD, "the landing keeps its writer's state (%d) and takes an identity of its own (%q, the page's is %q)", mine, k, kD)
	return c.bad
}

// TestNavGuardHistoryForeignEntries: history written around the router.
// With the API its entries have keys, and the undo is exact. Without
// it, see nhForeign; an allow sends the landing on to the router.
func TestNavGuardHistoryForeignEntries(t *testing.T) {
	t.Run("no-api", func(t *testing.T) {
		nhReport(t, nhForeign(t))
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, true, "allow")
		ngGo(t, ctx, "/b")
		ngEval(t, ctx, `history.pushState({ mine: 2 }, '', '/c'); true`, nil)
		ngGo(t, ctx, "/d")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/c")
		time.Sleep(300 * time.Millisecond)
		if h := nhHist(t, ctx); h != "about:blank /a /b /c@ /d" || h0 != "about:blank /a /b /c /d@" {
			t.Errorf("an allowed Back onto a foreign entry: %s -> %s", h0, h)
		}
		if b := ngBlocks(t, ctx); len(b) != 0 {
			t.Errorf("allowed: %+v", b)
		}
	})
	t.Run("api", func(t *testing.T) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, false, "allow")
		ngGo(t, ctx, "/b")
		ngEval(t, ctx, `history.pushState({ mine: 1 }, '', '/c'); true`, nil)
		ngGo(t, ctx, "/d")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "deny")
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denied Back")
		time.Sleep(300 * time.Millisecond)
		if b := ngBlocks(t, ctx); b[0].Reason != "denied" {
			t.Errorf("with the API the foreign entry has a key: %+v", b)
		}
		if h := nhHist(t, ctx); h != h0 {
			t.Errorf("denied Back onto a foreign entry: %s -> %s", h0, h)
		}
	})
}

// TestNavGuardHistoryTraverseToRefused: when the Navigation API
// refuses the undo (traverseTo throws or its commit rejects), the move
// has no way back and takes the unknown path: denied, the landing alone
// takes the page's URL and keeps its key; allowed, the router loads it.
func TestNavGuardHistoryTraverseToRefused(t *testing.T) {
	stubs := map[string]string{
		"rejects": `navigation.traverseTo = () => { const p = Promise.reject(new DOMException('refused', 'AbortError')); return { committed: p, finished: p }; }; true`,
		"throws":  `navigation.traverseTo = () => { throw new DOMException('refused', 'InvalidStateError'); }; true`,
	}
	for name, stub := range stubs {
		t.Run(name, func(t *testing.T) {
			open := func(mode string) (context.Context, []string) {
				r := newNGRig(t, false)
				ctx := nhOpenRig(t, r, false, "allow")
				ngGo(t, ctx, "/b")
				ngGo(t, ctx, "/c")
				keys := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
				ngEval(t, ctx, stub, nil)
				ngMode(t, ctx, mode)
				ngType(t, ctx, "draft")
				ngEval(t, ctx, `history.back(); true`, nil)
				return ctx, keys
			}
			ctx, keys := open("deny")
			ngWait(t, ctx, `__ng.blocked.length === 1`, "the refused Back")
			time.Sleep(300 * time.Millisecond)
			if b := ngBlocks(t, ctx); b[0].Reason != "unknown" {
				t.Errorf("blocked = %+v, want 'unknown'", b)
			}
			if h := nhHist(t, ctx); h != "about:blank /a /c@ /c" {
				t.Errorf("a refused undo may only rename its landing: %s", h)
			}
			if got := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`); strings.Join(got, ",") != strings.Join(keys, ",") {
				t.Errorf("entries changed: %v -> %v", keys, got)
			}
			if s := ngRead(t, ctx); s.URL != "/c" || s.Page != "page-c" || s.Note != "draft" || s.Key != keys[1] {
				t.Errorf("refused undo: %+v, want /c on /b's own key %q", s, keys[1])
			}
			// Allowed, the landing goes on to the router.
			ctx, keys = open("later")
			nhOn(t, ctx, "/b")
			time.Sleep(300 * time.Millisecond)
			if b := ngBlocks(t, ctx); len(b) != 0 {
				t.Errorf("allowed: %+v", b)
			}
			if h, k := nhHist(t, ctx), nhKey(t, ctx); h != "about:blank /a /b@ /c" || k != keys[1] {
				t.Errorf("allowed after a refused undo: %s key %q want %q", h, k, keys[1])
			}
		})
	}
}

// --- Composition with the headless leave guard ---

// nhLeaveRig: four pages; each page's input sits in a
// data-hui-leave-guard form, the real headless leave guard loads after
// the runtime, and each page carries a save that navigates (button and
// form) and a save the server redirects. themed adds the kit's confirm
// dialog template, which makes the leave guard ask asynchronously.
type nhLeaveRig struct {
	*ngRig
	saves, redirs atomic.Int32
}

func newNHLeaveRig(t *testing.T, themed bool) *nhLeaveRig {
	t.Helper()
	r := &nhLeaveRig{ngRig: &ngRig{}}
	inner := func(path string) string {
		n := strings.Trim(path, "/")
		return `<section id="page-` + n + `"><h1>` + strings.ToUpper(n) + `</h1>` +
			`<form id="f-` + n + `" data-hui-leave-guard><input id="note-` + n + `" aria-label="note ` + n + `"></form>` +
			`<button id="save" type="button" data-cui-rpc="/api/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/c">Save</button>` +
			`<button id="redir" type="button" data-cui-rpc="/api/redir" data-cui-rpc-method="POST">Move</button>` +
			`<form id="rf" data-hui-leave-guard data-cui-rpc="/api/save" data-cui-rpc-method="POST" data-cui-rpc-navigate="/c">` +
			`<input id="rf-in" name="x" aria-label="record"><button id="rf-go" type="submit">Save record</button></form>` +
			`<div style="block-size:2000px"></div></section>`
	}
	doc := func(path string) string {
		s := `<!doctype html><html lang="en"><head><title>leave</title>` +
			`<script type="application/json" id="gofastr-routes">` + ngRoutes + `</script></head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="toA" href="/a">A</a> <a id="toB" href="/b">B</a> <a id="toC" href="/c">C</a> <a id="toD" href="/d">D</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner(path) + `</main></div>`
		if themed {
			s += `<template data-cui-confirm-dialog><dialog><p data-cui-confirm-message></p><button value="cancel">Cancel</button><button value="ok">OK</button></dialog></template>`
		}
		return s + `<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script><script src="/lg.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	ngServeRuntime(t, mux)
	nhServeLeaveGuard(t, mux)
	mux.HandleFunc("/api/save", func(w http.ResponseWriter, _ *http.Request) {
		r.saves.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/redir", func(w http.ResponseWriter, req *http.Request) {
		r.redirs.Add(1)
		http.Redirect(w, req, "/d", http.StatusSeeOther)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if req.Header.Get("X-Gofastr-Navigate") != "1" {
			r.note("full " + req.URL.Path)
			fmt.Fprint(w, doc(req.URL.Path))
			return
		}
		r.note("partial " + req.URL.RequestURI())
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		fmt.Fprint(w, inner(req.URL.Path))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// nhConfirm stubs window.confirm: __cf counts the asks, __ans answers.
const nhConfirm = `window.__cf = 0; window.__ans = false; window.confirm = () => { __cf++; return __ans; }; true`

func nhInt(t *testing.T, ctx context.Context, js string) int {
	t.Helper()
	var n int
	ngEval(t, ctx, js, &n)
	return n
}

// TestNavGuardHistoryLeaveGuard: the public guard and the real leave
// guard on one page. A dirty form's Cancel keeps the page, the draft
// and the history whichever guard answered first; OK leaves once, on
// the entry the reader chose.
func TestNavGuardHistoryLeaveGuard(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNHLeaveRig(t, false)
		ctx := nhOpenRig(t, r.ngRig, noAPI, "allow")
		ngEval(t, ctx, nhConfirm, nil)
		ngGo(t, ctx, "/b")
		ngGo(t, ctx, "/c")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/b")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "later")
		ngType(t, ctx, "draft")
		for i, act := range []string{`history.back()`, `history.forward()`, `document.getElementById('toD').click()`} {
			ngEval(t, ctx, act+`; true`, nil)
			ngWait(t, ctx, fmt.Sprintf(`__cf === %d`, i+1), "the leave guard's ask after "+act)
			time.Sleep(500 * time.Millisecond)
			if s := ngRead(t, ctx); s.URL != "/b" || s.Page != "page-b" || s.Note != "draft" || s.Stamp != "alive" {
				t.Errorf("%s, Cancel: %+v", act, s)
			}
			if h := nhHist(t, ctx); h != h0 {
				t.Errorf("%s, Cancel changed the history: %s -> %s", act, h0, h)
			}
		}
		ngEval(t, ctx, `__ans = true; __cf = 0; __ng.calls.length = 0; history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		time.Sleep(300 * time.Millisecond)
		if h := nhHist(t, ctx); h != "about:blank /a@ /b /c" || nhInt(t, ctx, `__cf`) != 1 || nhInt(t, ctx, `__ng.calls.length`) != 1 {
			t.Errorf("Back, OK: %s, confirms %d, guard calls %d", h, nhInt(t, ctx, `__cf`), nhInt(t, ctx, `__ng.calls.length`))
		}
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `__cf = 0; __ng.calls.length = 0; history.forward(); true`, nil)
		nhOn(t, ctx, "/b")
		time.Sleep(300 * time.Millisecond)
		if h := nhHist(t, ctx); h != "about:blank /a /b@ /c" || nhInt(t, ctx, `__cf`) != 1 {
			t.Errorf("Forward, OK: %s, confirms %d", h, nhInt(t, ctx, `__cf`))
		}
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `__cf = 0; document.getElementById('toD').click(); true`, nil)
		nhOn(t, ctx, "/d")
		if h := nhHist(t, ctx); h != "about:blank /a /b /d@" || nhInt(t, ctx, `__cf`) != 1 {
			t.Errorf("link, OK: %s, confirms %d", h, nhInt(t, ctx, `__cf`))
		}
	})
}

// TestNavGuardHistoryThemedDiscard: with the kit's dialog the leave
// guard asks asynchronously, after the public guard allowed the Back
// and the engine let it through. Cancel keeps everything; Discard
// makes the move again, which the public guard is asked about once
// more (two calls for one Back, one dialog), and lands on /a.
func TestNavGuardHistoryThemedDiscard(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNHLeaveRig(t, true)
		ctx := nhOpenRig(t, r.ngRig, noAPI, "allow")
		ngEval(t, ctx, `(async () => { await __gofastr.loadModule('confirm'); window.__asks = 0; __gofastr.askWords = () => { __asks++; return new Promise((r) => setTimeout(() => r(window.__discard), 100)); }; return true; })()`, nil)
		ngGo(t, ctx, "/b")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "later")
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `window.__discard = false; __ng.calls.length = 0; history.back(); true`, nil)
		ngWait(t, ctx, `__asks === 1`, "the dialog")
		time.Sleep(600 * time.Millisecond)
		if s := ngRead(t, ctx); s.URL != "/b" || s.Note != "draft" || nhHist(t, ctx) != h0 {
			t.Errorf("Cancel in the dialog: %+v %s", s, nhHist(t, ctx))
		}
		ngEval(t, ctx, `window.__discard = true; __asks = 0; __ng.calls.length = 0; history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		time.Sleep(500 * time.Millisecond)
		calls, asks := nhInt(t, ctx, `__ng.calls.length`), nhInt(t, ctx, `__asks`)
		if h := nhHist(t, ctx); h != "about:blank /a@ /b" || calls != 2 || asks != 1 {
			t.Errorf("Discard: %s, public guard calls %d (want 2), dialog asks %d (want 1)", h, calls, asks)
		}
	})
}

// TestNavGuardHistoryOverlayLeaveScope: under an intercept stack a
// move discards only the layers it closes. A dirty leave-guard form on
// the page under the stack is not asked about when a layer closes; one
// inside the layer is, and its Cancel keeps the layer on its entry.
func TestNavGuardHistoryOverlayLeaveScope(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		srv := ngStackServer(t)
		ctx := nhCtx(t, noAPI)
		nhLoad(t, ctx, srv.URL+"/list", noAPI)
		ngEval(t, ctx, `new Promise((res, rej) => { const s = document.createElement('script'); s.src = '/lg.js'; s.onload = () => res(true); s.onerror = rej; document.body.append(s); })`, nil)
		ngInstall(t, ctx, "allow")
		ngEval(t, ctx, nhConfirm, nil)
		ngEval(t, ctx, `document.getElementById('list-main').insertAdjacentHTML('beforeend', '<form id="list-form" data-hui-leave-guard><input id="list-in" aria-label="filter"></form>'); true`, nil)
		ngRun(t, ctx, chromedp.SendKeys(`#list-in`, "page draft", chromedp.ByID))
		ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
		ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
		esc := `document.getElementById('rec-a').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); true`
		ngEval(t, ctx, esc, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept')?.children.length && location.pathname === '/list'`, "the closed layer")
		c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)
		if nhInt(t, ctx, `__cf`) != 0 || len(c) != 1 || c[0].AffectsList {
			t.Errorf("closing a layer over a dirty page form: confirms %d, attempts %+v", nhInt(t, ctx, `__cf`), c)
		}
		ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
		ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer again")
		ngEval(t, ctx, `document.getElementById('rec-a').insertAdjacentHTML('beforeend', '<form id="rec-form" data-hui-leave-guard><input id="rec-lg" aria-label="field"></form>'); true`, nil)
		ngRun(t, ctx, chromedp.SendKeys(`#rec-lg`, "layer draft", chromedp.ByID))
		before := ngReadLayer(t, ctx)
		h0 := nhHist(t, ctx)
		ngEval(t, ctx, esc, nil)
		ngWait(t, ctx, `__cf === 1`, "the leave guard's ask for the layer")
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `__cf === 2`, "the leave guard's ask for Back")
		time.Sleep(500 * time.Millisecond)
		if l := ngReadLayer(t, ctx); l.Layers != 1 || l.URL != "/rec/a" || l.Key != before.Key || nhHist(t, ctx) != h0 {
			t.Errorf("Cancel keeps the layer on its entry: %+v (was %+v), %s -> %s", l, before, h0, nhHist(t, ctx))
		}
		ngEval(t, ctx, `__ans = true; true`, nil)
		ngEval(t, ctx, esc, nil)
		ngWait(t, ctx, `!document.getElementById('cui-intercept')?.children.length && location.pathname === '/list'`, "the discarded layer")
		var page string
		ngEval(t, ctx, `document.getElementById('list-in').value + '|' + document.getElementById('list-form').hasAttribute('data-hui-dirty')`, &page)
		if page != "page draft|true" {
			t.Errorf("the page's own dirty form is untouched by the layer's discard: %s", page)
		}
	})
}

// --- The intercept stack without the API ---

// nhStack opens /list, installs the guard in mode, opens the record
// layer and types a draft into it.
func nhStack(t *testing.T, noAPI bool, mode string) context.Context {
	t.Helper()
	srv := ngStackServer(t)
	ctx := nhCtx(t, noAPI)
	nhLoad(t, ctx, srv.URL+"/list", noAPI)
	ngInstall(t, ctx, mode)
	ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
	ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
	ngRun(t, ctx, chromedp.Focus(`#rec-note`, chromedp.ByID), chromedp.SendKeys(`#rec-note`, "draft", chromedp.ByID))
	return ctx
}

// TestNavGuardHistoryStack: Back inside the stack (the stack's own
// exact undo) and a move off it to a page further afield (the engine's
// undo by the tags), denied and allowed, keep the layer on its entry
// and the history whole.
func TestNavGuardHistoryStack(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		t.Run("pop", func(t *testing.T) {
			ctx := nhStack(t, noAPI, "hold")
			before := ngReadLayer(t, ctx)
			h0 := nhHist(t, ctx)
			ngEval(t, ctx, `history.back(); true`, nil)
			ngWait(t, ctx, `!!__ng.held[0]`, "Back's decision")
			ngWait(t, ctx, nhKeyJS+` === '`+before.Key+`'`, "the stack's undo")
			ngEval(t, ctx, `__ng.held[0].r(false); true`, nil)
			ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
			time.Sleep(300 * time.Millisecond)
			if l := ngReadLayer(t, ctx); l.Layers != 1 || l.Note == nil || *l.Note != "draft" || l.Key != before.Key || nhHist(t, ctx) != h0 {
				t.Errorf("denied Back in the stack: %+v %s (was %s)", l, nhHist(t, ctx), h0)
			}
			ngMode(t, ctx, "later")
			ngEval(t, ctx, `history.back(); true`, nil)
			ngWait(t, ctx, `!document.getElementById('cui-intercept')?.children.length && location.pathname === '/list'`, "the allowed Back")
			time.Sleep(300 * time.Millisecond)
			if h := nhHist(t, ctx); h != "about:blank /list@ /rec/a" || nhInt(t, ctx, `__ng.calls.length`) != 2 {
				t.Errorf("allowed Back in the stack: %s, calls %d", h, nhInt(t, ctx, `__ng.calls.length`))
			}
		})
		t.Run("afield", func(t *testing.T) {
			srv := ngStackServer(t)
			ctx := nhCtx(t, noAPI)
			nhLoad(t, ctx, srv.URL+"/pre", noAPI)
			kPre := nhKey(t, ctx)
			ngInstall(t, ctx, "allow")
			ngEval(t, ctx, `__gofastr.navigate('/list')`, nil)
			ngWait(t, ctx, `!!document.getElementById('to-a') && location.pathname === '/list'`, "the list")
			ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
			ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
			ngRun(t, ctx, chromedp.Focus(`#rec-note`, chromedp.ByID), chromedp.SendKeys(`#rec-note`, "draft", chromedp.ByID))
			ngEval(t, ctx, `__ng.calls.length = 0; __ngMode = 'hold'; true`, nil)
			before := ngReadLayer(t, ctx)
			h0 := nhHist(t, ctx)
			if h0 != "about:blank /pre /list /rec/a@" {
				t.Fatalf("setup history %s", h0)
			}
			ngEval(t, ctx, `history.go(-2); true`, nil)
			ngWait(t, ctx, `!!__ng.held[0]`, "the decision")
			ngWait(t, ctx, nhKeyJS+` === '`+before.Key+`'`, "the undo onto the layer's own entry")
			c := ngJSON[[]ngStackCall](t, ctx, `__ng.calls`)
			if len(c) != 1 || c[0].Intent != "traverse" || c[0].To != "/pre" || strp(c[0].ToEntry) != kPre || intp(c[0].Delta) != "-2" || strp(c[0].FromEntry) != before.Key || !c[0].AffectsNote {
				t.Errorf("a move off the stack = %+v, want a whole-page traverse by -2 from %q to %q", c, before.Key, kPre)
			}
			ngEval(t, ctx, `__ng.held[0].r(false); true`, nil)
			ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
			time.Sleep(300 * time.Millisecond)
			if l := ngReadLayer(t, ctx); l.Layers != 1 || l.Note == nil || *l.Note != "draft" || l.Key != before.Key || nhHist(t, ctx) != h0 {
				t.Errorf("denied move off the stack: %+v %s (was %s)", l, nhHist(t, ctx), h0)
			}
			ngMode(t, ctx, "later")
			ngEval(t, ctx, `history.go(-2); true`, nil)
			ngWait(t, ctx, `!document.getElementById('cui-intercept')?.children.length && location.pathname === '/pre' && /FULL \/pre|plain \/pre/.test(document.querySelector('main').textContent)`, "the allowed move")
			time.Sleep(300 * time.Millisecond)
			if h := nhHist(t, ctx); h != "about:blank /pre@ /list /rec/a" || nhKey(t, ctx) != kPre || nhInt(t, ctx, `__ng.calls.length`) != 2 {
				t.Errorf("allowed move off the stack: %s key %q, calls %d", h, nhKey(t, ctx), nhInt(t, ctx, `__ng.calls.length`))
			}
		})
	})
}

// nhM8b: a move off the stack while the record layer shows. A
// navigate() fired while the engine's undo is in flight is refused
// 'pending', and that refusal and the denial both come from the
// layer's entry, the one the page still shows, not the landing.
func nhM8b(t *testing.T) []string {
	var c nhCheck
	srv := ngStackServer(t)
	ctx := nhCtx(t, false)
	nhLoad(t, ctx, srv.URL+"/pre", false)
	ngInstall(t, ctx, "allow")
	ngEval(t, ctx, `__gofastr.navigate('/list')`, nil)
	ngWait(t, ctx, `!!document.getElementById('to-a') && location.pathname === '/list'`, "the list")
	ngRun(t, ctx, chromedp.Click(`#to-a`, chromedp.ByID))
	ngWait(t, ctx, `!!document.getElementById('rec-note') && location.pathname === '/rec/a'`, "the record layer")
	before := ngReadLayer(t, ctx)
	all := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
	ngEval(t, ctx, `__ng.calls.length = 0; __ngMode = 'hold'; window.__from = [];
addEventListener('gofastr:navigateblocked', (e) => __from.push([e.detail.reason, e.detail.attempt.from.entry, new URL(e.detail.attempt.from.url).pathname]));
window.__armed = true; addEventListener('popstate', () => { if (!__armed) return; __armed = false; __gofastr.navigate('/list?x=1'); });
history.go(-2); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[0]`, 4*time.Second) {
		return append(c.bad, "the move off the stack was never asked about")
	}
	c.want(nhUntil(ctx, `navigation.currentEntry.key === '`+before.Key+`'`, 4*time.Second), "the undo did not land")
	ngEval(t, ctx, `__ng.held[0].r(false); true`, nil)
	c.want(nhUntil(ctx, `__from.length === 2`, 4*time.Second), "the reports: %v", ngJSON[[][]string](t, ctx, `__from`))
	time.Sleep(300 * time.Millisecond)
	from := ngJSON[[][]string](t, ctx, `__from`)
	want := [][]string{{"pending", before.Key, "/rec/a"}, {"denied", before.Key, "/rec/a"}}
	c.want(fmt.Sprint(from) == fmt.Sprint(want), "blocked from = %v, want %v", from, want)
	l := ngReadLayer(t, ctx)
	now := ngJSON[[]string](t, ctx, `navigation.entries().map((e) => e.key)`)
	c.want(l.Layers == 1 && l.URL == "/rec/a" && l.Key == before.Key && strings.Join(now, ",") == strings.Join(all, ","), "after the denial: %+v entries %v (were %v)", l, now, all)
	// The next move departs from the layer's entry too.
	ngEval(t, ctx, `__ngMode = 'deny'; __ng.calls.length = 0; document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); true`, nil)
	c.want(nhUntil(ctx, `__ng.calls.length === 1`, 4*time.Second), "Esc was not asked about")
	next := ngCalls(t, ctx)
	c.want(len(next) == 1 && strp(next[0].FromEntry) == before.Key, "the next move's from = %+v, want %q", next, before.Key)
	return c.bad
}

func TestNavGuardHistoryPendingFromLayer(t *testing.T) { nhReport(t, nhM8b(t)) }

// --- Failing, disposed and superseded guards ---

// nhThenGetter: a guard whose answer's then getter throws denies the
// move and reports the throw as 'error'; nothing escapes as an
// uncaught error, the page and the history stay, and with that guard
// disposed the next move goes.
func nhThenGetter(t *testing.T, noAPI bool, act string) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, noAPI, "allow")
	if strings.HasPrefix(act, "history") {
		ngGo(t, ctx, "/c")
	}
	h0, s0 := nhHist(t, ctx), ngRead(t, ctx)
	ngEval(t, ctx, `window.__errs = []; addEventListener('error', (e) => __errs.push(String(e.message))); addEventListener('unhandledrejection', (e) => __errs.push(String(e.reason))); __stop(); window.__tg = __gofastr.guardNavigation(() => ({ get then() { throw new Error('getter'); } })); true`, nil)
	ngEval(t, ctx, act+`; true`, nil)
	c.want(nhUntil(ctx, `__ng.blocked.length === 1`, 4*time.Second), "%s: the throw was not reported", act)
	time.Sleep(400 * time.Millisecond)
	b := ngBlocks(t, ctx)
	c.want(len(b) == 1 && b[0].Reason == "error" && b[0].Error == "getter", "%s: blocked = %+v, want one 'error' carrying the throw", act, b)
	errs := ngJSON[[]string](t, ctx, `__errs`)
	c.want(len(errs) == 0, "%s: uncaught: %v", act, errs)
	s := ngRead(t, ctx)
	c.want(s.URL == s0.URL && s.Page == s0.Page && s.Stamp == "alive", "%s: %+v, was %+v", act, s, s0)
	h := nhHist(t, ctx)
	c.want(h == h0, "%s: history %s -> %s", act, h0, h)
	ngEval(t, ctx, `__tg(); document.getElementById('toB').click(); true`, nil)
	c.want(nhUntil(ctx, `location.pathname === '/b' && `+nhSettled, 4*time.Second), "%s: the next move after disposing the guard did not go: %+v", act, ngRead(t, ctx))
	return c.bad
}

func TestNavGuardHistoryThrowingThenGetter(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		for _, act := range []string{`document.getElementById('toB').click()`, `history.back()`} {
			t.Run(strings.Fields(act)[0], func(t *testing.T) { nhReport(t, nhThenGetter(t, noAPI, act)) })
		}
	})
}

// nhSuperseded: a link held by the guard, then a Back. The Back
// replaces the link's decision: the link's signal aborts and its late
// allow moves nothing; the Back's denial keeps the page and history.
func nhSuperseded(t *testing.T, noAPI bool) []string {
	var c nhCheck
	r := newNGRig(t, false)
	ctx := nhOpenRig(t, r, noAPI, "allow")
	ngGo(t, ctx, "/b")
	kB := nhKey(t, ctx)
	h0 := nhHist(t, ctx)
	ngMode(t, ctx, "hold")
	ngType(t, ctx, "draft")
	ngEval(t, ctx, `document.getElementById('toD').click(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[0]`, 4*time.Second) {
		return append(c.bad, "the link was never asked about")
	}
	ngEval(t, ctx, `history.back(); true`, nil)
	if !nhUntil(ctx, `!!__ng.held[1]`, 4*time.Second) {
		return append(c.bad, "the Back was never asked about")
	}
	var aborted bool
	ngEval(t, ctx, `__ng.held[0].nav.signal.aborted`, &aborted)
	c.want(aborted, "the replaced link's signal did not abort")
	c.want(nhUntil(ctx, nhKeyJS+` === '`+kB+`'`, 4*time.Second), "the undo did not land")
	ngEval(t, ctx, `__ng.held[0].r(true); true`, nil)
	time.Sleep(600 * time.Millisecond)
	s := ngRead(t, ctx)
	c.want(s.URL == "/b" && s.Page == "page-b" && s.Note == "draft", "the replaced link's late allow moved the page: %+v", s)
	ngEval(t, ctx, `__ng.held[1].r(false); true`, nil)
	c.want(nhUntil(ctx, `__ng.blocked.length >= 1`, 4*time.Second), "the Back's denial was not reported")
	time.Sleep(300 * time.Millisecond)
	s = ngRead(t, ctx)
	c.want(s.URL == "/b" && s.Page == "page-b" && s.Note == "draft", "after the denial: %+v", s)
	b := ngBlocks(t, ctx)
	c.want(len(b) == 1 && b[0].Intent == "traverse" && b[0].Reason == "denied", "blocked = %+v, want the Back's denial alone (a replaced move does not report)", b)
	h := nhHist(t, ctx)
	c.want(h == h0, "history %s -> %s", h0, h)
	return c.bad
}

// TestNavGuardHistorySupersedeAndDispose: see nhSuperseded. A guard
// disposed while its decision is pending ends that decision as a
// refusal ('disposed') with the history put back; a link or navigate()
// fired while the engine's undo is in flight is refused 'pending', and
// the traversal still lands where the reader sent it once allowed.
func TestNavGuardHistorySupersedeAndDispose(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		t.Run("superseded", func(t *testing.T) { nhReport(t, nhSuperseded(t, noAPI)) })
		t.Run("disposed", func(t *testing.T) {
			r := newNGRig(t, false)
			ctx := nhOpenRig(t, r, noAPI, "allow")
			ngGo(t, ctx, "/b")
			ngGo(t, ctx, "/c")
			kC := nhKey(t, ctx)
			h0 := nhHist(t, ctx)
			ngMode(t, ctx, "hold")
			ngType(t, ctx, "draft")
			ngEval(t, ctx, `history.back(); true`, nil)
			ngWait(t, ctx, `!!__ng.held[0]`, "the decision")
			ngWait(t, ctx, nhKeyJS+` === '`+kC+`'`, "the undo")
			ngEval(t, ctx, `__stop(); true`, nil)
			ngWait(t, ctx, `__ng.blocked.length === 1`, "the disposal")
			time.Sleep(300 * time.Millisecond)
			var aborted bool
			ngEval(t, ctx, `__ng.held[0].nav.signal.aborted`, &aborted)
			if b := ngBlocks(t, ctx); b[0].Reason != "disposed" || !aborted {
				t.Errorf("blocked = %+v, aborted %v", b, aborted)
			}
			if s := ngRead(t, ctx); s.URL != "/c" || s.Page != "page-c" || s.Note != "draft" || nhHist(t, ctx) != h0 {
				t.Errorf("disposed: %+v %s", s, nhHist(t, ctx))
			}
			ngEval(t, ctx, `__ng.held[0].r(true); history.back(); true`, nil)
			nhOn(t, ctx, "/b")
		})
		t.Run("pending", func(t *testing.T) {
			r := newNGRig(t, false)
			ctx := nhOpenRig(t, r, noAPI, "allow")
			ngGo(t, ctx, "/b")
			h0 := nhHist(t, ctx)
			ngMode(t, ctx, "later")
			ngEval(t, ctx, `window.__armed = true; addEventListener('popstate', () => { if (!__armed) return; __armed = false; document.getElementById('toD').click(); __gofastr.navigate('/c'); }); history.back(); true`, nil)
			nhOn(t, ctx, "/a")
			time.Sleep(500 * time.Millisecond)
			b := ngBlocks(t, ctx)
			if len(b) != 2 || b[0].Reason != "pending" || b[1].Reason != "pending" || b[0].Intent != "push" {
				t.Errorf("blocked = %+v, want the link and navigate() refused 'pending'", b)
			}
			if h := nhHist(t, ctx); h != "about:blank /a@ /b" || h0 != "about:blank /a /b@" {
				t.Errorf("the traversal lands where it was going: %s -> %s", h0, h)
			}
		})
	})
}

// --- Envelope navigation ---

// TestNavGuardHistoryEnvelope: under envelope navigation a denied Back
// keeps the page, its draft, focus and the history; an allowed Back
// restores the destination's scroll position and its outlet; rapid
// traversals end coherent: the page, its outlet and the URL agree, and
// the entries are the ones there were.
func TestNavGuardHistoryEnvelope(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, true)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		ngEval(t, ctx, `scrollTo(0, 700); true`, nil)
		time.Sleep(200 * time.Millisecond)
		ngGo(t, ctx, "/b")
		ngGo(t, ctx, "/c")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/b")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "deny")
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
		time.Sleep(300 * time.Millisecond)
		if s := ngRead(t, ctx); s.URL != "/b" || s.Page != "page-b" || s.Note != "draft" || s.Focus != "note-b" || nhHist(t, ctx) != h0 {
			t.Errorf("denied Back under the envelope: %+v %s (was %s)", s, nhHist(t, ctx), h0)
		}
		ngMode(t, ctx, "later")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		ngWait(t, ctx, `Math.abs(scrollY - 700) < 4`, "the restored scroll position")
		// The aside the page's URL names: /a's is the first document's.
		asideOK := func(url, aside string) bool { return aside == "ASIDE "+url || url == "/a" && aside == "ASIDE" }
		var aside string
		ngEval(t, ctx, `document.getElementById('aside').textContent`, &aside)
		if s := ngRead(t, ctx); !asideOK("/a", aside) || s.Focus != "main" || nhHist(t, ctx) != "about:blank /a@ /b /c" {
			t.Errorf("allowed Back under the envelope: %+v aside %q %s", s, aside, nhHist(t, ctx))
		}
		ngEval(t, ctx, `history.forward(); history.forward(); true`, nil)
		time.Sleep(2 * time.Second)
		ngQuiet(t, ctx)
		s := ngRead(t, ctx)
		ngEval(t, ctx, `document.getElementById('aside').textContent`, &aside)
		h := nhHist(t, ctx)
		if !asideOK(s.URL, aside) || s.Stamp != "alive" || strings.ReplaceAll(h, "@", "") != "about:blank /a /b /c" || !strings.Contains(h, s.URL+"@") {
			t.Errorf("rapid Forward x2 under the envelope: %+v aside %q %s", s, aside, h)
		}
	})
}

// nhPageLoads keeps the rig's page requests from its log: a partial,
// or a full document other than the favicon and the runtime's own
// endpoints.
func nhPageLoads(events []string) []string {
	var out []string
	for _, e := range events {
		if strings.HasPrefix(e, "partial ") || strings.HasPrefix(e, "full /") && !strings.HasPrefix(e, "full /favicon") && !strings.HasPrefix(e, "full /__gofastr/") {
			out = append(out, e)
		}
	}
	return out
}

// --- Moves the guards scope to the whole page ---

// TestNavGuardHistoryStatefulOnlyBack: a Back that changes only a
// pane's deep link loads nothing, yet it can close the pane and drop
// a form in it, which the guards cannot see: it is asked about as a
// page move (affects() is true for every node). Denied, the deep link
// comes back; with no guard it still loads nothing.
func TestNavGuardHistoryStatefulOnlyBack(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhOpenRig(t, r, noAPI, "allow")
		ngEval(t, ctx, `(() => { const d = document.createElement('div'); d.setAttribute('data-hui-pane-deeplink', 'pane'); d.id = 'panehost'; d.innerHTML = '<input id="pane-in" aria-label="pane">'; document.querySelector('main section').appendChild(d); __gofastr._pushURL('/a?pane=x'); return true; })()`, nil)
		h0 := nhHist(t, ctx)
		ngEval(t, ctx, `window.__aff = []; window.__deny = __gofastr.guardNavigation((nav) => { __aff.push([nav.intent, nav.affects(document.getElementById('pane-in')), nav.affects(document.getElementById('note-a'))]); return false; }); true`, nil)
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `__ng.blocked.length === 1`, "the denial")
		time.Sleep(300 * time.Millisecond)
		aff := ngJSON[[][]any](t, ctx, `__aff`)
		if fmt.Sprint(aff) != "[[traverse true true]]" {
			t.Errorf("a pane-only Back is asked as a page move: %v", aff)
		}
		if s := ngRead(t, ctx); s.URL != "/a?pane=x" || s.Page != "page-a" || s.Note != "draft" || nhHist(t, ctx) != h0 {
			t.Errorf("denied pane-only Back: %+v %s (was %s)", s, nhHist(t, ctx), h0)
		}
		ngEval(t, ctx, `__deny(); __stop(); true`, nil)
		mark := len(r.events())
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `location.search === ''`, "the unguarded Back")
		time.Sleep(400 * time.Millisecond)
		if e := nhPageLoads(r.events()[mark:]); len(e) != 0 {
			t.Errorf("a pane-only Back loads nothing: %v", e)
		}
	})
}

// TestNavGuardHistoryRPCNavigation: an RPC's data-cui-rpc-navigate and
// a redirect the RPC followed both go through navigate() and ask as a
// push once the write is done. Denied, the page stays and the write is
// not sent again; a form's own formresult has already cleared the
// leave guard, so the leave guard does not ask.
func TestNavGuardHistoryRPCNavigation(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNHLeaveRig(t, false)
		ctx := nhOpenRig(t, r.ngRig, noAPI, "allow")
		ngEval(t, ctx, nhConfirm, nil)
		ngGo(t, ctx, "/b")
		h0 := nhHist(t, ctx)
		ngMode(t, ctx, "deny")
		ngEval(t, ctx, `__ng.calls.length = 0; true`, nil)
		check := func(what string, writes *atomic.Int32, to string) {
			t.Helper()
			ngWait(t, ctx, `__ng.blocked.length === 1`, what+"'s denial")
			time.Sleep(500 * time.Millisecond)
			c := ngCalls(t, ctx)
			if len(c) != 1 || c[0].Intent != "push" || c[0].To != to {
				t.Errorf("%s: attempts %+v, want one push to %s", what, c, to)
			}
			if n := writes.Load(); n != 1 {
				t.Errorf("%s: the write was sent %d times", what, n)
			}
			if s := ngRead(t, ctx); s.URL != "/b" || s.Page != "page-b" || nhHist(t, ctx) != h0 || nhInt(t, ctx, `__cf`) != 0 {
				t.Errorf("%s denied: %+v %s, confirms %d", what, s, nhHist(t, ctx), nhInt(t, ctx, `__cf`))
			}
			ngEval(t, ctx, `__ng.calls.length = 0; __ng.blocked.length = 0; true`, nil)
			writes.Store(0)
		}
		ngRun(t, ctx, chromedp.Click(`#save`, chromedp.ByID))
		check("data-cui-rpc-navigate", &r.saves, "/c")
		ngRun(t, ctx, chromedp.SendKeys(`#rf-in`, "record", chromedp.ByID))
		ngRun(t, ctx, chromedp.Click(`#rf-go`, chromedp.ByID))
		check("a form's data-cui-rpc-navigate after its formresult", &r.saves, "/c")
		var dirty bool
		ngEval(t, ctx, `document.getElementById('rf').hasAttribute('data-hui-dirty')`, &dirty)
		if dirty {
			t.Error("the saved form is still dirty: its formresult did not reach the leave guard")
		}
		ngRun(t, ctx, chromedp.Click(`#redir`, chromedp.ByID))
		check("the RPC's followed redirect", &r.redirs, "/d")
		// Moving on later replays no write.
		ngMode(t, ctx, "allow")
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		ngEval(t, ctx, `history.forward(); true`, nil)
		nhOn(t, ctx, "/b")
		time.Sleep(300 * time.Millisecond)
		if r.saves.Load() != 0 || r.redirs.Load() != 0 {
			t.Errorf("a traversal replayed a write: saves %d, redirects %d", r.saves.Load(), r.redirs.Load())
		}
	})
}

// --- replaceURL ---

// TestNavGuardHistoryReplaceURL: __gofastr.replaceURL rewrites the URL
// of the entry the browser is on and nothing else: no fetch, no
// gofastr:navigate, no guard asked; the entry's identity, every tag and
// key on its state, the scroll position, the page, its draft and focus
// stay; currentPath follows; and a later Back is measured from it like
// any entry (a backward view transition, not a click). It refuses,
// writing nothing: another origin, a script URL, another document,
// outside the app's base, and, while a guarded traversal is unresolved,
// anything at all.
func TestNavGuardHistoryReplaceURL(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) {
		r := newNGRig(t, false)
		ctx := nhCtx(t, noAPI)
		ngRun(t, ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}))
		nhLoad(t, ctx, r.srv.URL+"/a", noAPI)
		ngWait(t, ctx, `!!window.__gofastr?.loadedModules?.transition`, "the transition module")
		ngInstall(t, ctx, "allow")
		ngGo(t, ctx, "/b")
		ngType(t, ctx, "draft")
		ngEval(t, ctx, `scrollTo(0, 500); history.replaceState({ ...history.state, mine: 7 }, ''); true`, nil)
		time.Sleep(200 * time.Millisecond)
		k0, h0 := nhKey(t, ctx), nhHist(t, ctx)
		state0 := ngJSON[map[string]any](t, ctx, `history.state`)
		mark := len(r.events())
		ngEval(t, ctx, `__ng.calls.length = 0; window.__navs = 0; addEventListener('gofastr:navigate', () => __navs++); true`, nil)

		var ok bool
		ngEval(t, ctx, `__gofastr.replaceURL('/b?tab=2')`, &ok)
		time.Sleep(400 * time.Millisecond)
		var cp string
		ngEval(t, ctx, `__gofastr.currentPath`, &cp)
		state1 := ngJSON[map[string]any](t, ctx, `history.state`)
		s := ngRead(t, ctx)
		if !ok || s.URL != "/b?tab=2" || cp != "/b?tab=2" || nhKey(t, ctx) != k0 || fmt.Sprint(state1) != fmt.Sprint(state0) {
			t.Errorf("replaceURL: ok %v, %+v, currentPath %q, key %q (was %q), state %v (was %v)", ok, s, cp, nhKey(t, ctx), k0, state1, state0)
		}
		if s.Page != "page-b" || s.Note != "draft" || s.Focus != "note-b" || s.ScrollY < 480 || s.Stamp != "alive" {
			t.Errorf("replaceURL must leave the page alone: %+v", s)
		}
		if h := nhHist(t, ctx); h != strings.Replace(h0, "/b@", "/b?tab=2@", 1) {
			t.Errorf("replaceURL history: %s -> %s", h0, h)
		}
		if e := nhPageLoads(r.events()[mark:]); len(e) != 0 || nhInt(t, ctx, `__navs`) != 0 || nhInt(t, ctx, `__ng.calls.length`) != 0 {
			t.Errorf("replaceURL fetched %v, navigated %d times, asked %d", e, nhInt(t, ctx, `__navs`), nhInt(t, ctx, `__ng.calls.length`))
		}

		var cross string
		ngEval(t, ctx, `[
  __gofastr.replaceURL('https://example.com/b'),
  __gofastr.replaceURL('javascript:alert(1)'),
  __gofastr.replaceURL('/doc'),
  (() => { window.__gofastr_locales = { base: '/app', prefixes: [] }; try { return __gofastr.replaceURL('/b?tab=3'); } finally { delete window.__gofastr_locales; } })(),
  `+nhRefusedWrite(`__gofastr.replaceURL('/b?tab=4')`)+`,
  __gofastr.currentPath,
].join()`, &cross)
		if cross != "false,false,false,false,false,/b?tab=2" || ngRead(t, ctx).URL != "/b?tab=2" {
			t.Errorf("replaceURL must refuse another origin, a script URL, another document, outside the base, and report a write the browser refused: %s, at %s", cross, ngRead(t, ctx).URL)
		}

		readTypes := captureTypes(ctx)
		ngEval(t, ctx, `history.back(); true`, nil)
		nhOn(t, ctx, "/a")
		c := ngCalls(t, ctx)
		if len(c) != 1 || c[0].From != "/b" || intp(c[0].Delta) != "-1" || strp(c[0].FromEntry) != k0 {
			t.Errorf("Back after replaceURL = %+v, want -1 from the rewritten entry %q", c, k0)
		}
		ngWait(t, ctx, `(window.__vtTypes || []).length === 1`, "the view transition")
		if ty := readTypes(); len(ty) != 1 || ty[0][0] != "back" {
			t.Errorf("the Back after replaceURL animates as %v, want back", ty)
		}

		// Refused while a guarded traversal is unresolved.
		ngEval(t, ctx, `history.forward(); true`, nil)
		nhOn(t, ctx, "/b")
		if noAPI {
			// The browser waits on a landing no router wrote.
			ngEval(t, ctx, `history.pushState(null, '', '/c'); true`, nil)
			ngGo(t, ctx, "/d")
		} else {
			// The engine's own traversal never commits.
			ngEval(t, ctx, `navigation.traverseTo = () => ({ committed: new Promise(() => {}), finished: new Promise(() => {}) }); true`, nil)
		}
		ngMode(t, ctx, "hold")
		ngEval(t, ctx, `history.back(); true`, nil)
		ngWait(t, ctx, `!!__ng.held[0]`, "the held Back")
		var during string
		ngEval(t, ctx, `[__gofastr.replaceURL('?while=1'), location.search].join()`, &during)
		if during != "false," {
			t.Errorf("replaceURL during an unresolved traversal = %s, want false and nothing written", during)
		}
	})
}

// nhRefusedWrite runs call while history.replaceState throws as a
// browser's rate limit makes it.
func nhRefusedWrite(call string) string {
	return `(() => { const f = history.replaceState; history.replaceState = () => { throw new DOMException('limit', 'SecurityError'); }; try { return ` + call + `; } finally { history.replaceState = f; } })()`
}

// nhReplaceInLayer: replaceURL inside an open record layer rewrites the
// layer's own entry. The layer, its draft, the entry's identity and the
// state other code put on it stay; the router's current path stays on
// the page under the stack; Back then closes the layer as from any of
// its entries.
func nhReplaceInLayer(t *testing.T, noAPI bool) []string {
	var c nhCheck
	ctx := nhStack(t, noAPI, "allow")
	ngEval(t, ctx, `history.replaceState({ ...history.state, mine: 7 }, ''); true`, nil)
	before := ngReadLayer(t, ctx)
	state0 := ngJSON[map[string]any](t, ctx, `history.state`)
	h0 := nhHist(t, ctx)
	var ok bool
	ngEval(t, ctx, `__gofastr.replaceURL('/rec/a?tab=2')`, &ok)
	time.Sleep(200 * time.Millisecond)
	l := ngReadLayer(t, ctx)
	state1 := ngJSON[map[string]any](t, ctx, `history.state`)
	var cp string
	ngEval(t, ctx, `__gofastr.currentPath`, &cp)
	c.want(ok && l.Layers == 1 && l.URL == "/rec/a?tab=2" && l.Note != nil && *l.Note == "draft" && l.Key == before.Key && cp == "/list",
		"replaceURL in a layer: ok %v, %+v (was %+v), currentPath %q", ok, l, before, cp)
	c.want(fmt.Sprint(state1) == fmt.Sprint(state0), "the layer entry's state %v, was %v", state1, state0)
	c.want(nhHist(t, ctx) == strings.Replace(h0, "/rec/a@", "/rec/a?tab=2@", 1), "history %s -> %s", h0, nhHist(t, ctx))
	var refused bool
	ngEval(t, ctx, nhRefusedWrite(`__gofastr.replaceURL('/rec/a?tab=3')`), &refused)
	c.want(!refused && ngReadLayer(t, ctx).URL == "/rec/a?tab=2", "a write the browser refused in a layer: %v at %s", refused, ngReadLayer(t, ctx).URL)
	ngEval(t, ctx, `history.back(); true`, nil)
	c.want(nhUntil(ctx, `!document.getElementById('cui-intercept')?.children.length && location.pathname === '/list'`, 4*time.Second), "Back did not close the layer")
	return c.bad
}

func TestNavGuardHistoryReplaceURLInLayer(t *testing.T) {
	nhModes(t, func(t *testing.T, noAPI bool) { nhReport(t, nhReplaceInLayer(t, noAPI)) })
}

// --- Mutations ---

// nhMutate serves the runtime with one exact edit for the rest of the
// test: to a module's source file (served as written), or for module
// "" to the composed core bundle.
func nhMutate(t *testing.T, module, from, to string) {
	t.Helper()
	src, ok := ngSource("")
	if module != "" {
		b, err := os.ReadFile("src/" + module + ".js")
		src, ok = string(b), err == nil
	}
	if !ok || strings.Count(src, from) != 1 {
		t.Fatalf("mutation target %q not found once in %q", from, module)
	}
	orig := ngSource
	ngSource = func(name string) (string, bool) {
		if name == module {
			return strings.Replace(src, from, to, 1), true
		}
		return orig(name)
	}
	t.Cleanup(func() { ngSource = orig })
}

// TestNavGuardHistoryMutationsAreCaught: each check the contract rests
// on, taken out alone, breaks a scenario above.
func TestNavGuardHistoryMutationsAreCaught(t *testing.T) {
	cases := []struct {
		name, module, from, to string
		run                    func(t *testing.T) []string
	}{
		{"no undo", "navguard", "if (d) travel(m.src, () => lost(m));\n    else lost(m);", "lost(m);",
			func(t *testing.T) []string { return nhDeniedBack(t, true) }},
		{"tags without a position", "", "p[1]+step", "p[1]",
			func(t *testing.T) []string { return nhDeniedBack(t, true) }},
		{"any landing is the undo's", "navguard", "if (e.key !== heading.key) {", "if (false) {",
			func(t *testing.T) []string { return nhTwoBacks(t, true) }},
		{"shown follows a landing mid-undo", "navguard", "if (!heading) shown = e;", "shown = e;", nhM8b},
		{"a then getter outside the try", "navguard",
			"try { v = g.fn(r.a); p = !!v && typeof v.then === 'function'; } catch (err) { r.err = err; return false; }",
			"try { v = g.fn(r.a); } catch (err) { r.err = err; return false; } p = !!v && typeof v.then === 'function';",
			func(t *testing.T) []string { return nhThenGetter(t, true, `document.getElementById('toB').click()`) }},
		{"a superseded decision is not aborted", "navguard", "if (r) r.c.abort(why);", "",
			func(t *testing.T) []string { return nhSuperseded(t, true) }},
		{"a superseded decision still answers", "navguard", "v.then((ok) => { if (live === r) { live = null; done(ok); } });", "v.then((ok) => { live = null; done(ok); });",
			func(t *testing.T) []string { return nhSuperseded(t, true) }},
		{"an unknown refusal pushes", "navguard", "try { history.replaceState({ ...st, ...NS._entryTag(0) }, '', m.src.url); } catch (_) {}", "history.pushState({ ...st, ...NS._entryTag(1) }, '', m.src.url);", nhForeign},
		{"a layer's replace drops the entry's state", "intercept", "{ ...replace && history.state, ...NS._entryTag(+!replace),", "{ ...NS._entryTag(+!replace),",
			func(t *testing.T) []string { return nhReplaceInLayer(t, true) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nhMutate(t, tc.module, tc.from, tc.to)
			bad := tc.run(t)
			if len(bad) == 0 {
				t.Fatalf("mutation %q survived", tc.name)
			}
			t.Logf("caught: %s", bad[0])
		})
	}
}
