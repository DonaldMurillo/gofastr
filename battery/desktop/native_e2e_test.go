//go:build desktop_e2e && darwin && arm64

package desktop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The battery's app-shell tests: the REAL darwin shell (AppKit +
// WKWebView) driven through the REAL battery with the exported
// desktoptest harness. This file owns the app under test and TestMain;
// shell_darwin_e2e_test.go owns the scenario that walks the whole
// shell. They replace browser_harness_e2e_test.go (deleted): desktop
// tests never run against a browser stand-in, the page half runs
// inside the WKWebView the app ships in.
//
// Run by hand on a Mac with a display (never in CI):
//
//	CGO_ENABLED=0 go test -count=1 -tags desktop_e2e -run . ./battery/desktop/
//
// TestMain runs the unit tests FIRST, in the pre-Run world
// (TestShellConstructsLazy pins DlopenCount 0, so no framework may
// load before m.Run returns; the old e2e TestMain did the same), then
// hands the process to desktoptest.NativePhase: one app, booted on the
// main goroutine the shell owns, with the native tests and the
// scenario driven from a goroutine once the page is ready.

// ─── The app under test ──────────────────────────────────────────────

// e2eTitle is the main window's configured title (and the app menu's).
const e2eTitle = "Desktop Shell E2E"

// e2eTrayTitle is the tray item's configured menu-bar title.
const e2eTrayTitle = "E2ENotes"

// e2eScheme is the deep-link scheme the app claims.
const e2eScheme = "e2enative"

// e2eAppID is the reverse-DNS app id (names the data dir).
const e2eAppID = "shell-e2e.gofastr.dev"

var e2eCount atomic.Int64

func e2eCountHTML() render.HTML {
	return render.Tag("strong", map[string]string{"id": "e2e-count"},
		render.Text(fmt.Sprintf("Count: %d", e2eCount.Load())))
}

type e2eHome struct{}

func (e2eHome) Render() render.HTML {
	region := interactive.BindHTML(render.Tag("div", nil, e2eCountHTML()), "e2e-count")
	btn := interactive.OnClick(
		ui.Button(ui.ButtonConfig{Label: "Increment"}),
		interactive.Post("/shell-e2e/counter").OnSuccess(interactive.SetSignal("e2e-count")),
	)
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Shell E2E Home")),
		ui.Link(ui.LinkConfig{Href: "/two", Text: "Go to screen two"}),
		region,
		btn,
	)
}

type e2eTwo struct{}

func (e2eTwo) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Shell E2E Second")),
		ui.Link(ui.LinkConfig{Href: "/", Text: "Back to home"}),
	)
}

type e2eSettings struct{}

func (e2eSettings) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Shell E2E Settings")),
		ui.Link(ui.LinkConfig{Href: "/", Text: "Back to home"}),
	)
}

type e2eMini struct{}

func (e2eMini) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("h1", nil, render.Text("Shell E2E Mini")),
	)
}

// e2eChrome is the phase 13 step's window: the page contract a
// material needs (transparent html and body, a left column the width
// of the configured sidebar zone) written as one fixture rule. The
// desktop theme ships this shape properly later; the step asserts the
// native side against the OS, so the fixture only has to let the
// effect show through.
type e2eChrome struct{}

func (e2eChrome) Render() render.HTML {
	style := render.Tag("style", nil, render.Text(
		"html,body{background:transparent}"+
			".chrome-sidebar{position:fixed;left:0;top:0;bottom:0;width:220px;"+
			"background:rgba(128,128,150,0.15);border-right:1px solid rgba(128,128,128,0.35)}"))
	return render.Tag("div", nil,
		style,
		render.Tag("div", map[string]string{"class": "chrome-sidebar"},
			render.Tag("h1", nil, render.Text("Sidebar zone")),
		),
		render.Tag("p", nil, render.Text("Chrome content column")),
	)
}

// e2eHandlerRan receives the menu Handler item's context error.
var e2eHandlerRan chan error

// e2eDataDir is the app's data dir under the phase's temp base
// (NativePhase points GOFASTR_DESKTOP_DATA_DIR at one when unset).
func e2eDataDir() string {
	return filepath.Join(os.Getenv("GOFASTR_DESKTOP_DATA_DIR"), e2eAppID)
}

// buildNativeApp assembles the app the whole e2e build runs against:
// the uiApp shape (three screens, an island) plus everything the
// native tests and the scenario exercise: a menu, a tray, settings, a
// widget spec, and the deep-link scheme.
func buildNativeApp() (*framework.App, *desktop.Battery, error) {
	opts, err := desktop.AppOptions(e2eAppID)
	if err != nil {
		return nil, nil, err
	}
	fwApp := framework.NewApp(append(opts, framework.WithConfig(framework.AppConfig{Name: "shellE2E"}))...)

	site := uiapp.NewApp("shellE2E")
	layout := uiapp.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", e2eHome{}, layout)
	site.Register("/two", e2eTwo{}, layout)
	site.Register("/settings", e2eSettings{}, layout)
	site.Register("/mini", e2eMini{}, layout)
	site.Register("/chrome", e2eChrome{}, layout)
	fwApp.Mount(uihost.New(site))

	fwApp.Router().Post("/shell-e2e/counter", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := e2eCount.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<strong id=\"e2e-count\">Count: %d</strong>", n)
	}))

	e2eHandlerRan = make(chan error, 1)
	b := native.New(desktop.Config{
		ID:      e2eAppID,
		Title:   e2eTitle,
		DataDir: e2eDataDir(),
		Width:   1100,
		Height:  800,
		Settings: &desktop.WindowSpec{
			Path:   "/settings",
			Title:  "Settings",
			Width:  420,
			Height: 320,
		},
		// A widget spec opens at launch: the NSPanel the phase asserts
		// on (borderless, floating, non-activating). It takes "w2";
		// later windows.open calls continue from "w3". The path is its
		// own screen: OpenWindow dedupes by path, and "/two" is what
		// the two-windows test and the scenario's styled window open.
		Widgets: []desktop.WindowSpec{desktop.Widget("/mini", 300, 200)},
		Tray: &desktop.Tray{
			Title:            e2eTrayTitle,
			Tooltip:          "Shell E2E",
			CloseHidesWindow: true,
			Menu: &desktop.Menu{Items: []desktop.MenuItem{
				{Title: "Show Window", Role: desktop.RoleShow},
				{Role: desktop.RoleQuit},
			}},
		},
		Menu: &desktop.Menu{Items: []desktop.MenuItem{
			{Title: "Go", Children: []desktop.MenuItem{
				{Title: "Two", Navigate: "/two", Key: "cmd+2"},
				{Title: "Ping", Handler: func(ctx context.Context) error {
					select {
					case e2eHandlerRan <- ctx.Err():
					default:
					}
					return nil
				}},
			}},
		}},
		DeepLink: &desktop.DeepLinkConfig{Scheme: e2eScheme},
		// RememberWindows: the phase step moves the real main window,
		// asserts state.json picked it up, and opens a framed
		// secondary window.
		RememberWindows: true,
	})
	fwApp.RegisterBattery(b)
	return fwApp, b, nil
}

// ─── TestMain and the phase driver ───────────────────────────────────

func TestMain(m *testing.M) {
	// Unit tests first, in the pre-Run world (the lazy-loading pin
	// needs DlopenCount 0, so m.Run must finish before AppKit loads).
	code := m.Run()
	if code == 0 {
		code = desktoptest.NativePhase(buildNativeApp, runNativePhase)
	}
	os.Exit(code)
}

// runNativePhase drives the native tests and the scenario, in this
// order: the scenario ends by quitting the app, so it must be last.
func runNativePhase(h *desktoptest.NativeHarness) bool {
	fmt.Println("=== native phase (opens a window on the real shell)")
	ok := true
	steps := []struct {
		name string
		fn   func(t desktoptest.TB, h *desktoptest.NativeHarness)
	}{
		{"MainWindowHoldsThePage", phaseMainWindowHoldsPage},
		{"EvalAsyncGuards", phaseEvalAsyncGuards},
		{"BridgeAnswersThroughThePage", phaseBridgeThroughPage},
		{"PromptedCallsGate", phasePromptedCalls},
		{"EmitReachesPageListener", phaseEmitReachesListener},
		{"MenuNavigateMovesRealPage", phaseMenuMovesPage},
		{"TrayTitleReachesStatusItem", phaseTrayTitle},
		{"TwoWindowsTalkThroughBridge", phaseTwoWindows},
		{"WidgetSpecOpensAPanel", phaseWidgetSpec},
		{"DeepLinkNavigatesRealPage", phaseDeepLink},
		{"PageStateRoundTrips", phasePageState},
		{"RemembersWindowFrames", phaseWindowState},
		{"ChromeContractOnTheOS", phaseWindowChrome},
		{"SnapshotDecodesAtWindowScale", phaseSnapshot},
		{"ShellScenario", phaseShellScenario},
	}
	for _, step := range steps {
		if !runPhaseTest(h, step.name, step.fn) {
			ok = false
		}
	}
	return ok
}

// phaseAbort unwinds a failed phase test; the runner catches it.
type phaseAbort struct{ msg string }

// phaseT is desktoptest.TB for the hand-driven phase: a Fatalf aborts
// the current step and fails the run. testing.TB itself cannot be
// implemented outside package testing since Go 1.24 (it carries an
// unexported method), which is why desktoptest.TB exists.
type phaseT struct {
	name   string
	failed bool
}

func (p *phaseT) Helper() {}
func (p *phaseT) Name() string {
	return p.name
}
func (p *phaseT) Logf(format string, args ...any) {
	fmt.Printf("    %s: %s\n", p.name, fmt.Sprintf(format, args...))
}
func (p *phaseT) Fatal(args ...any) {
	p.failed = true
	panic(phaseAbort{msg: fmt.Sprint(args...)})
}
func (p *phaseT) Fatalf(format string, args ...any) {
	p.failed = true
	panic(phaseAbort{msg: fmt.Sprintf(format, args...)})
}

// runPhaseTest runs one hand-driven step, catching its abort.
func runPhaseTest(h *desktoptest.NativeHarness, name string, fn func(t desktoptest.TB, h *desktoptest.NativeHarness)) bool {
	t := &phaseT{name: name}
	h.SetTB(t)
	failed := true
	defer func() {
		switch r := recover(); {
		case r == nil:
			if failed {
				fmt.Printf("--- FAIL: %s\n", name)
			} else {
				fmt.Printf("--- PASS: %s\n", name)
			}
		default:
			ab, isAbort := r.(phaseAbort)
			if !isAbort {
				panic(r)
			}
			fmt.Printf("--- FAIL: %s: %s\n", name, ab.msg)
		}
	}()
	fn(t, h)
	failed = t.failed
	return !failed
}

// jsString decodes one JSON string result from the page.
func jsString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Sprintf("<not a string: %s>", raw)
	}
	return s
}

// wantCallOK fails unless the bridge call resolved.
func wantCallOK(t desktoptest.TB, r *desktoptest.CallResult, what string) json.RawMessage {
	t.Helper()
	if r == nil || !r.OK {
		t.Fatalf("%s rejected: code=%q message=%q", what, r.Code, r.Message)
	}
	return r.Result
}

// wantCallCode fails unless the bridge call rejected with code.
func wantCallCode(t desktoptest.TB, r *desktoptest.CallResult, code, what string) {
	t.Helper()
	if r == nil || r.OK {
		t.Fatalf("%s resolved, want rejection %q (result %s)", what, code, r.Result)
	}
	if r.Code != code {
		t.Fatalf("%s rejected with %q (%s), want %q", what, r.Code, r.Message, code)
	}
}

// ─── The native tests ────────────────────────────────────────────────

// phaseEvalAsyncGuards proves the harness's own guards on the real
// page: a value, undefined, a throw with its message, an awaited
// promise, and a hung evaluation timing out on the caller's context
// long before the 20 s cap.
func phaseEvalAsyncGuards(t desktoptest.TB, h *desktoptest.NativeHarness) {
	w, ok := h.Battery.Window()
	if !ok {
		t.Fatalf("no main window")
	}
	pe, ok := w.(desktop.PageEvaluator)
	if !ok {
		t.Fatalf("the window %T implements no desktop.PageEvaluator", w)
	}
	ctx := func(d time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), d)
	}

	c, cancel := ctx(30 * time.Second)
	r, err := pe.EvalAsync(c, "return 40 + 2")
	cancel()
	if err != nil || string(r) != "42" {
		t.Fatalf("a value: r=%s err=%v, want 42", r, err)
	}

	c, cancel = ctx(30 * time.Second)
	r, err = pe.EvalAsync(c, "return undefined")
	cancel()
	if err != nil || string(r) != "null" {
		t.Fatalf("undefined: r=%s err=%v, want null", r, err)
	}

	c, cancel = ctx(30 * time.Second)
	r, err = pe.EvalAsync(c, "return await new Promise(res => setTimeout(() => res({k: 7}), 50))")
	cancel()
	if err != nil || string(r) != `{"k":7}` {
		t.Fatalf("a promise: r=%s err=%v, want {\"k\":7}", r, err)
	}

	c, cancel = ctx(30 * time.Second)
	_, err = pe.EvalAsync(c, `throw new Error("e2e boom")`)
	cancel()
	var de *desktop.Error
	if !errors.As(err, &de) || de.Code != desktop.CodeInternal || !strings.Contains(de.Message, "e2e boom") {
		t.Fatalf("a throw: err=%v, want internal carrying the message", err)
	}

	c, cancel = ctx(1 * time.Second)
	start := time.Now()
	_, err = pe.EvalAsync(c, "await new Promise(res => setTimeout(res, 5000))")
	cancel()
	elapsed := time.Since(start)
	if !errors.As(err, &de) || de.Code != desktop.CodeInternal || de.Message != "page evaluation timed out" {
		t.Fatalf("a hung eval: err=%v, want internal \"page evaluation timed out\"", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("a hung eval with ctx 1s took %s; the context must bound the wait", elapsed)
	}
}

// phaseBridgeThroughPage: the bridge answers window.title through the
// real page.
func phaseBridgeThroughPage(t desktoptest.TB, h *desktoptest.NativeHarness) {
	var out struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("window", "title", nil), "window.title"), &out); err != nil {
		t.Fatalf("window.title result: %v", err)
	}
	if out.Title != e2eTitle {
		t.Fatalf("window.title() through the page = %q, want %q", out.Title, e2eTitle)
	}
}

// phasePromptedCalls: an allowed notifications.show reaches the
// notification log (the unbundled host then answers unsupported, after
// recording), and a denied clipboard.readText rejects with denied.
func phasePromptedCalls(t desktoptest.TB, h *desktoptest.NativeHarness) {
	h.Answer(desktop.DecisionAllowOnce, desktop.DecisionDeny)
	wantCallCode(t, h.Call("notifications", "show", map[string]any{"title": "From the phase", "body": "b"}),
		desktop.CodeUnsupported, "notifications.show (unbundled)")
	found := false
	for _, n := range h.Notifications() {
		if n.Title == "From the phase" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the allowed notifications.show never reached the log: %+v", h.Notifications())
	}
	wantCallCode(t, h.Call("clipboard", "readText", nil), desktop.CodeDenied, "clipboard.readText")
}

// phaseEmitReachesListener: Battery.Emit reaches a page listener.
func phaseEmitReachesListener(t desktoptest.TB, h *desktoptest.NativeHarness) {
	h.RecordEvents(t, "exported")
	if err := h.Battery.Emit("exported", map[string]any{"path": "/tmp/notes.md", "n": 3}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	ev := h.WaitEvent("exported")
	var p struct {
		Path string `json:"path"`
		N    int    `json:"n"`
	}
	if err := ev.Unmarshal(&p); err != nil || p.Path != "/tmp/notes.md" || p.N != 3 {
		t.Fatalf("listener payload = %s (err %v)", ev.Payload, err)
	}
}

// phaseMenuMovesPage: a menu Navigate item moves the real page; a
// Handler item runs its Go func.
func phaseMenuMovesPage(t desktoptest.TB, h *desktoptest.NativeHarness) {
	h.ClickMenu("Go", "Two")
	h.WaitLocation("/two")
	h.WaitText("h1", "Shell E2E Second")
	if got := h.Text("h1"); got != "Shell E2E Second" {
		t.Fatalf("h1 after the menu navigate = %q", got)
	}
	h.ClickMenu("Go", "Ping")
	select {
	case err := <-e2eHandlerRan:
		if err != nil {
			t.Fatalf("the Handler item ran with a dead context: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Handler item never ran")
	}
	h.Navigate("/")
}

// phaseTrayTitle: tray.setTitle through the page reaches the real
// status item.
func phaseTrayTitle(t desktoptest.TB, h *desktoptest.NativeHarness) {
	reader, ok := h.Shell.(interface{ StatusItemTitle() string })
	if !ok {
		t.Fatalf("the shell %T exposes no StatusItemTitle", h.Shell)
	}
	if got := reader.StatusItemTitle(); got != e2eTrayTitle {
		t.Fatalf("status item title at boot = %q, want %q", got, e2eTrayTitle)
	}
	wantCallOK(t, h.Call("tray", "setTitle", map[string]any{"title": e2eTrayTitle + "2"}), "tray.setTitle")
	h.Wait("the status item title to change", func() bool {
		return reader.StatusItemTitle() == e2eTrayTitle+"2"
	})
	wantCallOK(t, h.Call("tray", "setTitle", map[string]any{"title": e2eTrayTitle}), "tray.setTitle back")
	h.Wait("the status item title to return", func() bool {
		return reader.StatusItemTitle() == e2eTrayTitle
	})
}

// phaseTwoWindows: a second window opened through the page receives a
// windows.post from main, and windows.self answers each window's own
// id.
func phaseTwoWindows(t desktoptest.TB, h *desktoptest.NativeHarness) {
	var opened struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("windows", "open", map[string]any{"path": "/two"}), "windows.open"), &opened); err != nil {
		t.Fatalf("windows.open result: %v", err)
	}
	if opened.ID == "" {
		t.Fatal("windows.open returned no id")
	}
	h.Wait("the second window", func() bool { return h.Window(opened.ID) != nil })
	nw := h.Window(opened.ID)
	h.Wait("the second page's runtime to load", func() bool {
		raw, err := nw.EvalQuiet(`if (!(window.__gofastr && window.__gofastr.desktop)) return false;` +
			` window.__phaseGot = null;` +
			` if (!window.__pingHooked) { window.__pingHooked = true;` +
			` window.__gofastr.desktop.on("ping_w2", p => { window.__phaseGot = p; }); }` +
			` return true;`)
		var b bool
		return err == nil && json.Unmarshal(raw, &b) == nil && b
	})

	wantCallOK(t, h.Call("windows", "post", map[string]any{
		"to":   opened.ID,
		"name": "ping_w2",
		// RawMessage keeps the sender's key order, which the arrival
		// check below asserts (a Go map would marshal alphabetically).
		"payload": json.RawMessage(`{"n":7,"from":"main"}`),
	}), "windows.post")
	h.Wait("the posted message to arrive", func() bool {
		raw, err := nw.EvalQuiet(`return window.__phaseGot === null ? null : JSON.stringify(window.__phaseGot)`)
		return err == nil && jsString(raw) == `{"n":7,"from":"main"}`
	})

	var selfMain struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("windows", "self", nil), "windows.self from main"), &selfMain); err != nil {
		t.Fatalf("windows.self from main: %v", err)
	}
	selfW2 := jsString(nw.Eval(t, `try { const r = await window.__gofastr.desktop.call("windows", "self", undefined);`+
		` return r.id; } catch (e) { return "err:" + (e && e.code); }`))
	if selfMain.ID != "main" || selfW2 != opened.ID {
		t.Fatalf("windows.self: main page %q, second page %q (want main and %q)", selfMain.ID, selfW2, opened.ID)
	}

	wantCallOK(t, h.Call("windows", "close", map[string]any{"id": opened.ID}), "windows.close")
	h.Wait("the second window to close", func() bool { return h.Window(opened.ID) == nil })
}

// phaseWidgetSpec: the launch widget spec opened an NSPanel with no
// titled bit, at the floating level, never key.
func phaseWidgetSpec(t desktoptest.TB, h *desktoptest.NativeHarness) {
	const (
		maskTitled             = 1 << 0 // NSWindowStyleMaskTitled
		maskNonactivatingPanel = 1 << 7 // NSWindowStyleMaskNonactivatingPanel
		floatingLevel          = 3      // NSFloatingWindowLevel
	)
	st, err := h.WindowState("w2")
	if err != nil {
		t.Fatalf("the launch widget: %v", err)
	}
	if st.Class != "NSPanel" {
		t.Fatalf("the widget's class = %q, want NSPanel", st.Class)
	}
	if st.StyleMask&maskTitled != 0 {
		t.Fatalf("the widget's styleMask = %#x, want no titled bit", st.StyleMask)
	}
	if st.StyleMask&maskNonactivatingPanel == 0 {
		t.Fatalf("the widget's styleMask = %#x, want the non-activating bit", st.StyleMask)
	}
	if st.Level != floatingLevel {
		t.Fatalf("the widget's level = %d, want %d (floating)", st.Level, floatingLevel)
	}
	if st.Key {
		t.Fatal("the widget became the key window; a non-activating panel must not")
	}
}

// phaseDeepLink: a URL posted the way the OS does navigates the main
// page and delivers deep_link.
func phaseDeepLink(t desktoptest.TB, h *desktoptest.NativeHarness) {
	h.Navigate("/")
	h.RecordEvents(t, "deep_link")
	h.OpenURL(e2eScheme + "://two")
	h.WaitLocation("/two")
	ev := h.WaitEvent("deep_link")
	var p struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := ev.Unmarshal(&p); err != nil || p.URL != e2eScheme+"://two" || p.Path != "/two" {
		t.Fatalf("deep_link payload = %s (err %v)", ev.Payload, err)
	}
}

// phasePageState: the real page's bridge round-trips a state key
// (state.set then state.get through the typed namespace, driven by
// EvalAsync), the state_changed event reaches the page listener, and
// the value lands on disk in state.json.
func phasePageState(t desktoptest.TB, h *desktoptest.NativeHarness) {
	w, ok := h.Battery.Window()
	if !ok {
		t.Fatalf("no main window")
	}
	pe, ok := w.(desktop.PageEvaluator)
	if !ok {
		t.Fatalf("the window %T implements no desktop.PageEvaluator", w)
	}
	h.RecordEvents(t, "state_changed")
	// The typed namespace arrives with the bridge script; wait for it
	// rather than racing its loadModule handshake.
	h.Wait("the state namespace on the bridge", func() bool {
		raw, err := h.EvalQuiet(`return !!(window.__gofastr && window.__gofastr.desktop && window.__gofastr.desktop.state)`)
		var b bool
		return err == nil && json.Unmarshal(raw, &b) == nil && b
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := pe.EvalAsync(ctx, `const D = window.__gofastr.desktop;`+
		` await D.state.set({key: "page.demo", value: {n: 1}});`+
		` const got = await D.state.get({key: "page.demo"});`+
		` return got.value;`)
	if err != nil || string(raw) != `{"n":1}` {
		t.Fatalf("state round trip through the page: r=%s err=%v, want {\"n\":1}", raw, err)
	}
	ev := h.WaitEvent("state_changed")
	var p struct {
		Key string `json:"key"`
	}
	if err := ev.Unmarshal(&p); err != nil || p.Key != "page.demo" {
		t.Fatalf("state_changed payload = %s (err %v)", ev.Payload, err)
	}
	h.Wait("state.json to hold the value", func() bool {
		data, err := os.ReadFile(filepath.Join(e2eDataDir(), "state.json"))
		return err == nil && strings.Contains(string(data), `"page.demo"`)
	})
}

// phaseWindowState: SetFrame on the real main window, the windowDidMove
// delegate reports through OnWindowFrame, state.json holds the frame
// after the debounce, and a secondary window opened with a Frame lands
// exactly there per the OS's own WindowState.
func phaseWindowState(t desktoptest.TB, h *desktoptest.NativeHarness) {
	// A frame that fits any real display (the screen check would drop
	// one that is not, which is the unit suite's case).
	main := desktop.Frame{X: 60, Y: 80, Width: 900, Height: 640}
	h.MoveWindow("main", main)
	// The delegate reported and the read path round-trips the flip.
	h.Wait("windowDidMove to report the frame", func() bool {
		f, err := h.WindowFrame("main")
		return err == nil && f == main
	})
	// The store's debounced write landed in the data dir.
	h.Wait("state.json to hold the main frame", func() bool {
		data, err := os.ReadFile(filepath.Join(e2eDataDir(), "state.json"))
		if err != nil {
			return false
		}
		return strings.Contains(string(data), `"X": 60`) && strings.Contains(string(data), `"Height": 640`)
	})
	t.Logf("main window moved to %+v, state.json holds it", main)

	// A secondary window opened with a Frame lands there.
	framed := desktop.Frame{X: 200, Y: 300, Width: 500, Height: 350}
	w, err := h.Battery.OpenWindow(desktop.WindowSpec{
		Path:   "/settings",
		Title:  "Framed",
		Width:  400,
		Height: 300,
		Frame:  &framed,
	})
	if err != nil {
		t.Fatalf("OpenWindow with a Frame: %v", err)
	}
	if w.ID() != "settings" {
		t.Fatalf("framed window id = %q, want settings", w.ID())
	}
	st, err := h.WindowState("settings")
	if err != nil {
		t.Fatalf("framed window state: %v", err)
	}
	if st.X != framed.X || st.Y != framed.Y || st.Width != framed.Width || st.Height != framed.Height {
		t.Fatalf("framed window landed at %+v, want %+v", st, framed)
	}
	t.Logf("secondary window with a Frame landed at X=%d Y=%d %dx%d", st.X, st.Y, st.Width, st.Height)
	h.CloseWindow("settings")
	h.Wait("the framed window to close", func() bool { return h.Window("settings") == nil })
}

// phaseWindowChrome: a secondary window with the full phase 13 chrome
// contract (MaterialSidebar, ChromeUnified, a traffic-light inset, a
// sidebar width). Every assertion reads the OS's own WindowState off
// the live objects; SetSidebarWidth and the page's setChrome both move
// the zone; the Reduce Transparency read matches the OS defaults
// domain; the focus events reach the page across a deactivate and
// reactivate, and the id gate keeps another window's focus from
// clearing this page's inactive class; the CGWindowID capture is the
// pixel proof.
func phaseWindowChrome(t desktoptest.TB, h *desktoptest.NativeHarness) {
	w, err := h.Battery.OpenWindow(desktop.WindowSpec{
		Path:   "/chrome",
		Title:  "Chrome",
		Width:  560,
		Height: 440,
		Style: desktop.WindowStyle{
			Material:          desktop.MaterialSidebar,
			Chrome:            desktop.ChromeUnified,
			TrafficLightInset: &desktop.Inset{X: 12, Y: 10},
		},
		SidebarWidth: 220,
	})
	if err != nil {
		t.Fatalf("OpenWindow with chrome: %v", err)
	}
	id := w.ID()
	h.Wait("the chrome window to open", func() bool { return h.Window(id) != nil })
	st, err := h.WindowState(id)
	if err != nil {
		t.Fatalf("chrome window state: %v", err)
	}
	// The sidebar material always answers the zone vibrancy view (the
	// glass shape is whole-window only); what the OS reports is what
	// the shell applied.
	if st.Material != "vibrancy-sidebar" {
		t.Fatalf("chrome window material = %q, want vibrancy-sidebar", st.Material)
	}
	if !st.TitlebarTransparent {
		t.Fatal("chrome window title bar is not transparent")
	}
	if st.ToolbarStyle != "unified" {
		t.Fatalf("chrome window toolbar style = %q, want unified", st.ToolbarStyle)
	}
	if st.SidebarWidth != 220 {
		t.Fatalf("chrome window sidebar width = %d, want 220", st.SidebarWidth)
	}
	if st.CGWindowID == 0 {
		t.Fatal("chrome window has no CGWindowID")
	}
	t.Logf("chrome window: material=%s toolbar=%s sidebar=%d cgwindow=%d",
		st.Material, st.ToolbarStyle, st.SidebarWidth, st.CGWindowID)

	// SetSidebarWidth moves the zone the OS reports.
	if err := w.SetSidebarWidth(280); err != nil {
		t.Fatalf("SetSidebarWidth: %v", err)
	}
	h.Wait("the zone to resize to 280", func() bool {
		s2, err := h.WindowState(id)
		return err == nil && s2.SidebarWidth == 280
	})

	// The page's own report through the capability reaches the same
	// place. The secondary window's page has not loaded the desktop
	// module yet, so this is the module's own transport (fetch with the
	// window header) spelled inline. Wait for the page first: a page
	// still at about:blank has no base URL for the relative fetch.
	h.Wait("the chrome page to reach /chrome", func() bool {
		out, err := h.Window(id).EvalQuiet("return location.pathname")
		return err == nil && jsString(out) == "/chrome"
	})
	nw := h.Window(id)
	nw.Eval(t, fmt.Sprintf("return await fetch('/__gofastr/desktop/call/window/setChrome', {"+
		"method:'POST', headers:{'Content-Type':'application/json','X-Gofastr-Window':%q}, "+
		"body: JSON.stringify({sidebarWidth:240}), credentials:'same-origin'}).then(r => r.text())", id))
	h.Wait("the page report to resize the zone to 240", func() bool {
		s2, err := h.WindowState(id)
		return err == nil && s2.SidebarWidth == 240
	})

	// The shell's Reduce Transparency read matches the OS domain.
	out, derr := exec.Command("defaults", "read", "com.apple.universalaccess", "reduceTransparency").Output()
	osOn := derr == nil && strings.TrimSpace(string(out)) == "1"
	if got := h.Battery.Shell().Appearance().ReduceTransparency; got != osOn {
		t.Fatalf("shell Reduce Transparency = %v, OS defaults domain = %v (out=%q err=%v)", got, osOn, strings.TrimSpace(string(out)), derr)
	}
	t.Logf("Reduce Transparency matches the OS: %v", osOn)

	// Focus events reach the page across a deactivate/reactivate, the
	// user's app-switch shape.
	h.RecordEvents(t, "window_focus", "window_blur")
	h.DeactivateReactivate()
	ev := h.WaitEvent("window_blur")
	var blurID struct {
		ID string `json:"id"`
	}
	if err := ev.Unmarshal(&blurID); err != nil || blurID.ID != id {
		t.Fatalf("window_blur payload = %s, want id %q", ev.Payload, id)
	}
	if os.Getenv("GOFASTR_CHROME_DEBUG") != "" {
		time.Sleep(700 * time.Millisecond)
		for _, dbg := range []string{id, "main"} {
			if ds, derr := h.WindowState(dbg); derr == nil {
				t.Logf("DEBUG after switch: %s key=%v visible=%v", dbg, ds.Key, ds.Visible)
			}
		}
		if err := w.Focus(); err != nil {
			t.Logf("DEBUG Focus: %v", err)
		}
		time.Sleep(700 * time.Millisecond)
		evs := h.Events()
		names := make([]string, 0, len(evs))
		for _, e := range evs {
			names = append(names, e.Name)
		}
		t.Logf("DEBUG events so far: %v", names)
	}
	ev = h.WaitEvent("window_focus")
	var focusID struct {
		ID string `json:"id"`
	}
	if err := ev.Unmarshal(&focusID); err != nil || focusID.ID != id {
		t.Fatalf("window_focus payload = %s, want id %q", ev.Payload, id)
	}

	// The id gate, behaviorally: the MAIN page carries desktop-inactive
	// (it lost key when this window opened) even though this window's
	// window_focus event reaches every page.
	var inactive bool
	h.EvalInto(t, "return document.documentElement.classList.contains('desktop-inactive')", &inactive)
	if !inactive {
		t.Fatal("main page lacks desktop-inactive while another window holds key; the focus classes must be gated on the event's window id")
	}

	// The pixel proof: capture this window by its CGWindowID.
	if dir := os.Getenv("GOFASTR_DESKTOP_PROOF_DIR"); dir != "" {
		path := filepath.Join(dir, "window-chrome-"+id+".png")
		if err := exec.Command("screencapture", "-x", "-l",
			strconv.FormatInt(st.CGWindowID, 10), path).Run(); err != nil {
			t.Logf("screencapture failed (no display?): %v", err)
		} else {
			t.Logf("chrome window capture written to %s", path)
		}
	}
	h.CloseWindow(id)
	h.Wait("the chrome window to close", func() bool { return h.Window(id) == nil })
}

// phaseSnapshot: the window's real pixels decode as a PNG at the
// window's size times an integer backing scale.
func phaseSnapshot(t desktoptest.TB, h *desktoptest.NativeHarness) {
	img := h.Snapshot(t)
	// The snapshot captures the web view's CONTENT, whose point size
	// is the page's viewport (the frame WindowState reports includes
	// the title bar), so compare against window.innerWidth/innerHeight.
	var vp struct {
		W int `json:"w"`
		H int `json:"h"`
	}
	h.EvalInto(t, "return {w: window.innerWidth, h: window.innerHeight}", &vp)
	b := img.Bounds()
	for scale := 1; scale <= 3; scale++ {
		if b.Dx() == vp.W*scale && b.Dy() == vp.H*scale {
			t.Logf("snapshot %dx%d at %dx backing, viewport %dx%d points", b.Dx(), b.Dy(), scale, vp.W, vp.H)
			return
		}
	}
	t.Fatalf("snapshot %dx%d does not match the viewport %dx%d points at any integer scale", b.Dx(), b.Dy(), vp.W, vp.H)
}

// phaseMainWindowHoldsPage pins the fact a web-view snapshot cannot:
// the WKWebView is the main window's content view, so the page is on
// screen. A refactor once dropped setContentView: for the main window
// and every app opened blank while every snapshot test passed.
func phaseMainWindowHoldsPage(t desktoptest.TB, h *desktoptest.NativeHarness) {
	st, err := h.WindowState("main")
	if err != nil {
		t.Fatalf("main window state: %v", err)
	}
	if st.ContentClass != "WKWebView" || !st.Visible {
		t.Fatalf("main window state = %+v, want a visible window whose content view is the WKWebView", st)
	}
}
