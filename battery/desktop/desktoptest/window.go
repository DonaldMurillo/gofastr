package desktoptest

import (
	"context"
	"encoding/base64"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Window is the fake desktop.Window Run hands the battery: it records
// Navigate/Eval/SetTitle/Focus and answers a fixed 1x1 PNG from
// Snapshot. ID is "main" for the window Run created, then the id
// OpenWindow received.
type Window struct {
	mu        sync.Mutex
	shell     *Shell
	id        string
	title     string
	navigated []string
	evaluated []string
	titles    []string
	focuses   int
	closed    bool
	hidden    bool
	// evalHook, when set, receives every Eval after it is recorded: the
	// browser-backed harness forwards native evals into a real page.
	evalHook func(js string) error
	// frame is the window's current frame; setFrames records every
	// SetFrame (MoveWindow routes through the shell, which fires
	// OnWindowFrame the way the native delegate does).
	frame     desktop.Frame
	setFrames []desktop.Frame
	// sidebarWidth is the sidebar zone the window carries: the
	// WindowConfig/WindowSpec value at creation, then whatever
	// SetSidebarWidth reported.
	sidebarWidth int
}

// SetSidebarWidth implements desktop.Window: the zone the page
// reported through window.setChrome, recorded for SidebarWidthOf.
func (w *Window) SetSidebarWidth(points int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sidebarWidth = points
	return nil
}

// SidebarWidth returns the window's current sidebar zone in points.
func (w *Window) SidebarWidth() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sidebarWidth
}

// ID implements desktop.Window.
func (w *Window) ID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

// Focus implements desktop.Window. A hidden main window (the tray's
// CloseHidesWindow flow) becomes visible again, as on the native
// shells.
func (w *Window) Focus() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.focuses++
	w.hidden = false
	return nil
}

// NewWindow returns a standalone Window with the given starting title,
// for tests that exercise Window methods without running the Shell.
func NewWindow(title string) *Window {
	return &Window{title: title}
}

// Navigate implements desktop.Window.
func (w *Window) Navigate(url string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.navigated = append(w.navigated, url)
	return nil
}

// Eval implements desktop.Window: the script is recorded, then handed
// to the eval hook when one is set.
func (w *Window) Eval(js string) error {
	w.mu.Lock()
	w.evaluated = append(w.evaluated, js)
	hook := w.evalHook
	w.mu.Unlock()
	if hook != nil {
		return hook(js)
	}
	return nil
}

// SetEvalHook forwards every later Eval to fn after recording it. A
// harness that drives a real browser installs one so native events
// (Battery.Emit) and menu navigations reach the live page; fn's error
// is what Eval returns.
func (w *Window) SetEvalHook(fn func(js string) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evalHook = fn
}

// png1x1 is a fixed valid 1x1 transparent PNG.
var png1x1 = func() []byte {
	const s = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("bad test PNG fixture: " + err.Error())
	}
	return b
}()

// PNG1x1 is the fixed PNG bytes Snapshot returns, exported so a test can
// compare what a snapshot round-trip delivered.
func PNG1x1() []byte { return png1x1 }

// Snapshot implements desktop.Window.
func (w *Window) Snapshot(context.Context) ([]byte, error) { return png1x1, nil }

// Title implements desktop.Window.
func (w *Window) Title() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.title
}

// SetTitle implements desktop.Window.
func (w *Window) SetTitle(t string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.title = t
	w.titles = append(w.titles, t)
	return nil
}

// Native implements desktop.Window; the fake has no native handle.
func (w *Window) Native() uintptr { return 0 }

// Frame implements desktop.Window: the recorded frame (zero before the
// first SetFrame or MoveWindow).
func (w *Window) Frame() (desktop.Frame, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frame, nil
}

// SetFrame implements desktop.Window: the frame is recorded, and
// fireFrame lets the shell report it the way the native delegate does.
func (w *Window) SetFrame(f desktop.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frame = f
	w.setFrames = append(w.setFrames, f)
	return nil
}

// setFrameUser is MoveWindow's half: the frame changes without a
// recorded SetFrame call, because a drag is not a SetFrame.
func (w *Window) setFrameUser(f desktop.Frame) {
	w.mu.Lock()
	w.frame = f
	w.mu.Unlock()
}

// SetFrames returns every SetFrame call, in order.
func (w *Window) SetFrames() []desktop.Frame {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]desktop.Frame{}, w.setFrames...)
}

// Close implements desktop.Window.
func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

// NavURLs returns every URL Navigate received.
func (w *Window) NavURLs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.navigated...)
}

// Evals returns every script Eval received.
func (w *Window) Evals() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.evaluated...)
}

// Events returns every native event dispatched to THIS window's page,
// in order, parsed out of the evals the battery issued (Harness.Events
// is the main window's slice).
func (w *Window) Events() []Event {
	var out []Event
	for _, js := range w.Evals() {
		if ev, ok := ParseEvent(js); ok {
			out = append(out, ev)
		}
	}
	return out
}

// Focuses counts Focus calls.
func (w *Window) Focuses() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.focuses
}

// Titles returns every title SetTitle received.
func (w *Window) Titles() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.titles...)
}

// Closed reports whether Close ran.
func (w *Window) Closed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// Hidden reports whether the window is hidden (the main window after
// the user closed it under Tray.CloseHidesWindow). Focus clears it.
func (w *Window) Hidden() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hidden
}

// hide marks the window hidden, what the native shells do on the main
// window's close button when the tray keeps the app alive.
func (w *Window) hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hidden = true
}
