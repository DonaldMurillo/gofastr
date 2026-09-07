package desktoptest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
)

// REAL app shell (the WKWebView the app ships in), never against a
// browser stand-in. NativeMain boots one app per test binary through
// Battery.Run on the main goroutine (the shell owns the main thread),
// starts m.Run on another goroutine once the page is ready, and quits
// the app when the tests finish. On a host with no native shell the
// tests still run and Native skips.
//
//	NativeMain(m, func() (*framework.App, *desktop.Battery, error) {
//		return buildApp(nil) // the app's own builder, no fake shell
//	})
//
// battery/desktop's own suite cannot use NativeMain (its unit tests
// pin the pre-Run world, so m.Run must finish before AppKit loads);
// NativePhase is the same machine for a hand-driven phase after
// m.Run.

// TB is the slice of testing.TB the native harness needs. testing.TB
// itself has carried an unexported method since Go 1.24, so nothing
// outside package testing can implement it; a *testing.T satisfies
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// nativeTimeoutEnv names the watchdog's budget (a Go duration).
const nativeTimeoutEnv = "GOFASTR_NATIVE_TIMEOUT"

// nativeHarness is the one running app's harness, installed by
// NativeMain or NativePhase before the tests run and read by Native.
var nativeHarness *NativeHarness

// NativeMain runs the app inside the real shell for the whole test
// binary. Call it from TestMain:
//
//	os.Exit(desktoptest.NativeMain(m, build))
//
// It sets GOFASTR_DESKTOP_DATA_DIR to a fresh temp dir (unless set)
// and GOFASTR_ISOLATION=off, calls build (the app's own buildApp with
// a nil shell, so the host's native.New fills the real one), registers a
// watchdog that quits after GOFASTR_NATIVE_TIMEOUT (default 10 min),
// and waits for readiness: the main window exists and the page reports
// its desktop manifest. It returns the exit code (non-zero when Run
// failed). On a host with no native shell it runs m.Run with no app,
// and Native skips.
func NativeMain(m *testing.M, build func() (*framework.App, *desktop.Battery, error)) int {
	bootEnv()
	app, d, err := build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "desktoptest.NativeMain: build failed: %v\n", err)
		code := m.Run()
		if code == 0 {
			code = 1
		}
		return code
	}
	h := newNativeHarness(app, d)
	testCode := make(chan int, 1)
	go func() {
		select {
		case <-h.ready:
			nativeHarness = h
			testCode <- m.Run()
			h.quitSafely() // no-op when a test already quit the app
		case <-h.runDone:
			// No app this run (unsupported host or a boot that failed):
			// the tests run and Native skips.
			testCode <- m.Run()
		}
	}()
	runErr := h.block()
	code := <-testCode
	if runFailed(runErr) || h.timedOut.Load() {
		if runFailed(runErr) {
			fmt.Fprintf(os.Stderr, "desktoptest.NativeMain: Battery.Run returned %v\n", runErr)
		}
		if code == 0 {
			code = 1
		}
	}
	return code
}

// NativePhase is NativeMain for a package whose unit tests must see
// the pre-Run world (battery/desktop's own suite pins DlopenCount 0
// before any Run loads AppKit, so m.Run has already happened when the
// phase starts). fn runs on a goroutine with the harness once the page
// is ready; when fn returns the app quits and the phase's exit code is
// 0 exactly when fn and Battery.Run both succeeded. Like NativeMain,
// fn runs while Run owns the main thread, so fn must never be the
// caller's goroutine: NativePhase blocks in Run itself.
func NativePhase(build func() (*framework.App, *desktop.Battery, error), fn func(h *NativeHarness) bool) int {
	bootEnv()
	app, d, err := build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "desktoptest.NativePhase: build failed: %v\n", err)
		return 1
	}
	h := newNativeHarness(app, d)
	phaseDone := make(chan bool, 1)
	go func() {
		select {
		case <-h.ready:
			nativeHarness = h
			phaseDone <- fn(h)
			h.quitSafely()
		case <-h.runDone:
			phaseDone <- false
		}
	}()
	runErr := h.block()
	ok := <-phaseDone
	h.watchdog.Stop()
	if runFailed(runErr) {
		fmt.Fprintf(os.Stderr, "desktoptest.NativePhase: Battery.Run returned %v\n", runErr)
		return 1
	}
	if h.timedOut.Load() || !ok {
		return 1
	}
	return 0
}

// Native returns the running app's harness, or skips the test when
// NativeMain did not start one (a host with no native shell, or a boot
// that failed before the page was ready).
func Native(t testing.TB) *NativeHarness {
	t.Helper()
	h := nativeHarness
	if h == nil {
		t.Skip("desktoptest: no native app is running on this host")
	}
	h.tb = t
	return h
}

// NativeHarness drives the real app: the page side runs inside the
// window's own JavaScript, the native side goes straight through the
// shell's NativeDriver.
type NativeHarness struct {
	// App is the app under test; Battery its desktop battery; Shell
	// the native shell the battery runs on.
	App     *framework.App
	Battery *desktop.Battery
	Shell   desktop.Shell

	// tb is the fail/log target the t-less helpers report through;
	// Native installs it, a NativePhase driver calls SetTB.
	tb TB

	ready    chan struct{}
	runDone  chan struct{}
	runMu    sync.Mutex
	runErr   error
	watchdog *time.Timer
	timedOut atomic.Bool

	// eventsSeen is WaitEvent's cursor into Events.
	eventsSeen int
}

// newNativeHarness wires the harness and starts its readiness watcher
// and watchdog. The caller (NativeMain/NativePhase) runs block on the
// main goroutine right after.
func newNativeHarness(app *framework.App, d *desktop.Battery) *NativeHarness {
	h := &NativeHarness{
		App:     app,
		Battery: d,
		Shell:   d.Shell(),
		ready:   make(chan struct{}),
		runDone: make(chan struct{}),
	}
	go h.watchReady()
	timeout := nativeTimeout()
	h.watchdog = time.AfterFunc(timeout, func() {
		h.timedOut.Store(true)
		fmt.Fprintf(os.Stderr, "desktoptest: the native run exceeded %s; quitting\n", timeout)
		h.quitSafely()
	})
	return h
}

// block runs Battery.Run and records its result. Only NativeMain and
// NativePhase call it, on the caller's (the process's main) goroutine.
func (h *NativeHarness) block() error {
	runErr := h.Battery.Run(h.App)
	h.runMu.Lock()
	h.runErr = runErr
	h.runMu.Unlock()
	close(h.runDone)
	return runErr
}

// nativeReadyWait bounds the wait for the page to boot.
const nativeReadyWait = 60 * time.Second

// manifestBody is the readiness probe: the runtime booted, the desktop
// demand module loaded, and the generated bridge.js installed the
// frozen manifest.
const manifestBody = "return !!(window.__gofastr && window.__gofastr.desktop && window.__gofastr.desktop.manifest)"

// watchReady closes h.ready once the main window's page answers the
// manifest probe. A window that cannot evaluate at all (a shell
// without PageEvaluator) counts as ready rather than hanging the
// phase; the watchdog ends a boot that never gets that far.
func (h *NativeHarness) watchReady() {
	deadline := time.Now().Add(nativeReadyWait)
	for time.Now().Before(deadline) {
		select {
		case <-h.runDone:
			return
		default:
		}
		if h.pageReady() {
			close(h.ready)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// pageReady is one readiness probe.
func (h *NativeHarness) pageReady() bool {
	w, ok := h.Battery.Window()
	if !ok {
		return false
	}
	pe, canEval := w.(desktop.PageEvaluator)
	if !canEval {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := pe.EvalAsync(ctx, manifestBody)
	return err == nil && string(r) == "true"
}

// SetTB installs the fail/log target the t-less helpers (Wait, Events,
// ClickMenu, ...) report through; Native(t) does it for every test.
func (h *NativeHarness) SetTB(t TB) { h.tb = t }

// fatalf reports through tb, or panics when none was installed (a
// driver forgot SetTB; the message stays diagnosable).
func (h *NativeHarness) fatalf(format string, args ...any) {
	if h.tb == nil {
		panic("desktoptest: " + fmt.Sprintf(format, args...))
	}
	h.tb.Fatalf(format, args...)
}

// ----- the page side (everything runs inside the page) --------------

// harnessEvalWait bounds one page evaluation from the harness side.
const harnessEvalWait = 30 * time.Second

// EvalQuiet runs body in the main window's page and returns the raw
// JSON result, error included, for poll conditions.
func (h *NativeHarness) EvalQuiet(body string) (json.RawMessage, error) {
	w, ok := h.Battery.Window()
	if !ok {
		return nil, errors.New("no main window")
	}
	pe, ok := w.(desktop.PageEvaluator)
	if !ok {
		return nil, fmt.Errorf("the window %T implements no desktop.PageEvaluator", w)
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessEvalWait)
	defer cancel()
	return pe.EvalAsync(ctx, body)
}

// mainEval is EvalQuiet that fails the test on error.
func (h *NativeHarness) mainEval(t TB, body string) json.RawMessage {
	t.Helper()
	r, err := h.EvalQuiet(body)
	if err != nil {
		t.Fatalf("desktoptest: page evaluation failed: %v (body: %.160s)", err, body)
	}
	return r
}

// Eval runs body (an async function body) in the main window's page
// and returns the JSON-encoded result.
func (h *NativeHarness) Eval(t TB, body string) json.RawMessage {
	t.Helper()
	return h.mainEval(t, body)
}

// EvalInto runs body and decodes the result into v.
func (h *NativeHarness) EvalInto(t TB, body string, v any) {
	t.Helper()
	r := h.mainEval(t, body)
	if err := json.Unmarshal(r, v); err != nil {
		t.Fatalf("desktoptest: page result %s does not decode into %T: %v", r, v, err)
	}
}

// Call invokes a capability method through the page's own bridge
// transport (window.__gofastr.desktop.call, what the typed namespaces
// wrap); a rejection's code and message land in the CallResult.
func (h *NativeHarness) Call(capability, method string, input any) *CallResult {
	in := "undefined"
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return &CallResult{err: err}
		}
		in = string(b)
	}
	capJSON, _ := json.Marshal(capability)
	metJSON, _ := json.Marshal(method)
	body := fmt.Sprintf(`try { const r = await window.__gofastr.desktop.call(%s, %s, %s);`+
		` return {ok: true, result: (r === undefined ? null : r)}; }`+
		` catch (e) { return {ok: false, code: (e && e.code) || "error",`+
		` message: String((e && e.message) || e)}; }`, capJSON, metJSON, in)
	raw, err := h.EvalQuiet(body)
	if err != nil {
		return &CallResult{err: err}
	}
	var env struct {
		OK      bool            `json:"ok"`
		Result  json.RawMessage `json:"result"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return &CallResult{err: fmt.Errorf("bridge envelope %s does not decode: %w", raw, err)}
	}
	return &CallResult{OK: env.OK, Result: env.Result, Code: env.Code, Message: env.Message}
}

// Get fetches path from inside the page (the window's own session,
// same-origin credentials) and records the reply.
func (h *NativeHarness) Get(path string) *Response {
	pathJSON, _ := json.Marshal(path)
	body := fmt.Sprintf(`const r = await fetch(%s, {credentials: "same-origin"});`+
		` return {status: r.status, body: await r.text()};`, pathJSON)
	raw, err := h.EvalQuiet(body)
	if err != nil {
		return &Response{err: err}
	}
	var out struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return &Response{err: fmt.Errorf("fetch envelope %s does not decode: %w", raw, err)}
	}
	return &Response{Status: out.Status, Body: out.Body}
}

// Post issues a JSON POST from the page (what its islands and forms
// send to the app's own routes).
func (h *NativeHarness) Post(path string, body any) *Response {
	pathJSON, _ := json.Marshal(path)
	payload := "undefined"
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &Response{err: err}
		}
		payload = string(b)
	}
	js := fmt.Sprintf(`const r = await fetch(%s, {method: "POST", credentials: "same-origin",`+
		` headers: {"Content-Type": "application/json"}, body: JSON.stringify(%s)});`+
		` return {status: r.status, body: await r.text()};`, pathJSON, payload)
	raw, err := h.EvalQuiet(js)
	if err != nil {
		return &Response{err: err}
	}
	var post struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &post); err != nil {
		return &Response{err: fmt.Errorf("fetch envelope %s does not decode: %w", raw, err)}
	}
	return &Response{Status: post.Status, Body: post.Body}
}

// Navigate moves the page client-side (window.__gofastr.navigate, no
// reload) and waits for the path to arrive.
func (h *NativeHarness) Navigate(path string) {
	pathJSON, _ := json.Marshal(path)
	h.mainEval(h.tbOrPanic(), fmt.Sprintf(`window.__gofastr.navigate(%s); return true;`, pathJSON))
	h.WaitLocation(path)
}

// tbOrPanic returns the installed TB or a fatal one, for the helpers
// the brief spells without a t parameter.
func (h *NativeHarness) tbOrPanic() TB {
	if h.tb == nil {
		panic("desktoptest: no TB installed; call Native(t) or SetTB first")
	}
	return h.tb
}

// Location returns the main page's current path.
func (h *NativeHarness) Location() string {
	var p string
	h.EvalInto(h.tbOrPanic(), "return location.pathname", &p)
	return p
}

// textOr reads one element's text, reporting whether it existed.
func (h *NativeHarness) textOr(selector string) (string, bool) {
	sel, _ := json.Marshal(selector)
	raw, err := h.EvalQuiet(fmt.Sprintf(`const el = document.querySelector(%s); return el ? el.textContent : null;`, sel))
	if err != nil {
		return "", false
	}
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return "", false
	}
	return *s, true
}

// Text reads one element's text and fails the test when it is missing.
func (h *NativeHarness) Text(selector string) string {
	s, ok := h.textOr(selector)
	if !ok {
		h.fatalf("desktoptest: no element matches %s", selector)
	}
	return s
}

// ExistsQuiet reports whether the selector matches an element, false
// when the page cannot answer, for poll conditions.
func (h *NativeHarness) ExistsQuiet(selector string) bool {
	sel, _ := json.Marshal(selector)
	raw, err := h.EvalQuiet(fmt.Sprintf(`return document.querySelector(%s) !== null;`, sel))
	if err != nil {
		return false
	}
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}

// TextQuiet is Text without the fatal: "" when the element is missing
// or the page cannot answer, for poll conditions.
func (h *NativeHarness) TextQuiet(selector string) string {
	s, _ := h.textOr(selector)
	return s
}

// Exists reports whether the selector matches an element.
func (h *NativeHarness) Exists(selector string) bool {
	sel, _ := json.Marshal(selector)
	var b bool
	h.EvalInto(h.tbOrPanic(), fmt.Sprintf(`return document.querySelector(%s) !== null;`, sel), &b)
	return b
}

// Click clicks the first element the selector matches.
func (h *NativeHarness) Click(selector string) {
	sel, _ := json.Marshal(selector)
	raw, err := h.EvalQuiet(fmt.Sprintf(`const el = document.querySelector(%s); if (el) { el.click(); return true; } return false;`, sel))
	var clicked bool
	if err != nil || json.Unmarshal(raw, &clicked) != nil || !clicked {
		h.fatalf("desktoptest: no clickable element matches %s", selector)
	}
}

// Fill sets an input's value the way typing does (value plus an input
// event).
func (h *NativeHarness) Fill(selector, value string) {
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	raw, err := h.EvalQuiet(fmt.Sprintf(`const el = document.querySelector(%s);`+
		` if (!el) return false; el.value = %s;`+
		` el.dispatchEvent(new Event("input", {bubbles: true})); return true;`, sel, val))
	var filled bool
	if err != nil || json.Unmarshal(raw, &filled) != nil || !filled {
		h.fatalf("desktoptest: no fillable element matches %s", selector)
	}
}

// Submit submits the form the selector matches through the runtime's
// form intercept (requestSubmit).
func (h *NativeHarness) Submit(selector string) {
	sel, _ := json.Marshal(selector)
	raw, err := h.EvalQuiet(fmt.Sprintf(`const f = document.querySelector(%s);`+
		` if (!f || !f.requestSubmit) return false; f.requestSubmit(); return true;`, sel))
	var submitted bool
	if err != nil || json.Unmarshal(raw, &submitted) != nil || !submitted {
		h.fatalf("desktoptest: no submittable form matches %s", selector)
	}
}

// ----- waiting -------------------------------------------------------

// nativePoll and nativeWait bound Wait and its derivatives.
const (
	nativePoll = 50 * time.Millisecond
	nativeWait = 10 * time.Second
)

// Wait polls cond every 50 ms for up to 10 s and fails the test naming
// what it waited for.
func (h *NativeHarness) Wait(what string, cond func() bool) {
	deadline := time.Now().Add(nativeWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(nativePoll)
	}
	if cond() {
		return
	}
	h.fatalf("desktoptest: timed out waiting for %s", what)
}

// WaitText waits until the element's text contains substr.
func (h *NativeHarness) WaitText(selector, substr string) {
	h.Wait("text "+substr+" at "+selector, func() bool {
		return strings.Contains(h.TextQuiet(selector), substr)
	})
}

// WaitLocation waits until the main page's path is path.
func (h *NativeHarness) WaitLocation(path string) {
	h.Wait("location "+path, func() bool {
		raw, err := h.EvalQuiet("return location.pathname")
		if err != nil {
			return false
		}
		var p string
		return json.Unmarshal(raw, &p) == nil && p == path
	})
}

// ----- snapshots and events ------------------------------------------

// Snapshot captures the main window's real pixels and decodes them.
func (h *NativeHarness) Snapshot(t TB) image.Image {
	t.Helper()
	pngBytes, err := h.snapshotBytes()
	if err != nil {
		t.Fatalf("desktoptest: snapshot: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("desktoptest: snapshot does not decode as PNG: %v", err)
	}
	return img
}

// SavePNG captures the main window and writes the PNG bytes to path.
func (h *NativeHarness) SavePNG(t TB, path string) {
	t.Helper()
	pngBytes, err := h.snapshotBytes()
	if err != nil {
		t.Fatalf("desktoptest: snapshot: %v", err)
	}
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		t.Fatalf("desktoptest: write %s: %v", path, err)
	}
}

func (h *NativeHarness) snapshotBytes() ([]byte, error) {
	w, ok := h.Battery.Window()
	if !ok {
		return nil, errors.New("no main window")
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessEvalWait)
	defer cancel()
	return w.Snapshot(ctx)
}

// RecordEvents installs a page-side recorder for the named events
// (window.__gofastrTestEvents); listeners stay installed across calls.
func (h *NativeHarness) RecordEvents(t TB, names ...string) {
	t.Helper()
	if len(names) == 0 {
		return
	}
	namesJSON, _ := json.Marshal(names)
	body := fmt.Sprintf(`window.__gofastrTestEvents = window.__gofastrTestEvents || {all: []};`+
		` const rec = window.__gofastrTestEvents;`+
		` for (const n of %s) { if (rec["on_" + n]) continue; rec["on_" + n] = true;`+
		` window.__gofastr.desktop.on(n, p => rec.all.push({name: n, payload: p})); }`+
		` return true;`, namesJSON)
	h.mainEval(t, body)
}

// Events reads the recorder back, in delivery order; nil when no
// recorder is installed yet.
func (h *NativeHarness) Events() []Event {
	raw, err := h.EvalQuiet(`return window.__gofastrTestEvents ? window.__gofastrTestEvents.all : []`)
	if err != nil {
		return nil
	}
	var out []Event
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// WaitEvent waits for the next event named name that Events has not
// handed out through an earlier WaitEvent, and returns it.
func (h *NativeHarness) WaitEvent(name string) Event {
	seen := h.eventsSeen
	var got Event
	h.Wait("event "+name, func() bool {
		evs := h.Events()
		for i := seen; i < len(evs); i++ {
			if evs[i].Name == name {
				got = evs[i]
				h.eventsSeen = i + 1
				return true
			}
		}
		return false
	})
	return got
}

// ----- the native side (straight through NativeDriver) ---------------

func (h *NativeHarness) driver() (desktop.NativeDriver, error) {
	d, ok := h.Shell.(desktop.NativeDriver)
	if !ok {
		return nil, fmt.Errorf("the shell %T implements no desktop.NativeDriver", h.Shell)
	}
	return d, nil
}

// ClickMenu activates a main-menu item by its title path.
func (h *NativeHarness) ClickMenu(titles ...string) {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	if err := d.ActivateMenu(titles...); err != nil {
		h.fatalf("desktoptest: menu activation %v failed: %v", titles, err)
	}
}

// ClickTray activates a tray-menu row by title.
func (h *NativeHarness) ClickTray(title string) {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	if err := d.ActivateTray(title); err != nil {
		h.fatalf("desktoptest: tray activation %q failed: %v", title, err)
	}
}

// Native returns the window's raw native web view handle, the escape
// hatch for a platform-specific oracle.
func (nw *NativeWindow) Native() uintptr { return nw.w.Native() }

// OpenSettings activates the app menu's own Settings item.
func (h *NativeHarness) OpenSettings() {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	if err := d.OpenSettingsItem(); err != nil {
		h.fatalf("desktoptest: the Settings item failed: %v", err)
	}
}

// Answer queues decisions for the next permission prompts (no alert
// shows for them).
func (h *NativeHarness) Answer(decisions ...desktop.Decision) {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	d.ScriptPrompts(decisions...)
}

// ClickPrompt clicks a button on the modal permission alert that is
// up (the real alert path). The error is the caller's to report: an
// answerer usually runs this on a goroutine while a blocked call
// waits for its prompt.
func (h *NativeHarness) ClickPrompt(button string) error {
	d, err := h.driver()
	if err != nil {
		return err
	}
	return d.ClickPrompt(button)
}

// OpenURL delivers a URL the way the OS does.
func (h *NativeHarness) OpenURL(rawURL string) {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	if err := d.PostDeepLink(rawURL); err != nil {
		h.fatalf("desktoptest: posting %s failed: %v", rawURL, err)
	}
}

// CloseWindow is the red button on that window.
func (h *NativeHarness) CloseWindow(id string) {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	if err := d.CloseWindowNative(id); err != nil {
		h.fatalf("desktoptest: closing %s failed: %v", id, err)
	}
}

// MoveWindow drives a real SetFrame on the window; the native delegate
// reports the move through WindowConfig.OnWindowFrame by itself.
func (h *NativeHarness) MoveWindow(id string, f desktop.Frame) {
	w := h.Window(id)
	if w == nil {
		h.fatalf("desktoptest: no window %q", id)
	}
	if err := w.w.SetFrame(f); err != nil {
		h.fatalf("desktoptest: SetFrame %s: %v", id, err)
	}
}

// WindowFrame reads the window's live frame (top-left screen points).
func (h *NativeHarness) WindowFrame(id string) (desktop.Frame, error) {
	w := h.Window(id)
	if w == nil {
		return desktop.Frame{}, fmt.Errorf("desktoptest: no window %q", id)
	}
	return w.w.Frame()
}

// WindowState reads the OS facts about a window.
func (h *NativeHarness) WindowState(id string) (desktop.WindowState, error) {
	d, err := h.driver()
	if err != nil {
		return desktop.WindowState{}, err
	}
	return d.WindowState(id)
}

// Notifications is every Notification Show received, in order.
func (h *NativeHarness) Notifications() []desktop.Notification {
	d, err := h.driver()
	if err != nil {
		h.fatalf("desktoptest: %v", err)
	}
	return d.NotificationLog()
}

// WindowIDs lists the battery's live windows, main first.
func (h *NativeHarness) WindowIDs() []string {
	var ids []string
	for _, w := range h.Battery.Windows() {
		ids = append(ids, w.ID())
	}
	return ids
}

// Window returns the live window with the given id, or nil when there
// is none yet (poll with Wait for one that is still opening).
func (h *NativeHarness) Window(id string) *NativeWindow {
	for _, w := range h.Battery.Windows() {
		if w.ID() == id {
			return &NativeWindow{h: h, w: w}
		}
	}
	return nil
}

// WaitRun waits for Battery.Run to return (the quit role, the
// watchdog, or a failure) and reports its error. ok is false when the
// timeout passed first.
func (h *NativeHarness) WaitRun(timeout time.Duration) (error, bool) {
	select {
	case <-h.runDone:
		h.runMu.Lock()
		defer h.runMu.Unlock()
		return h.runErr, true
	case <-time.After(timeout):
		return nil, false
	}
}

// Click clicks the first element the selector matches in this
// window's page.
func (nw *NativeWindow) Click(t TB, selector string) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	var clicked bool
	raw := nw.Eval(t, fmt.Sprintf(`const el = document.querySelector(%s); if (el) { el.click(); return true; } return false;`, sel))
	if json.Unmarshal(raw, &clicked) != nil || !clicked {
		t.Fatalf("desktoptest: no clickable element matches %s in %s", selector, nw.ID())
	}
}

// Fill sets an input's value in this window's page the way typing
// does.
func (nw *NativeWindow) Fill(t TB, selector, value string) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	var filled bool
	raw := nw.Eval(t, fmt.Sprintf(`const el = document.querySelector(%s);`+
		` if (!el) return false; el.value = %s;`+
		` el.dispatchEvent(new Event("input", {bubbles: true})); return true;`, sel, val))
	if json.Unmarshal(raw, &filled) != nil || !filled {
		t.Fatalf("desktoptest: no fillable element matches %s in %s", selector, nw.ID())
	}
}

// Submit submits the form the selector matches in this window's page
// through the runtime's form intercept.
func (nw *NativeWindow) Submit(t TB, selector string) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	var submitted bool
	raw := nw.Eval(t, fmt.Sprintf(`const f = document.querySelector(%s);`+
		` if (!f || !f.requestSubmit) return false; f.requestSubmit(); return true;`, sel))
	if json.Unmarshal(raw, &submitted) != nil || !submitted {
		t.Fatalf("desktoptest: no submittable form matches %s in %s", selector, nw.ID())
	}
}

// NativeWindow is one live window seen from its page and from the OS.
type NativeWindow struct {
	h *NativeHarness
	w desktop.Window
}

// ID is the window's id ("main", "settings", "w2", ...).
func (nw *NativeWindow) ID() string { return nw.w.ID() }

func (nw *NativeWindow) eval(body string) (json.RawMessage, error) {
	pe, ok := nw.w.(desktop.PageEvaluator)
	if !ok {
		return nil, fmt.Errorf("the window %T implements no desktop.PageEvaluator", nw.w)
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessEvalWait)
	defer cancel()
	return pe.EvalAsync(ctx, body)
}

// Eval runs body in this window's page and returns the JSON-encoded
// result.
func (nw *NativeWindow) Eval(t TB, body string) json.RawMessage {
	t.Helper()
	r, err := nw.eval(body)
	if err != nil {
		t.Fatalf("desktoptest: page evaluation in %s failed: %v", nw.ID(), err)
	}
	return r
}

// EvalQuiet is Eval with the error, for poll conditions.
func (nw *NativeWindow) EvalQuiet(body string) (json.RawMessage, error) { return nw.eval(body) }

// Text reads one element's text from this window's page.
func (nw *NativeWindow) Text(t TB, selector string) string {
	t.Helper()
	sel, _ := json.Marshal(selector)
	var s *string
	r := nw.Eval(t, fmt.Sprintf(`const el = document.querySelector(%s); return el ? el.textContent : null;`, sel))
	if err := json.Unmarshal(r, &s); err != nil || s == nil {
		t.Fatalf("desktoptest: no element matches %s in %s", selector, nw.ID())
	}
	return *s
}

// Snapshot captures this window's real pixels and decodes them.
func (nw *NativeWindow) Snapshot(t TB) image.Image {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), harnessEvalWait)
	defer cancel()
	pngBytes, err := nw.w.Snapshot(ctx)
	if err != nil {
		t.Fatalf("desktoptest: snapshot of %s: %v", nw.ID(), err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("desktoptest: snapshot of %s does not decode as PNG: %v", nw.ID(), err)
	}
	return img
}

// State reads the OS facts about this window.
func (nw *NativeWindow) State() (desktop.WindowState, error) {
	d, err := nw.h.driver()
	if err != nil {
		return desktop.WindowState{}, err
	}
	return d.WindowState(nw.ID())
}

// ----- shared plumbing ------------------------------------------------

// bootEnv prepares the process for a native run.
func bootEnv() {
	os.Setenv("GOFASTR_ISOLATION", "off")
	if os.Getenv(dataDirEnv) == "" {
		if dir, err := os.MkdirTemp("", "gofastr-desktop-native-*"); err == nil {
			os.Setenv(dataDirEnv, dir)
		}
	}
}

// nativeTimeout resolves the watchdog's budget.
func nativeTimeout() time.Duration {
	if s := os.Getenv(nativeTimeoutEnv); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Minute
}

// quitSafely ends the app from a lifecycle goroutine (after the tests,
// or the watchdog). A panic inside Quit means the teardown itself is
// broken; recovering it lets the run report its real result instead of
// dying mid-report.
func (h *NativeHarness) quitSafely() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "desktoptest: quitting the app panicked: %v\n", r)
		}
	}()
	h.Shell.Quit()
}

// runFailed reports whether Run's error is a real failure; the named
// unsupported error is the expected no-native-shell path.
func runFailed(err error) bool {
	if err == nil {
		return false
	}
	var de *desktop.Error
	if errors.As(err, &de) && de.Code == desktop.CodeUnsupported {
		return false
	}
	return true
}
