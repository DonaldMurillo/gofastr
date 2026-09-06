//go:build desktop_e2e && darwin && arm64

package objc

// The phase-0 spike (docs/desktop-plan.md): a real GoFastr SSR app,
// served in-process over loopback, rendered inside a native WKWebView
// opened by a CGO_ENABLED=0 binary. The scenario walks the spike
// claims: client-side navigation between two screens, one island RPC
// round-trip, a JavaScript message into Go, a PNG snapshot of the live
// window, and every native callback landing on the Go main thread.
//
// Run by hand on a Mac with a display:
//
//	CGO_ENABLED=0 go test -count=1 -tags desktop_e2e -run TestSpike -v ./battery/desktop/internal/objc/
//
// Never in CI (see the plan's testing policy).

import (
	"context"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// ─── The app under test ──────────────────────────────────────────────

var spikeCount atomic.Int64

const islandSignal = "spike-count"

// countHTML is the island's inner content: what SSR renders inside the
// signal region and what the RPC handler returns after incrementing.
func countHTML() render.HTML {
	return render.Tag("strong", map[string]string{"id": "spike-count"},
		render.Text(fmt.Sprintf("Count: %d", spikeCount.Load())))
}

// homeScreen is the "/" screen: heading, nav link, one island.
type homeScreen struct{}

func (s *homeScreen) Render() render.HTML {
	region := interactive.BindHTML(render.Tag("div", nil, countHTML()), islandSignal)
	btn := interactive.OnClick(
		ui.Button(ui.ButtonConfig{Label: "Increment"}),
		interactive.Post("/spike/counter").OnSuccess(interactive.SetSignal(islandSignal)),
	)
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Spike Home Screen")),
		ui.Link(ui.LinkConfig{Href: "/two", Text: "Go to screen two"}),
		region,
		btn,
	)
}

// twoScreen is the "/two" screen: heading and a nav link back.
type twoScreen struct{}

func (s *twoScreen) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Spike Second Screen")),
		ui.Link(ui.LinkConfig{Href: "/", Text: "Back to home"}),
	)
}

// reqRecord is one observed request, for the Sec-Fetch-Site question.
type reqRecord struct {
	method, path, secFetchSite, secFetchDest, host string
}

// recorder keeps the first 40 requests' header facts.
type recorder struct {
	mu   sync.Mutex
	reqs []reqRecord
}

func (r *recorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if len(r.reqs) < 40 {
			r.reqs = append(r.reqs, reqRecord{
				method:       req.Method,
				path:         req.URL.Path,
				secFetchSite: req.Header.Get("Sec-Fetch-Site"),
				secFetchDest: req.Header.Get("Sec-Fetch-Dest"),
				host:         req.Host,
			})
		}
		r.mu.Unlock()
		next.ServeHTTP(w, req)
	})
}

// spikeOutDir is where spike.png lands.
func spikeOutDir() string {
	if d := os.Getenv("GOFASTR_SPIKE_OUT"); d != "" {
		return d
	}
	return "/private/tmp/claude-501/-Users-dom-programming-gofastr/3153fbe6-2abc-4626-bc55-2c56eeb94112/scratchpad/spike-out"
}

// ─── The spike ───────────────────────────────────────────────────────

// TestMain owns the process: `go test` runs test functions on spawned
// goroutines, which land on non-main OS threads, while AppKit work must
// happen on the exact thread the package init locked. TestMain is the
// one entry point that runs on the main goroutine, so the spike lives
// here. The isolation-probe subprocess (which needs normal test
// dispatch, not a window) is the exception and takes the m.Run path.
func TestMain(m *testing.M) {
	if os.Getenv("GOFASTR_ISOLATION_PROBE") == "1" {
		os.Exit(m.Run())
	}
	log := &spikeLog{name: "TestSpike"}
	fmt.Println("=== RUN  TestSpike (on the main thread; opens a window)")
	spikeTest(log)
	log.report()
	if len(log.problems) > 0 {
		fmt.Println("--- FAIL: TestSpike")
		os.Exit(1)
	}
	fmt.Println("--- PASS: TestSpike")
	os.Exit(0)
}

// spikeLog is the t.Log/t.Error surface for the TestMain-driven spike.
type spikeLog struct {
	name     string
	findings []string
	problems []string
}

func (l *spikeLog) Logf(format string, args ...any) {
	l.findings = append(l.findings, fmt.Sprintf(format, args...))
}

func (l *spikeLog) Errorf(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
}

func (l *spikeLog) Fatalf(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
	l.report()
	fmt.Println("--- FAIL: TestSpike")
	os.Exit(1)
}

func (l *spikeLog) report() {
	for _, f := range l.findings {
		fmt.Printf("    spike: %s\n", f)
	}
	for _, p := range l.problems {
		fmt.Printf("    spike problem: %s\n", p)
	}
}

func spikeTest(t *spikeLog) {
	// Worktree isolation would remap listen addresses; the spike runs
	// with it off and separately records what Resolve would have done
	// (question 3, below).
	os.Setenv("GOFASTR_ISOLATION", "off")

	AssertMainThread("spike test start")
	// Frameworks load explicitly now (init is lazy so a blank import
	// of the desktop battery loads nothing): the spike owns the main
	// thread, which is where AppKit's initializers must run.
	OpenFrameworks()

	// No warm-up call: the "first callback on an M loses its result
	// write" anomaly documented by the spike was a property of the
	// syscall9/libcCall path and does not reproduce under cgocall
	// (ffi.TestFirstCallbackReturnOnFreshM pins 16 fresh Ms and the
	// qsort GC regression asserts every one of ~50k comparator
	// returns). If a first-callback result ever reads back as zero
	// again, those two tests fail first.

	// Question 3: what WOULD isolation have done in this worktree? The
	// probe re-execs this binary with GOFASTR_ISOLATION unset, so the
	// process answers for the real worktree state.
	probeIsolation(t)

	// ── App: two screens, one island, one recording middleware ──
	rec := &recorder{}
	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "spike"}))
	fwApp.Use(rec.wrap)

	site := uiapp.NewApp("spike")
	layout := uiapp.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", &homeScreen{}, layout)
	site.Register("/two", &twoScreen{}, layout)
	fwApp.Mount(uihost.New(site))

	fwApp.Router().Post("/spike/counter", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := spikeCount.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<strong id=\"spike-count\">Count: %d</strong>", n)
	}))

	ready := make(chan string, 1)
	fwApp.OnReady(func(addr string) { ready <- addr })
	go func() {
		if err := fwApp.Start("127.0.0.1:0"); err != nil {
			t.Logf("app.Start returned: %v", err)
		}
	}()
	var addr string
	select {
	case addr = <-ready:
		t.Logf("app ready at %s", addr)
	case <-time.After(30 * time.Second):
		t.Fatalf("app did not become ready in 30s")
	}

	// ── Native window + web view, all on this (main) thread ──
	Send(ID(Send(Class("NSAutoreleasePool"), Sel("alloc"))), Sel("init"))

	nsApp := ID(Send(Class("NSApplication"), Sel("sharedApplication")))
	Send(nsApp, Sel("setActivationPolicy:"), 0) // NSApplicationActivationPolicyRegular

	// The bridge: one NSObject subclass whose methods are Go callbacks.
	var cbCount atomic.Int64
	scriptMsgs := make(chan string, 16)
	navFinish := make(chan struct{}, 16)
	navFail := make(chan string, 16)
	windowCloseAttempts := make(chan struct{}, 4)

	bridgeCls := RegisterClass("GofastrSpikeBridge", Class("NSObject"), []Method{
		{
			Sel:   "userContentController:didReceiveScriptMessage:",
			Types: "v@:@@",
			Fn: ffi.NewCallback(func(a *ffi.Args) uintptr {
				cbCount.Add(1)
				AssertMainThread("didReceiveScriptMessage")
				body := ID(Send(ID(a.Int[3]), Sel("body")))
				var s string
				if Send(body, Sel("isKindOfClass:"), uintptr(Class("NSString"))) != 0 {
					s = GoString(body)
				} else {
					s = GoString(ID(Send(body, Sel("description"))))
				}
				scriptMsgs <- s
				return 0
			}),
		},
		{
			Sel:   "webView:didFinishNavigation:",
			Types: "v@:@@",
			Fn: ffi.NewCallback(func(a *ffi.Args) uintptr {
				cbCount.Add(1)
				AssertMainThread("didFinishNavigation")
				navFinish <- struct{}{}
				return 0
			}),
		},
		{
			Sel:   "webView:didFailProvisionalNavigation:withError:",
			Types: "v@:@@@",
			Fn: ffi.NewCallback(func(a *ffi.Args) uintptr {
				cbCount.Add(1)
				AssertMainThread("didFailProvisionalNavigation")
				err := ID(a.Int[3])
				navFail <- GoString(ID(Send(err, Sel("localizedDescription"))))
				return 0
			}),
		},
		{
			Sel:   "windowShouldClose:",
			Types: "B@:@",
			Fn: ffi.NewCallback(func(a *ffi.Args) uintptr {
				cbCount.Add(1)
				AssertMainThread("windowShouldClose")
				windowCloseAttempts <- struct{}{}
				return 1 // YES, allow closing
			}),
		},
	})
	bridge := ID(Send(ID(Send(bridgeCls, Sel("alloc"))), Sel("init")))

	config := ID(Send(ID(Send(Class("WKWebViewConfiguration"), Sel("alloc"))), Sel("init")))
	ucc := ID(Send(config, Sel("userContentController")))
	Send(ucc, Sel("addScriptMessageHandler:name:"), uintptr(bridge), uintptr(NSString("gofastr")))

	webView := ID(SendRect(ID(Send(Class("WKWebView"), Sel("alloc"))),
		Sel("initWithFrame:configuration:"), Rect{}, uintptr(config)))
	Send(webView, Sel("setNavigationDelegate:"), uintptr(bridge))

	rect := Rect{0, 0, 1024, 768}
	styleMask, backing, deferFlag := uintptr(1|2|8), uintptr(2), uintptr(0)
	window := ID(SendRect(ID(Send(Class("NSWindow"), Sel("alloc"))),
		Sel("initWithContentRect:styleMask:backing:defer:"),
		rect, styleMask, backing, deferFlag))
	Send(window, Sel("setTitle:"), uintptr(NSString("GoFastr desktop spike")))
	Send(window, Sel("setContentView:"), uintptr(webView))
	Send(window, Sel("setDelegate:"), uintptr(bridge))
	Send(window, Sel("makeKeyAndOrderFront:"), 0)
	Send(window, Sel("center"))
	Send(nsApp, Sel("activateIgnoringOtherApps:"), 1)

	url := ID(Send(Class("NSURL"), Sel("URLWithString:"), uintptr(NSString("http://"+addr+"/"))))
	req := ID(Send(Class("NSURLRequest"), Sel("requestWithURL:"), uintptr(url)))
	Send(webView, Sel("loadRequest:"), uintptr(req))

	// ── The scenario drives everything through Main() hops while the
	// run loop owns this thread ──
	sc := &scenario{
		t: t, webView: webView, nsApp: nsApp,
		rec: rec, scriptMsgs: scriptMsgs,
		navFinish: navFinish, navFail: navFail,
		cbCount: &cbCount,
		evalRes: make(chan [2]string, 1),
	}
	sc.evalBlock = NewBlock(ffi.NewCallback(func(a *ffi.Args) uintptr {
		cbCount.Add(1)
		AssertMainThread("evaluateJavaScript block")
		var out [2]string
		if result := ID(a.Int[1]); result != 0 {
			out[0] = GoString(ID(Send(result, Sel("description"))))
		}
		if e := ID(a.Int[2]); e != 0 {
			out[1] = GoString(ID(Send(e, Sel("localizedDescription"))))
		}
		sc.evalRes <- out
		return 0
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc.run()
	}()

	// The GC hammer for the whole run: every 50ms a full GC while the
	// main goroutine sits parked inside cgocall([NSApp run]) and
	// callbacks run Go code on it.
	gcStop := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		cycles := 0
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-gcStop:
				t.Logf("gc hammer: %d cycles over the run", cycles)
				return
			case <-tick.C:
				runtime.GC()
				cycles++
			}
		}
	}()
	defer func() { <-gcDone }()

	// Run the event loop with the GC ON. The spike needed GC off
	// because it entered [NSApp run] through syscall9's libcCall,
	// which records the parked goroutine in m.libcall*; a GC scan then
	// marks it preemptShrink and the first callback stack check throws
	// "shrinking stack in libcall" (stack.go:1306). Every C call now
	// goes through runtime.cgocall (ffi.Call under the fake-cgo layer,
	// which sets runtime.iscgo so cgocall stops throwing), and cgocall
	// keeps no libcall record: the parked goroutine may be scanned,
	// shrunk, and its stack copied while a callback runs Go code on it.
	// The 30s soak below is the proof: GC every 50ms, a JS evaluation
	// (one native callback each) every 100ms, callbacks allocating.
	Send(nsApp, Sel("run")) // blocks until the scenario stops the loop
	close(gcStop)

	<-done
	// Off the run loop but still on the main thread: stop the page and
	// close the window so the SSE connection drops and Shutdown can
	// drain the HTTP server instead of timing out on it.
	Send(webView, Sel("stopLoading"))
	Send(window, Sel("performClose:"), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fwApp.Shutdown(ctx); err != nil {
		t.Errorf("app.Shutdown: %v", err)
	}

	sc.report(t)
}

// probeIsolation answers spike question 3 by re-execing this test
// binary with GOFASTR_ISOLATION stripped from the environment and
// resolving the listen address the way App.Start would have.
func probeIsolation(t *spikeLog) {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestIsolationProbeHelper$", "-test.v")
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFASTR_ISOLATION=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "GOFASTR_ISOLATION_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("q3: isolation probe subprocess failed: %v\n%s", err, out)
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ISOLATION ") {
			t.Logf("q3: %s", strings.TrimPrefix(line, "ISOLATION "))
			return
		}
	}
	t.Logf("q3: probe produced no result line; raw output:\n%s", out)
}

// TestIsolationProbeHelper runs only inside the probe subprocess.
func TestIsolationProbeHelper(t *testing.T) {
	if os.Getenv("GOFASTR_ISOLATION_PROBE") != "1" {
		t.Skip("probe helper runs only under TestSpike's subprocess")
	}
	rt, err := isolation.Resolve(".")
	if err != nil {
		fmt.Printf("ISOLATION resolve error: %v\n", err)
		return
	}
	addr, err := rt.Addr("127.0.0.1:0")
	if err != nil {
		fmt.Printf("ISOLATION addr error: %v\n", err)
		return
	}
	fmt.Printf("ISOLATION with GOFASTR_ISOLATION unset in this worktree: active=%v resolved=%q\n", rt.Active(), addr)
}

// ─── Scenario steps ──────────────────────────────────────────────────

type scenario struct {
	t          *spikeLog
	webView    ID
	nsApp      ID
	rec        *recorder
	scriptMsgs chan string
	navFinish  chan struct{}
	navFail    chan string
	cbCount    *atomic.Int64

	// evalBlock is the ONE completion-handler block every evalJS call
	// reuses: ffi callback slots are process-lifetime and never freed,
	// and the scenario evaluates JavaScript in poll loops, so a block
	// per call would exhaust the 64-slot table. Evals are strictly
	// sequential (the scenario waits for each), so one result channel
	// suffices.
	evalBlock uintptr
	evalRes   chan [2]string

	problems []string
	findings []string
}

func (s *scenario) fail(format string, args ...any) {
	s.problems = append(s.problems, fmt.Sprintf(format, args...))
}

func (s *scenario) note(format string, args ...any) {
	s.findings = append(s.findings, fmt.Sprintf(format, args...))
}

func (s *scenario) run() {
	// 1. Wait for initial navigation to finish, then read the heading.
	s.waitNav("initial load", 30*time.Second)
	h1 := strings.TrimSpace(s.evalJS("document.querySelector('h1').textContent"))
	if h1 != "Spike Home Screen" {
		s.fail("home h1 after load: %q", h1)
	} else {
		s.note("step1: home h1 = %q", h1)
	}

	// 2. Client-side navigation to /two: no didFinishNavigation.
	before := len(drainNav(s.navFinish))
	s.evalJS(`document.querySelector('a[href="/two"]').click()`)
	if !s.poll(5*time.Second, func() bool {
		return strings.TrimSpace(s.evalJS("location.pathname + ' | ' + document.querySelector('h1').textContent")) == "/two | Spike Second Screen"
	}) {
		s.fail("client-side nav to /two did not swap the screen (last: %q)",
			s.evalJS("location.pathname + ' | ' + document.querySelector('h1').textContent"))
	} else {
		s.note("step2: client-side nav to /two ok; heading swapped in place")
	}
	time.Sleep(time.Second) // let any stray didFinishNavigation arrive
	if n := len(drainNav(s.navFinish)) - before; n > 0 {
		s.note("step2 FINDING: %d didFinishNavigation fired for client-side nav (expected none)", n)
	} else {
		s.note("step2: no didFinishNavigation for client-side nav, as designed")
	}

	// 3. Navigate back and fire the island RPC.
	s.evalJS(`document.querySelector('a[href="/"]').click()`)
	if !s.poll(5*time.Second, func() bool {
		return strings.TrimSpace(s.evalJS("document.querySelector('h1').textContent")) == "Spike Home Screen"
	}) {
		s.fail("client-side nav back to / failed")
	}
	beforeCount := strings.TrimSpace(s.evalJS("document.querySelector('#spike-count').textContent"))
	s.evalJS("document.querySelector('button').click()")
	if !s.poll(5*time.Second, func() bool {
		return strings.TrimSpace(s.evalJS("document.querySelector('#spike-count').textContent")) != beforeCount
	}) {
		s.fail("island RPC did not change #spike-count (still %q)", beforeCount)
	} else {
		s.note("step3: island RPC round-trip: %q -> %q", beforeCount,
			strings.TrimSpace(s.evalJS("document.querySelector('#spike-count').textContent")))
	}

	// 4. Script message into Go.
	s.evalJS(`window.webkit.messageHandlers.gofastr.postMessage("hello")`)
	select {
	case m := <-s.scriptMsgs:
		if m != "hello" {
			s.fail("script message body: %q, want \"hello\"", m)
		} else {
			s.note("step4: Go received script message %q", m)
		}
	case <-time.After(5 * time.Second):
		s.fail("no script message arrived in Go within 5s")
	}

	// 5. SSE connection state.
	sse := "absent"
	if !s.poll(5*time.Second, func() bool {
		sse = s.evalJS("(window.__gofastr && window.__gofastr.sseStatus) ? String(window.__gofastr.sseStatus.connected) : 'absent'")
		return sse == "true"
	}) {
		s.fail("__gofastr.sseStatus did not report connected within 5s (last: %q)", sse)
	} else {
		s.note("step5: __gofastr.sseStatus.connected = %s", sse)
	}

	// 6. Service worker dormancy (synchronous controller probe only;
	// this WKWebView build does not resolve promise results).
	sw := s.evalJS("navigator.serviceWorker ? (navigator.serviceWorker.controller ? 'controlled' : 'none') : 'unsupported'")
	if sw != "none" {
		s.fail("service worker not dormant: %q", sw)
	} else {
		s.note("step6: service worker dormant: %q (no controller; WithPWA absent)", sw)
	}

	// 7. Snapshot the live window.
	if path := s.snapshot(); path != "" {
		s.verifySnapshot(path)
	}

	// 8. The GC soak: 30 seconds with a goroutine forcing runtime.GC()
	// every 50ms while the page evaluates JavaScript every 100ms (each
	// evaluation is one evaluateJavaScript completion callback plus a
	// Main() dispatch hop, and each callback allocates Go strings).
	// This is the spike's "cannot run with GC on" failure mode run in
	// reverse: it must survive the whole window with GC at full rate.
	soakStart := time.Now()
	for time.Since(soakStart) < 30*time.Second {
		if got := strings.TrimSpace(s.evalJS("String(1+1)")); got != "2" {
			s.fail("soak: evalJS(1+1) returned %q", got)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.note("soak: 30s with GC at 50ms and JS evals at 100ms survived; %d callbacks total", s.cbCount.Load())

	// 9. Snapshot again after the soak, then record the request headers
	// (question 1) and callback count (question 4), and stop.
	if path := s.snapshot(); path != "" {
		s.verifySnapshot(path)
	}
	s.rec.mu.Lock()
	for _, r := range s.rec.reqs {
		s.note("q1: %s %s: Sec-Fetch-Site=%q Sec-Fetch-Dest=%q Host=%q", r.method, r.path, r.secFetchSite, r.secFetchDest, r.host)
	}
	s.rec.mu.Unlock()
	s.note("q4: %d native callbacks executed; each asserted the main thread (a mismatch would have panicked)", s.cbCount.Load())

	s.stopLoop()
}

func drainNav(ch chan struct{}) []struct{} {
	var out []struct{}
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		default:
			return out
		}
	}
}

func (s *scenario) waitNav(what string, timeout time.Duration) {
	select {
	case <-s.navFinish:
		return
	case msg := <-s.navFail:
		s.fail("%s: provisional navigation failed: %s", what, msg)
		return
	case <-time.After(timeout):
		s.fail("%s: no didFinishNavigation within %s", what, timeout)
	}
}

func (s *scenario) poll(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// evalJS evaluates JavaScript and returns the result's description
// ("" for nil results). The shared completion block runs on the main
// thread. The expression is coerced through String(...) so undefined
// results (postMessage & friends, click()) come back as "undefined"
// instead of surfacing as "unsupported result type" JS errors; this
// WKWebView build does not auto-await promise results, so everything
// evaluated here must be synchronous.
func (s *scenario) evalJS(js string) string {
	err := Main(func() {
		wrapped := "String((" + js + "))"
		Send(s.webView, Sel("evaluateJavaScript:completionHandler:"), uintptr(NSString(wrapped)), s.evalBlock)
	})
	if err != nil {
		s.fail("evalJS Main: %v (js: %s)", err, js)
		return ""
	}
	select {
	case out := <-s.evalRes:
		if out[1] != "" {
			s.fail("evalJS js-error: %s (js: %s)", out[1], js)
			return ""
		}
		return out[0]
	case <-time.After(10 * time.Second):
		s.fail("evalJS completion did not fire in 10s (js: %s)", js)
		return ""
	}
}

// snapshot captures the window as PNG bytes through
// takeSnapshotWithConfiguration:completionHandler: and writes them to
// the spike output directory. The conversion happens inside the
// completion block, on the main thread.
func (s *scenario) snapshot() string {
	res := make(chan string, 1)
	err := Main(func() {
		blk := NewBlock(ffi.NewCallback(func(a *ffi.Args) uintptr {
			s.cbCount.Add(1)
			AssertMainThread("takeSnapshot block")
			image := ID(a.Int[1])
			if image == 0 {
				if e := ID(a.Int[2]); e != 0 {
					s.fail("takeSnapshot error: %s", GoString(ID(Send(e, Sel("localizedDescription")))))
				} else {
					s.fail("takeSnapshot returned no image and no error")
				}
				res <- ""
				return 0
			}
			tiff := Send(image, Sel("TIFFRepresentation"))
			rep := ID(Send(Class("NSBitmapImageRep"), Sel("imageRepWithData:"), uintptr(tiff)))
			pngType := int64(4) // NSBitmapImageFileTypePNG
			data := ID(Send(rep, Sel("representationUsingType:properties:"), uintptr(pngType), 0))
			if data == 0 {
				s.fail("representationUsingType returned nil")
				res <- ""
				return 0
			}
			n := Send(data, Sel("length"))
			p := Send(data, Sel("bytes"))
			if p == 0 || n == 0 {
				s.fail("PNG NSData empty")
				res <- ""
				return 0
			}
			buf := make([]byte, n)
			copy(buf, unsafe.Slice((*byte)(cptr(p)), n))
			dir := spikeOutDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				s.fail("mkdir %s: %v", dir, mkErr)
				res <- ""
				return 0
			}
			path := dir + "/spike.png"
			if wErr := os.WriteFile(path, buf, 0o600); wErr != nil {
				s.fail("write %s: %v", path, wErr)
				res <- ""
				return 0
			}
			s.note("step7: snapshot written to %s (%d bytes)", path, len(buf))
			res <- path
			return 0
		}))
		Send(s.webView, Sel("takeSnapshotWithConfiguration:completionHandler:"), 0, blk)
	})
	if err != nil {
		s.fail("snapshot Main: %v", err)
		return ""
	}
	select {
	case p := <-res:
		return p
	case <-time.After(15 * time.Second):
		s.fail("snapshot completion did not fire in 15s")
		return ""
	}
}

// verifySnapshot asserts the PNG is a real rendered page (plan gate:
// theme background at the four corners, rich color variety).
func (s *scenario) verifySnapshot(path string) {
	f, err := os.Open(path)
	if err != nil {
		s.fail("open snapshot: %v", err)
		return
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		s.fail("decode snapshot: %v", err)
		return
	}
	b := img.Bounds()
	if b.Dx() < 1000 || b.Dy() < 700 {
		s.fail("snapshot size %dx%d, want >= 1000x700", b.Dx(), b.Dy())
	}
	at := func(x, y int) (uint32, uint32, uint32, uint32) { return img.At(x, y).RGBA() }
	c1, _, _, _ := at(b.Min.X, b.Min.Y)
	c2, _, _, _ := at(b.Max.X-1, b.Min.Y)
	c3, _, _, _ := at(b.Min.X, b.Max.Y-1)
	c4, _, _, _ := at(b.Max.X-1, b.Max.Y-1)
	if c1 != c2 || c2 != c3 || c3 != c4 {
		s.fail("corner pixels differ: %v %v %v %v", c1, c2, c3, c4)
	} else {
		s.note("step7: four corner pixels equal (r=%d g=%d b=%d)", c1>>8, c2>>8, c3>>8)
	}
	colors := map[[3]uint32]bool{}
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			colors[[3]uint32{r >> 8, g >> 8, bl >> 8}] = true
		}
	}
	if len(colors) < 200 {
		s.fail("snapshot has only %d distinct sampled colors, want >= 200: looks like a blank sheet", len(colors))
	} else {
		s.note("step7: %d distinct sampled colors", len(colors))
	}
}

// stopLoop stops [NSApp run] and posts a dummy application-defined
// event so the loop notices immediately. otherEventWithType: takes an
// NSPoint and an NSTimeInterval (double) by value, so it goes through
// Invoke like the other struct-by-value calls.
func (s *scenario) stopLoop() {
	err := Main(func() {
		Send(s.nsApp, Sel("stop:"), 0)
		evType := uintptr(15) // NSEventTypeApplicationDefined
		location := Point{}
		ev := SendF(Class("NSEvent"),
			Sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			[]uintptr{evType, 0 /*flags*/, 0 /*winNum*/, 0 /*ctx*/, 0 /*subtype*/, 0 /*data1*/, 0 /*data2*/},
			[]float64{location.X, location.Y, 0 /*timestamp*/})
		Send(s.nsApp, Sel("postEvent:atStart:"), ev, 1)
	})
	if err != nil {
		s.fail("stopLoop Main: %v", err)
	}
}

func (s *scenario) report(t *spikeLog) {
	for _, f := range s.findings {
		t.Logf("FINDING %s", f)
	}
	for _, p := range s.problems {
		t.Errorf("%s", p)
	}
}
