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

	"github.com/chromedp/chromedp"
)

// handleRuntimeModules exposes the same split-module route production uses.
// Browser tests that render marker attributes must call it; serving only
// runtime.js would turn a demand-loaded interaction into a false no-op.
func handleRuntimeModules(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
}

// startPollServer wires a chromedp-facing test server with the runtime
// bundle at /__gofastr/runtime.js, every embedded split module at
// /__gofastr/runtime/<name>.js (so the module loader path actually
// fires), the page at "/", plus any test-specific endpoints passed in
// extra. Tests need the REAL module-loader script-inject path to run,
// which means modules must be reachable at exactly the loader-built URLs.
func startPollServer(t *testing.T, pageHTML string, extra map[string]http.HandlerFunc) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	for path, h := range extra {
		mux.HandleFunc(path, h)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Fallthrough only when no extra handler matched "/".
		if extra != nil {
			if _, ok := extra["/"]; ok {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, pageHTML)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newPollBrowserCtx is the chromedp allocator the poll tests use. The
// 5s clamp floor makes the data-fui-poll tests inherently slow, so the
// per-test timeout is 60s (vs the 30s seed tests use).
func newPollBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1024, 768),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run, so one timeout would
	// have to cover BOTH the cold start and the page work — and it caps the
	// WSURLReadTimeout above, making that 90s ineffective. Start the browser
	// under its own budget first; the returned context then only has to cover
	// the assertions.
	startCtx, startCancel := context.WithTimeout(browserCtx, 90*time.Second)
	t.Cleanup(startCancel)
	if err := chromedp.Run(startCtx); err != nil {
		t.Fatalf("chrome did not start within 90s: %v", err)
	}

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestPoll_SwapsRegion proves the load-bearing behavior: a
// data-fui-poll element fetches data-fui-poll-src on the (clamped)
// cadence and the response HTML replaces the element's innerHTML.
// The clamp forces a ~5s minimum wait; the test tolerates that rather
// than weaken the production rule.
func TestPoll_SwapsRegion(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	page := `<!doctype html><html><head></head><body>
<div id="region" data-fui-poll="5s" data-fui-poll-src="/fresh">stale</div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits++
			n := hits
			mu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<span id="hit-%d">fresh-%d</span>`, n, n)
		},
	})

	ctx := newPollBrowserCtx(t)
	var content string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		// First poll tick lands at ~4.5s earliest (5s clamp − 10% jitter).
		chromedp.Poll(`/fresh-/.test(document.getElementById('region').textContent)`, nil,
			chromedp.WithPollingInterval(200*time.Millisecond)),
		chromedp.Text(`#region`, &content, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	got := hits
	mu.Unlock()
	if got < 1 {
		t.Errorf("poll src not fetched; hits=%d", got)
	}
	if !strings.Contains(content, "fresh-") {
		t.Errorf("region innerHTML not swapped; got %q", content)
	}
}

// TestPoll_ClampsIntervalToFiveSeconds locks the clamp + parser via the
// module's exposed _pollClampedMs test hook. Fast (no browser wait for
// the actual 5s timer) but exercises the real parse + clamp code paths.
func TestPoll_ClampsIntervalToFiveSeconds(t *testing.T) {
	page := `<!doctype html><html><head></head><body>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, nil)
	ctx := newPollBrowserCtx(t)

	var underFloor, atFloor, aboveFloor, raised float64
	var bad string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadModule`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		// Explicitly load the poll module — no [data-fui-poll] marker
		// on this page means the scanner wouldn't trigger it. Same
		// loadModule path _scanForModules takes internally.
		chromedp.Evaluate(`window.__gofastr.loadModule('poll')`, nil),
		chromedp.Poll(`window.__gofastr._pollClampedMs`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(`window.__gofastr._pollClampedMs("1s")`, &underFloor),
		chromedp.Evaluate(`window.__gofastr._pollClampedMs("5s")`, &atFloor),
		chromedp.Evaluate(`window.__gofastr._pollClampedMs("30s")`, &aboveFloor),
		chromedp.Evaluate(`window.__gofastr._pollClampedMs("500ms")`, &raised),
		// NaN serializes as JSON null, which chromedp can't decode
		// into a float64 — wrap in String() so we capture "NaN".
		chromedp.Evaluate(`String(window.__gofastr._pollClampedMs("garbage"))`, &bad),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if underFloor != 5000 {
		t.Errorf(`clamp("1s") = %v, want 5000`, underFloor)
	}
	if atFloor != 5000 {
		t.Errorf(`clamp("5s") = %v, want 5000 (at floor)`, atFloor)
	}
	if aboveFloor != 30000 {
		t.Errorf(`clamp("30s") = %v, want 30000 (above floor, unchanged)`, aboveFloor)
	}
	if raised != 5000 {
		t.Errorf(`clamp("500ms") = %v, want 5000 (raised to floor)`, raised)
	}
	if bad != "NaN" {
		t.Errorf(`clamp("garbage") = %q, want "NaN"`, bad)
	}
}

// TestPoll_TeardownOnRemoval proves timers don't leak: after the polled
// element leaves the DOM, the source endpoint stops getting hit. Uses
// the 5s clamp floor so the assertion window is one tick.
func TestPoll_TeardownOnRemoval(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	page := `<!doctype html><html><head></head><body>
<div id="host"><div id="region" data-fui-poll="5s" data-fui-poll-src="/fresh">stale</div></div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits++
			n := hits
			mu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<span id="hit">fresh-%d</span>`, n)
		},
	})

	ctx := newPollBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		// Wait for first tick (clamp ~5s) — proves the poll armed.
		chromedp.Poll(`/fresh-/.test(document.getElementById('region').textContent)`, nil,
			chromedp.WithPollingInterval(200*time.Millisecond)),
		// Remove the polled element. The next tick must self-teardown
		// via the isConnected check — no further /fresh hit.
		chromedp.Evaluate(`document.getElementById('host').innerHTML = ''`, nil),
		// Sleep one full clamped interval. If the timer leaked,
		// /fresh would be hit again in this window.
		chromedp.Sleep(6*time.Second),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	finalHits := hits
	mu.Unlock()
	// Allow exactly 1 hit (the initial tick before removal). Any more
	// means the timer survived the element leaving the DOM.
	if finalHits > 1 {
		t.Errorf("poll src hit after element removal: hits=%d (want ≤1) — timer leaked", finalHits)
	}
}

// TestWidgetPoll_OverwritesSignalsAndStopsOnDismiss drives the widget
// polling path end-to-end: a widget catalog entry with pollMs + a
// state endpoint whose value increments on each GET. mountWidget polls
// statePath, OVERWRITES the signal with the fresh value (unlike mount
// hydration, which skips already-set signals), and STOPS polling once
// dismiss() runs pollStop().
func TestWidgetPoll_OverwritesSignalsAndStopsOnDismiss(t *testing.T) {
	var mu sync.Mutex
	stateHits := 0
	// 1s pollMs — the widget poll path doesn't apply the 5s clamp
	// (only data-fui-poll does), so the test observes multiple ticks
	// well within the chromedp timeout.
	const pollMs = 1000

	page := `<!doctype html><html><head></head><body>
<div class="fui-widget fui-pos-bottom-right" data-fui-widget="poller" role="status"><span data-fui-signal="count">0</span></div>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	widgetsJS, _ := Module("widgets")
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode([]map[string]any{
				{
					"hidden": false,
					"cfg": map[string]any{
						"name":          "poller",
						"position":      "bottom-right",
						"backdrop":      false,
						"closeOnEscape": false,
						"closeOnClick":  false,
						"stylePath":     "/core-ui/widget/poller/style.css",
						"chromePath":    "/core-ui/widget/poller/chrome",
						"statePath":     "/core-ui/widget/poller/state",
						"pollMs":        pollMs,
					},
				},
			})
		},
		"/core-ui/widget/poller/state": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			stateHits++
			n := stateHits
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"count":%d}`, n)
		},
		"/core-ui/widget/poller/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="poller" role="status"><span data-fui-signal="count">0</span></div>`)
		},
		"/core-ui/widget/poller/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		// Override the default /__gofastr/runtime/widgets.js handler so
		// the page gets the REAL widgets module bytes (startPollServer
		// serves all modules generically; this just confirms it's the
		// same path the loader requests).
		"/__gofastr/runtime/widgets.js": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(widgetsJS))
		},
	})

	ctx := newPollBrowserCtx(t)

	// Phase 1: let the widget mount + poll for 3 seconds (3 poll ticks
	// at 1s cadence, plus the mount-hydration fetch). Then read the
	// signal value + hit count. We use Sleep + Evaluate instead of
	// chromedp.Poll because Poll's internal default timeout races
	// with the 1s poll cadence — a Sleep is deterministic.
	var snapshot string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		// Confirm the widgets module actually loaded + mounted.
		chromedp.Poll(`window.__gofastr?._widgets?.["poller"]`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate(`JSON.stringify({
			val: document.querySelector('[data-fui-signal="count"]')?.textContent,
			sig: window.__gofastr?._signals?.["count"]?.value,
			hasPollStop: typeof window.__gofastr?._widgets?.["poller"]?.pollStop === 'function',
		})`, &snapshot),
	); err != nil {
		t.Fatalf("chromedp phase 1: %v", err)
	}
	mu.Lock()
	hitsBeforeDismiss := stateHits
	mu.Unlock()

	// Phase 2: dismiss + wait for leaked ticks.
	var postDismiss string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.closeWidget('poller')`, nil),
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify({
			widgetGone: window.__gofastr?._widgets?.["poller"] === undefined,
		})`, &postDismiss),
	); err != nil {
		t.Fatalf("chromedp phase 2: %v", err)
	}
	mu.Lock()
	hitsAfterDismiss := stateHits
	mu.Unlock()

	// Assert: polling overwrote the signal (count advanced past 0).
	// The exact value depends on timing + jitter, but ≥2 after 3s
	// (1s cadence) proves the poll fired AND overwrote (mount
	// hydration alone would have left it at 1).
	t.Logf("snapshot=%s hitsBefore=%d hitsAfter=%d postDismiss=%s", snapshot, hitsBeforeDismiss, hitsAfterDismiss, postDismiss)

	var state struct {
		Val         string `json:"val"`
		Sig         int    `json:"sig"`
		HasPollStop bool   `json:"hasPollStop"`
	}
	if err := json.Unmarshal([]byte(snapshot), &state); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if !state.HasPollStop {
		t.Errorf("pollStop not set on widget entry — polling block did not arm")
	}
	if state.Sig < 2 {
		t.Errorf("count signal = %d after 3s, want ≥2 (poll did not overwrite beyond hydration)", state.Sig)
	}
	// After dismiss + 2.5s (enough for ≥2 ticks at 1s cadence if the
	// timer leaked), hits should not have grown significantly.
	delta := hitsAfterDismiss - hitsBeforeDismiss
	if delta > 2 {
		t.Errorf("state endpoint hit %d times after dismiss (want ≤2) — pollStop did not clear timer", delta)
	}
}

// TestWidgetPollNow_RefreshesAfterRPC pins the mutation→authoritative-
// refresh contract added in #112: after a successful data-fui-rpc, the
// widget re-fetches /state immediately (dispatchRPC → entry.pollNow)
// instead of waiting out the cadence. The 60s pollMs makes the test
// deterministic — no scheduled tick can fire within the test window, so
// a second /state hit after the click can ONLY be pollNow. Deleting the
// pollNow call (or breaking its wiring through the demand-loaded poll
// module) fails this test; the kiln integration suite's 5s windows are
// deliberately cadence-tolerant and cannot catch that regression.
func TestWidgetPollNow_RefreshesAfterRPC(t *testing.T) {
	var mu sync.Mutex
	stateHits := 0

	page := `<!doctype html><html><head></head><body>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode([]map[string]any{
				{
					"hidden": false,
					"cfg": map[string]any{
						"name":          "bumper",
						"position":      "bottom-right",
						"backdrop":      false,
						"closeOnEscape": false,
						"closeOnClick":  false,
						"stylePath":     "/core-ui/widget/bumper/style.css",
						"chromePath":    "/core-ui/widget/bumper/chrome",
						"statePath":     "/core-ui/widget/bumper/state",
						"pollMs":        60000,
					},
				},
			})
		},
		"/core-ui/widget/bumper/state": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			stateHits++
			n := stateHits
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"count":%d}`, n)
		},
		"/core-ui/widget/bumper/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="bumper" role="status"><span data-fui-signal="count">0</span><button data-fui-rpc="/rpc/bump">Bump</button></div>`)
		},
		"/core-ui/widget/bumper/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		"/rpc/bump": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		},
	})

	ctx := newPollBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		// Widget mounted AND the demand-loaded poll module has armed
		// pollNow on its entry (loadModule('poll') is async).
		chromedp.Poll(`typeof window.__gofastr?._widgets?.["bumper"]?.pollNow === 'function'`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("mount: %v", err)
	}
	mu.Lock()
	hitsBefore := stateHits // mount hydration
	mu.Unlock()

	var sig int
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-fui-rpc="/rpc/bump"]`, chromedp.ByQuery),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`Number(window.__gofastr?._signals?.["count"]?.value ?? 0)`, &sig),
	); err != nil {
		t.Fatalf("rpc click: %v", err)
	}
	mu.Lock()
	hitsAfter := stateHits
	mu.Unlock()

	if hitsAfter <= hitsBefore {
		t.Errorf("no /state re-fetch after RPC (hits %d → %d) — pollNow path broken", hitsBefore, hitsAfter)
	}
	if sig < hitsAfter {
		t.Errorf("count signal = %d, want %d — pollNow response not applied to signals", sig, hitsAfter)
	}
}

// TestWidgetPollLargeIntervalNoOverflow pins the round-2 fix for the
// `| 0` 32-bit overflow: a legitimate long interval (Poll(30 days) →
// pollMs ~2.59e9) must NOT wrap negative and collapse to the 100ms
// floor — which would turn a monthly poll into a ~10 req/s hammer.
// With Math.trunc the first re-fetch is scheduled ~a month out, so no
// /state hit lands within the test window beyond the mount hydration.
func TestWidgetPollLargeIntervalNoOverflow(t *testing.T) {
	var mu sync.Mutex
	stateHits := 0
	const bigMs = 30 * 24 * 60 * 60 * 1000 // 30 days; > math.MaxInt32

	page := `<!doctype html><html><head></head><body><script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode([]map[string]any{{
				"hidden": false,
				"cfg": map[string]any{
					"name": "slow", "position": "bottom-right", "backdrop": false,
					"closeOnEscape": false, "closeOnClick": false,
					"stylePath":  "/core-ui/widget/slow/style.css",
					"chromePath": "/core-ui/widget/slow/chrome",
					"statePath":  "/core-ui/widget/slow/state", "pollMs": bigMs,
				},
			}})
		},
		"/core-ui/widget/slow/state": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			stateHits++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":1}`)
		},
		"/core-ui/widget/slow/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="slow" role="status"><span data-fui-signal="count">0</span></div>`)
		},
		"/core-ui/widget/slow/style.css": func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/css") },
	})

	ctx := newPollBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`typeof window.__gofastr?._widgets?.["slow"]?.pollStop === 'function'`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Sleep(2500*time.Millisecond), // ~1250 ticks if it collapsed to 100ms
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	hits := stateHits
	mu.Unlock()
	// Mount hydration fetches /state once; the scheduled poll is a month
	// out. If `| 0` overflowed to a 100ms cadence, we'd see dozens.
	if hits > 2 {
		t.Errorf("/state hit %d times in 2.5s for a 30-day interval — large pollMs collapsed to the 100ms floor (overflow)", hits)
	}
}

// TestPoll_StopsOnTerminalHeader pins issue #192 on the page-level path:
// a poll-src handler ends the poll by setting X-Gofastr-Poll-Stop on its
// response. The terminal body is applied (innerHTML swap) and the timer
// is torn down — the region reaches terminal state and /fresh is never
// hit again. Without honoring the header the poll runs forever (#192).
func TestPoll_StopsOnTerminalHeader(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	page := `<!doctype html><html><head></head><body>
<div id="region" data-fui-poll="5s" data-fui-poll-src="/fresh">stale</div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits++
			n := hits
			mu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// The 2nd tick is terminal: emit the stop header AND the
			// final body. The runtime must apply the body and clear
			// the timer so no 3rd tick fires.
			if n >= 2 {
				w.Header().Set("X-Gofastr-Poll-Stop", "1")
				fmt.Fprintf(w, `<span id="done">completed-%d</span>`, n)
				return
			}
			fmt.Fprintf(w, `<span id="hit">fresh-%d</span>`, n)
		},
	})

	ctx := newPollBrowserCtx(t)
	var finalText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		// Wait for the terminal body to land (tick 2, ~10s at the 5s
		// clamp) — proves the poll armed, ticked at least once, and
		// applied the terminal response body.
		chromedp.Poll(`/completed-/.test(document.getElementById('region').textContent)`, nil,
			chromedp.WithPollingInterval(200*time.Millisecond)),
		// Sleep one full clamped interval. A leaked timer would fire
		// /fresh a 3rd time in this window.
		chromedp.Sleep(6*time.Second),
		chromedp.Evaluate(`document.getElementById('region').textContent`, &finalText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	finalHits := hits
	mu.Unlock()

	if !strings.Contains(finalText, "completed") {
		t.Errorf("terminal body not applied: region=%q", finalText)
	}
	// Exactly 2 hits: tick 1 (non-terminal) + tick 2 (terminal). A 3rd
	// means the timer survived the stop header.
	if finalHits != 2 {
		t.Errorf("/fresh hits = %d, want 2 — poll did not stop on terminal header", finalHits)
	}
}

// TestWidgetPoll_StopsOnTerminalHeader pins issue #192 on the widget
// path: a Builder.Poll widget whose /state response carries
// X-Gofastr-Poll-Stop applies the final signal values, then stops — no
// further /state fetches. The stop header is the only thing that ends
// the cadence; without it the widget polls forever after terminal state.
func TestWidgetPoll_StopsOnTerminalHeader(t *testing.T) {
	var mu sync.Mutex
	stateHits := 0
	// 1s pollMs — the widget poll path doesn't apply the 5s clamp, so
	// the test observes multiple ticks within the chromedp timeout.
	const pollMs = 1000
	const terminalAt = 3

	page := `<!doctype html><html><head></head><body>
<div class="fui-widget fui-pos-bottom-right" data-fui-widget="jobpoll" role="status"><span data-fui-signal="status">0</span></div>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	widgetsJS, _ := Module("widgets")
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode([]map[string]any{
				{
					"hidden": false,
					"cfg": map[string]any{
						"name":          "jobpoll",
						"position":      "bottom-right",
						"backdrop":      false,
						"closeOnEscape": false,
						"closeOnClick":  false,
						"stylePath":     "/core-ui/widget/jobpoll/style.css",
						"chromePath":    "/core-ui/widget/jobpoll/chrome",
						"statePath":     "/core-ui/widget/jobpoll/state",
						"pollMs":        pollMs,
					},
				},
			})
		},
		"/core-ui/widget/jobpoll/state": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			stateHits++
			n := stateHits
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			// Once terminal, the server signals stop on this response.
			if n >= terminalAt {
				w.Header().Set("X-Gofastr-Poll-Stop", "1")
				fmt.Fprint(w, `{"status":"completed"}`)
				return
			}
			fmt.Fprintf(w, `{"status":"running-%d"}`, n)
		},
		"/core-ui/widget/jobpoll/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="jobpoll" role="status"><span data-fui-signal="status">0</span></div>`)
		},
		"/core-ui/widget/jobpoll/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		"/__gofastr/runtime/widgets.js": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(widgetsJS))
		},
	})

	ctx := newPollBrowserCtx(t)

	// Phase 1: wait for the terminal signal to land — proves ≥2 poll
	// ticks fired after mount hydration and the terminal body applied.
	var snapshot string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`window.__gofastr?._widgets?.["jobpoll"]`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Poll(`document.querySelector('[data-fui-signal="status"]')?.textContent === 'completed'`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(`JSON.stringify({
			val: document.querySelector('[data-fui-signal="status"]')?.textContent,
			hasPollStop: typeof window.__gofastr?._widgets?.["jobpoll"]?.pollStop === 'function',
		})`, &snapshot),
	); err != nil {
		t.Fatalf("chromedp phase 1: %v", err)
	}
	mu.Lock()
	hitsAtTerminal := stateHits
	mu.Unlock()

	// Phase 2: sleep well past 3 cadences — a leaked timer would have
	// fired /state several more times in this window.
	if err := chromedp.Run(ctx,
		chromedp.Sleep(3500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp phase 2: %v", err)
	}
	mu.Lock()
	hitsAfter := stateHits
	mu.Unlock()

	t.Logf("snapshot=%s hitsAtTerminal=%d hitsAfter=%d", snapshot, hitsAtTerminal, hitsAfter)

	var state struct {
		Val         string `json:"val"`
		HasPollStop bool   `json:"hasPollStop"`
	}
	if err := json.Unmarshal([]byte(snapshot), &state); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if state.Val != "completed" {
		t.Errorf("status signal = %q, want \"completed\" (terminal body not applied)", state.Val)
	}
	if !state.HasPollStop {
		t.Errorf("pollStop missing on widget entry — poll loop did not arm")
	}
	// After the terminal tick, /state must not be hit again. The stop
	// is authoritative; growth means the timer survived the header.
	if hitsAfter > hitsAtTerminal {
		t.Errorf("/state hits grew %d → %d after terminal — poll did not stop on header", hitsAtTerminal, hitsAfter)
	}
}
