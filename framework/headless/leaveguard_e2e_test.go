package headless

// Leave guard e2e (admin rebuild P0, worker C): a form marked
// data-hui-leave-guard becomes dirty on input/change and clean again
// after a successful submit or a reset. While dirty, link navigation
// (through gofastr:beforenavigate), closing an intercept layer (Esc,
// Back), a reload (beforeunload) and the browser's own Back all ask
// first. The ask is window.confirm — the same mechanism data-cui-confirm
// uses — because gofastr:beforenavigate is a synchronous, cancellable
// event: an async dialog cannot answer it in time, and the runtime has
// no synchronous dialog of its own.

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// leaveGuardStub installs a counting window.confirm whose return value
// is window.__confirmRet, and returns the asked messages.
const leaveGuardStub = `window.__confirms = window.__confirms || [];
window.confirm = function (msg) { window.__confirms.push(String(msg)); return !!window.__confirmRet; };`

// leaveGuardRoutes lets the router intercept the away link, so
// gofastr:beforenavigate fires for it; /rec/:id declares an intercept
// for the layer tests.
const leaveGuardRoutes = `[{"path":"/"},{"path":"/away"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}}]`

func leaveGuardBody(form string) string {
	return `<script type="application/json" id="gofastr-routes">` + leaveGuardRoutes + `</script>` +
		form + `<a id="go" href="/away">away</a>`
}

// TestLeaveGuardDirtyLinkAsksAndStays: typing into a guarded form arms
// the guard; the away link asks and, declined, the URL stays; a reset
// cleans the form and the same link navigates with no further ask.
func TestLeaveGuardDirtyLinkAsksAndStays(t *testing.T) {
	body := leaveGuardBody(`<form id="gf" data-hui-leave-guard data-hui-leave-guard-message="Custom guard words">
		<input id="gf-name" name="name"><input id="gf-reset" type="reset"></form>`)
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	var path string
	var confirms string
	var lenAfterReset int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`JSON.stringify(window.__confirms)`, &confirms),
		chromedp.Click(`#gf-reset`, chromedp.ByID),
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = true;`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__confirms.length`, &lenAfterReset),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if path != "/away" {
		t.Errorf("declined guard must keep the URL, reset + accepted must navigate; at %q", path)
	}
	if confirms != `["Custom guard words"]` {
		t.Errorf("confirm messages = %s, want exactly the form's own message once", confirms)
	}
	if lenAfterReset != 1 {
		t.Errorf("a clean form must navigate with no new ask (1 earlier ask), got %d", lenAfterReset)
	}
}

// TestLeaveGuardSubmitClearsFailedRPCRedirties: a successful RPC submit
// cleans the form (the save IS the change), a refused one puts it back
// dirty — the 422 means the words never reached the server.
func TestLeaveGuardSubmitClearsFailedRPCRedirties(t *testing.T) {
	body := `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/away"}]</script>` +
		`<form id="gf" data-hui-leave-guard data-cui-rpc="/__hui/ok" data-cui-rpc-method="POST">` +
		`<input id="gf-name" name="name"><button id="gf-save" type="submit">Save</button></form>` +
		`<form id="bf" data-hui-leave-guard data-cui-rpc="/__hui/fail" data-cui-rpc-method="POST">` +
		`<input id="bf-name" name="name"><button id="bf-save" type="submit">Save</button></form>` +
		`<a id="go" href="/away">away</a>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	var asks int
	var path string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		// The refused save: still dirty afterwards, the away link asks.
		chromedp.SetValue(`#bf-name`, "typed", chromedp.ByID),
		chromedp.Click(`#bf-save`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
		// The accepted save: clean, the away link goes without asking.
		// (bf stays dirty from its refusal, so it is reset out of the
		// way first — its ask is phase 1's assertion, not phase 2's.)
		chromedp.Evaluate(`document.getElementById('bf').reset()`, nil),
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Click(`#gf-save`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if asks != 1 {
		t.Errorf("after a refused 422 save the form must still be dirty (1 ask), got %d", asks)
	}
	if path != "/away" {
		t.Errorf("a successful save must clean the form and let the link navigate, at %q", path)
	}
}

// TestLeaveGuardBeforeunloadFollowsDirty: while any guarded form is
// dirty, a beforeunload is cancelled (the browser's own reload prompt);
// clean, it is not.
func TestLeaveGuardBeforeunloadFollowsDirty(t *testing.T) {
	body := leaveGuardBody(`<form id="gf" data-hui-leave-guard><input id="gf-name" name="name"></form>`)
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	var cleanPrevented, dirtyPrevented, recleanedPrevented string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){var e=new Event('beforeunload',{cancelable:true});window.dispatchEvent(e);return String(e.defaultPrevented);})()`, &cleanPrevented),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Evaluate(`(function(){var e=new Event('beforeunload',{cancelable:true});window.dispatchEvent(e);return String(e.defaultPrevented);})()`, &dirtyPrevented),
		chromedp.Evaluate(`document.getElementById('gf').reset()`, nil),
		chromedp.Evaluate(`(function(){var e=new Event('beforeunload',{cancelable:true});window.dispatchEvent(e);return String(e.defaultPrevented);})()`, &recleanedPrevented),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if cleanPrevented != "false" {
		t.Error("a clean page must not cancel beforeunload")
	}
	if dirtyPrevented != "true" {
		t.Error("a dirty guarded form must cancel beforeunload (the browser reload prompt)")
	}
	if recleanedPrevented != "false" {
		t.Error("a reset form must release beforeunload")
	}
}

// leaveGuardLayerServer adds the intercepted /rec/1 overlay to the
// behavior server: the overlay carries a guarded form, so closing the
// layer with edits in flight has to ask first.
func leaveGuardLayerServer(t *testing.T) *behaviorServer {
	t.Helper()
	body := `<script type="application/json" id="gofastr-routes">` + leaveGuardRoutes + `</script>` +
		`<a id="open" href="/rec/1">open</a>`
	b := startBehaviorServer(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/rec/1", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Intercept") == "" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<!doctype html><html><head><title>rec</title></head><body><main>plain record</main></body></html>`)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			fmt.Fprint(w, `<div id="rec-layer"><form id="gf" data-hui-leave-guard>`+
				`<input id="gf-name" name="name"></form>`+
				`<button id="layer-close" type="button" data-cui-intercept-close>Close</button></div>`)
		})
	})
	return b
}

// TestLeaveGuardInterceptCloseAsksFirst: Esc and the close control ask
// while the layer's form is dirty; declined, the layer stays; accepted,
// it closes through the same history path as ever.
func TestLeaveGuardInterceptCloseAsksFirst(t *testing.T) {
	b := leaveGuardLayerServer(t)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	var url string
	var asks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		// Esc, declined: the layer stays.
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`, &url),
		// The close control, accepted: the layer closes.
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = true;`, nil),
		chromedp.Click(`#layer-close`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`, &url),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if url != "/:closed" {
		t.Errorf("declined Esc must keep the layer open at /rec/1, accepted close must land on / closed; at %q", url)
	}
	if asks != 2 {
		t.Errorf("want one ask for Esc and one for the close control, got %d", asks)
	}
}

// TestLeaveGuardBackDeclineRepairsHistory: Back cannot be cancelled, so
// a declined guard re-pushes the layer's entry — the URL, the layer and
// history.length all come back to where they were.
func TestLeaveGuardBackDeclineRepairsHistory(t *testing.T) {
	b := leaveGuardLayerServer(t)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	var url string
	var lenBefore, lenAfter int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Evaluate(`history.length`, &lenBefore),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`, &url),
		chromedp.Evaluate(`history.length`, &lenAfter),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if url != "/rec/1:open" {
		t.Errorf("a declined Back must leave the layer open at /rec/1, at %q", url)
	}
	if lenAfter != lenBefore {
		t.Errorf("a declined Back must restore history.length (before %d, after %d)", lenBefore, lenAfter)
	}
}

// TestLeaveGuardQueryChangeInPaneAsks: a pane query change replaces the
// pane's content, so a dirty guarded form in it asks before the pane
// moves on — the click path guards through gofastr:beforenavigate.
func TestLeaveGuardQueryChangeInPaneAsks(t *testing.T) {
	body := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}}]</script>` +
		`<a id="open" href="/rec/1">open</a>`
	b := startBehaviorServer(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/rec/1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Intercept") != "" {
				w.Header().Set("X-Gofastr-Overlay", "drawer")
			}
			if r.URL.RawQuery == "sort=name" {
				fmt.Fprint(w, `<div id="rec-sorted">sorted</div>`)
				return
			}
			fmt.Fprint(w, `<div id="rec-layer"><form id="gf" data-hui-leave-guard>`+
				`<input id="gf-name" name="name"></form>`+
				`<a id="sort" href="/rec/1?sort=name">sort</a></div>`)
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	var content string
	var asks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Click(`#sort`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(document.getElementById('rec-sorted') ? 'sorted' : 'kept')`, &content),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if asks != 1 {
		t.Errorf("a dirty pane's query change must ask once, got %d", asks)
	}
	if content != "kept" {
		t.Errorf("a declined query change must keep the pane's content, got %q", content)
	}
}

// leaveGuardStackServer serves /rec/1 as a layer holding a guarded form,
// a link to the related /rel/1 (an intercept from /rec/:id, so it stacks
// over the record) and a query link on the record's own URL.
func leaveGuardStackServer(t *testing.T, page string) *behaviorServer {
	t.Helper()
	body := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/"},{"path":"/rec/:id","intercept":{"from":"/","as":"drawer"}},` +
		`{"path":"/rel/:id","intercept":{"from":"/rec/:id","as":"drawer"}}]</script>` +
		page + `<a id="open" href="/rec/1">open</a>`
	return startBehaviorServer(t, body, func(mux *http.ServeMux) {
		layer := func(html string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if r.Header.Get("X-Gofastr-Intercept") != "" {
					w.Header().Set("X-Gofastr-Overlay", "drawer")
				}
				fmt.Fprint(w, html)
			}
		}
		mux.HandleFunc("/rec/1", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery == "tab=history" {
				layer(`<div id="rec-history">history</div>`)(w, r)
				return
			}
			layer(`<div id="rec-layer"><form id="gf" data-hui-leave-guard>`+
				`<input id="gf-name" name="name"></form>`+
				`<a id="rel" href="/rel/1">related</a>`+
				`<a id="tab" href="/rec/1?tab=history">history</a></div>`)(w, r)
		})
		mux.HandleFunc("/rel/1", layer(`<div id="rel-layer">related</div>`))
	})
}

// TestLeaveGuardStackOpenKeepsEdits: opening a related record over a
// dirty one discards nothing (the lower layer keeps its DOM), so it
// opens without asking and the edit is still there underneath.
func TestLeaveGuardStackOpenKeepsEdits(t *testing.T) {
	b := leaveGuardStackServer(t, "")
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
	); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('gf-name')`) {
		t.Fatal("the record layer never mounted")
	}
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#gf-name`, "edited", chromedp.ByID),
		chromedp.Click(`#rel`, chromedp.ByID),
	); err != nil {
		t.Fatalf("stack: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('rel-layer')`) {
		t.Fatal("the related layer never mounted over the dirty record")
	}
	var asks int
	var note string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__confirms.length`, &asks),
		chromedp.Evaluate(`document.getElementById('gf-name').value`, &note),
	); err != nil {
		t.Fatalf("read: %v", err)
	}
	if asks != 0 {
		t.Errorf("a stacked open discards nothing and must not ask, got %d asks", asks)
	}
	if note != "edited" {
		t.Errorf("the record under the stack lost its edit: %q", note)
	}
}

// TestLeaveGuardPaneQueryIgnoresPageDirt: a query move re-renders only
// the top pane, so dirt on the page under the stack does not ask.
func TestLeaveGuardPaneQueryIgnoresPageDirt(t *testing.T) {
	b := leaveGuardStackServer(t, `<form id="pf" data-hui-leave-guard><input id="pf-name" name="name"></form>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.SetValue(`#pf-name`, "edited", chromedp.ByID),
		chromedp.Click(`#open`, chromedp.ByID),
	); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('tab')`) {
		t.Fatal("the record layer never mounted over the dirty page")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#tab`, chromedp.ByID)); err != nil {
		t.Fatalf("tab: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('rec-history')`) {
		t.Fatal("the clean pane's query move never re-rendered it")
	}
	var asks int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__confirms.length`, &asks)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if asks != 0 {
		t.Errorf("dirt on the page under the stack must not ask for a pane query move, got %d asks", asks)
	}
}

// TestLeaveGuardPageBackAsks: Back from a page whose guarded form is
// dirty asks first; declined, the URL and the edit stay, and the page
// is not reloaded.
func TestLeaveGuardPageBackAsks(t *testing.T) {
	body := leaveGuardBody(`<form id="gf" data-hui-leave-guard><input id="gf-name" name="name"></form>`)
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!window.__gofastr.loadedModules['headless-leaveguard']`) {
		t.Fatal("the guard marker never loaded headless-leaveguard")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__navs = 0; window.addEventListener("gofastr:navigate", function () { window.__navs++; })`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatalf("go: %v", err)
	}
	if !pollTrue(ctx, `window.__navs > 0 && location.pathname === '/away'`) {
		t.Fatal("the clean page never navigated to /away")
	}
	var path, note string
	var asks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.SetValue(`#gf-name`, "typed", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('gf-name').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`document.getElementById('gf-name').value`, &note),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if asks != 1 {
		t.Errorf("Back from a dirty page must ask once, got %d", asks)
	}
	if path != "/away" || note != "typed" {
		t.Errorf("a declined Back must keep the page and its edit: at %q, note %q", path, note)
	}
}

// TestLeaveGuardStackBackPastPageAsks: a history move from an open
// stack to a page other than the one under it replaces that page too,
// so dirt on the page under the stack asks; declined, the stack stays.
func TestLeaveGuardStackBackPastPageAsks(t *testing.T) {
	body := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/"},{"path":"/away"},{"path":"/rec/:id","intercept":{"from":"/away","as":"drawer"}}]</script>` +
		`<form id="pf" data-hui-leave-guard><input id="pf-name" name="name"></form>` +
		`<a id="go" href="/away">away</a><a id="open" href="/rec/1">open</a>`
	b := startBehaviorServer(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/rec/1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Intercept") != "" {
				w.Header().Set("X-Gofastr-Overlay", "drawer")
			}
			fmt.Fprint(w, `<div id="rec-layer">record</div>`)
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules.intercept)`) {
		t.Fatal("the intercept route never loaded the intercept module")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__navs = 0; window.addEventListener("gofastr:navigate", function () { window.__navs++; })`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatalf("go: %v", err)
	}
	if !pollTrue(ctx, `window.__navs > 0 && location.pathname === '/away'`) {
		t.Fatal("the clean page never navigated to /away")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(leaveGuardStub+`window.__confirmRet = false;`, nil),
		chromedp.SetValue(`#pf-name`, "typed", chromedp.ByID),
		chromedp.Click(`#open`, chromedp.ByID),
	); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !pollTrue(ctx, `!!document.getElementById('rec-layer')`) {
		t.Fatal("the record layer never mounted")
	}
	var url string
	var asks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`history.go(-2)`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname + ':' + (document.getElementById('cui-intercept') ? 'open' : 'closed')`, &url),
		chromedp.Evaluate(`window.__confirms.length`, &asks),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if asks != 1 {
		t.Errorf("leaving the page under a stack with a dirty form must ask once, got %d", asks)
	}
	if url != "/rec/1:open" {
		t.Errorf("a declined move must keep the stack, at %q", url)
	}
}
