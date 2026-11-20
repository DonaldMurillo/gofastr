package desktoptest

import (
	"context"
	"encoding/base64"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Window is the fake desktop.Window Run hands the battery: it records Navigate/Eval/SetTitle/Focus and answers a fixed 1x1 PNG from Snapshot.
type Window struct {
	mu           sync.Mutex
	shell        *Shell
	id           string
	title        string
	navigated    []string
	evaluated    []string
	titles       []string
	focuses      int
	closed       bool
	hidden       bool
	evalHook     func(js string) error
	frame        desktop.Frame
	setFrames    []desktop.Frame
	sidebarWidth int
}

// SetSidebarWidth implements desktop.Window: the zone the page reported through window.setChrome, recorded for SidebarWidthOf.
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

func (w *Window) ID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

func (w *Window) Focus() error {
	w.mu.Lock()
	w.focuses++
	w.hidden = false
	shell, id := w.shell, w.id
	w.mu.Unlock()
	if shell != nil {
		shell.FocusWindow(id)
	}
	return nil
}

// NewWindow returns a standalone Window with the given starting title, for tests that exercise Window methods without running the Shell.
func NewWindow(title string) *Window {
	return &Window{title: title}
}

func (w *Window) Navigate(url string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.navigated = append(w.navigated, url)
	return nil
}

// Eval implements desktop.Window: the script is recorded, then handed to the eval hook when one is set.
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

// SetEvalHook forwards every later Eval to fn after recording it.
func (w *Window) SetEvalHook(fn func(js string) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evalHook = fn
}

var png1x1 = func() []byte {
	const s = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("bad test PNG fixture: " + err.Error())
	}
	return b
}()

// PNG1x1 is the fixed PNG bytes Snapshot returns, exported so a test can compare what a snapshot round-trip delivered.
func PNG1x1() []byte { return png1x1 }

func (w *Window) Snapshot(context.Context) ([]byte, error) { return png1x1, nil }

func (w *Window) Title() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.title
}

func (w *Window) SetTitle(t string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.title = t
	w.titles = append(w.titles, t)
	return nil
}

// Native implements desktop.Window; the fake has no native handle.
func (w *Window) Native() uintptr { return 0 }

// Frame implements desktop.Window: the recorded frame (zero before the first SetFrame or MoveWindow).
func (w *Window) Frame() (desktop.Frame, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frame, nil
}

// SetFrame implements desktop.Window: the frame is recorded, and fireFrame lets the shell report it the way the native delegate does.
func (w *Window) SetFrame(f desktop.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frame = f
	w.setFrames = append(w.setFrames, f)
	return nil
}

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

// Events returns every native event dispatched to THIS window's page, in order, parsed out of the evals the battery issued (Harness.Events is the main window's slice).
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
	return w.IsClosed()
}

// IsClosed reports whether Close ran, including to the battery's stale-callback guard.
func (w *Window) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// Hidden reports whether the window is hidden (the main window after the user closed it under Tray.CloseHidesWindow).
func (w *Window) Hidden() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hidden
}

func (w *Window) hide() {
	w.mu.Lock()
	w.hidden = true
	shell, id := w.shell, w.id
	w.mu.Unlock()
	if shell != nil {
		shell.BlurWindow(id)
	}
}
