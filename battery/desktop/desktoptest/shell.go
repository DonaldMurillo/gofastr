// Package desktoptest is the no-native-code Shell double the desktop
// battery's own tests and app-level tests run against. It lives in its
// own package so an example app's test can wire the same fake into a
// desktop.Config and observe notifications, dialogs, and window evals
// without touching AppKit.
//
// The double is scriptable: each native surface returns a result the
// test sets beforehand (SetSaveFile, SetPromptDecisions, …) and records
// everything that crossed it (Notifications, Prompts, ClipboardWrites,
// the window's NavURLs and Evals) for assertions.
package desktoptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Shell is the fake desktop.Shell. Run records the WindowConfig, calls
// ready on a goroutine, and blocks until Quit (or the context ends).
// Safe for concurrent use.
type Shell struct {
	mu sync.Mutex

	runCalled bool
	runCfg    desktop.WindowConfig
	runErr    error

	quitCh   chan struct{}
	quitOnce sync.Once

	promptRequests  []desktop.PermissionRequest
	promptDecisions []desktop.Decision
	promptErr       error

	clipText   string
	clipReads  int
	clipWrites []string

	openFileResult []string
	openFileErr    error
	saveFileResult string
	saveFileErr    error
	folderResult   string
	folderErr      error
	dialogCalls    int

	// Secondary windows (OpenWindow): recorded calls and per-id state.
	openWindowCalls []OpenWindowCall
	openWindows     map[string]*Window
	nextWindowNum   int

	// Tray: SetTrayTitle's latest value.
	trayTitle string
	// settingsFires counts FireOnSettings activations.
	settingsFires int
	// appearance is the value SetReduceTransparency installed.
	appearance    desktop.Appearance
	notifications []desktop.Notification
	notifyErr     error
}

// NewShell returns an unscripted Shell.
func NewShell() *Shell {
	return &Shell{quitCh: make(chan struct{})}
}

// Run implements desktop.Shell.
func (f *Shell) Run(ctx context.Context, w desktop.WindowConfig, ready func(desktop.Window)) error {
	f.mu.Lock()
	if f.runCalled {
		f.mu.Unlock()
		return fmt.Errorf("desktoptest: Run called twice")
	}
	f.runCalled = true
	f.runCfg = w
	if f.openWindows == nil {
		f.openWindows = make(map[string]*Window)
	}
	win := &Window{shell: f, id: "main", title: w.Title, sidebarWidth: w.SidebarWidth}
	f.openWindows["main"] = win
	f.mu.Unlock()

	go ready(win)

	select {
	case <-f.quitCh:
		return f.runErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OpenWindowCall records one OpenWindow the shell received.
type OpenWindowCall struct {
	ID   string
	Spec desktop.WindowSpec
	URL  string
}

// OpenWindow implements desktop.Shell: a recorded window per id.
func (f *Shell) OpenWindow(id string, spec desktop.WindowSpec, url string) (desktop.Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openWindowCalls = append(f.openWindowCalls, OpenWindowCall{ID: id, Spec: spec, URL: url})
	if w, ok := f.openWindows[id]; ok {
		return w, nil
	}
	w := &Window{shell: f, id: id, title: spec.Title, sidebarWidth: spec.SidebarWidth}
	f.openWindows[id] = w
	return w, nil
}
func (f *Shell) SetTrayTitle(title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trayTitle = title
	return nil
}

// MoveWindow plays the user dragging window id to frame f: the fake
// window's frame changes and WindowConfig.OnWindowFrame fires, on a
// goroutine, the way the native window delegate reports a move.
func (f *Shell) MoveWindow(id string, fr desktop.Frame) {
	f.mu.Lock()
	w := f.openWindows[id]
	cb := f.runCfg.OnWindowFrame
	f.mu.Unlock()
	if w == nil {
		return
	}
	w.setFrameUser(fr)
	if cb != nil {
		go cb(id, fr)
	}
}

// Appearance implements desktop.Shell: the value SetReduceTransparency
// last installed (the zero value until then).
func (f *Shell) Appearance() desktop.Appearance {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appearance
}

// SetReduceTransparency plays the user flipping the accessibility
// setting: Appearance() changes and WindowConfig.OnAppearance fires on
// a goroutine, the way the NSWorkspace observer reports on darwin.
func (f *Shell) SetReduceTransparency(on bool) {
	f.mu.Lock()
	ap := desktop.Appearance{ReduceTransparency: on}
	f.appearance = ap
	cb := f.runCfg.OnAppearance
	f.mu.Unlock()
	if cb != nil {
		go cb(ap)
	}
}

// FocusWindow plays the OS making window id key: the frame changes and
// WindowConfig.OnWindowFocus fires on a goroutine, the way
// windowDidBecomeKey: reports.
func (f *Shell) FocusWindow(id string) { f.fireWindowActivity(id, true) }

// BlurWindow plays the window resigning key (windowDidResignKey:).
func (f *Shell) BlurWindow(id string) { f.fireWindowActivity(id, false) }

// fireWindowActivity snapshots the callback under the lock and fires
// it on a goroutine; nil ids fire nothing (no such window).
func (f *Shell) fireWindowActivity(id string, focus bool) {
	f.mu.Lock()
	w := f.openWindows[id]
	focusCB, blurCB := f.runCfg.OnWindowFocus, f.runCfg.OnWindowBlur
	f.mu.Unlock()
	if w == nil {
		return
	}
	if focus && focusCB != nil {
		go focusCB(id)
	}
	if !focus && blurCB != nil {
		go blurCB(id)
	}
}

// SidebarWidthOf returns the sidebar zone window id carries right now
// (0 when there is no such window), the fact the darwin WindowState
// reads off the live zone view.
func (f *Shell) SidebarWidthOf(id string) int {
	f.mu.Lock()
	w := f.openWindows[id]
	f.mu.Unlock()
	if w == nil {
		return 0
	}
	return w.SidebarWidth()
}

// FireOnSettings invokes the WindowConfig.OnSettings callback the
// battery installed (the native shells fire it from the app menu's
// Settings item and any RoleSettings row).
func (f *Shell) FireOnSettings() {
	f.mu.Lock()
	onSettings := f.runCfg.OnSettings
	f.settingsFires++
	f.mu.Unlock()
	if onSettings != nil {
		onSettings()
	}
}

// Quit implements desktop.Shell.
func (f *Shell) Quit() { f.quitOnce.Do(func() { close(f.quitCh) }) }

// Main implements desktop.Shell: no UI thread exists, so fn runs inline.
func (f *Shell) Main(fn func()) error {
	fn()
	return nil
}

// Prompt implements desktop.Shell. With no scripted decisions it denies.
func (f *Shell) Prompt(ctx context.Context, req desktop.PermissionRequest) (desktop.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptRequests = append(f.promptRequests, req)
	if f.promptErr != nil {
		return desktop.DecisionDeny, f.promptErr
	}
	if len(f.promptDecisions) == 0 {
		return desktop.DecisionDeny, nil
	}
	d := f.promptDecisions[0]
	f.promptDecisions = f.promptDecisions[1:]
	return d, nil
}

// Clipboard implements desktop.Shell (the Shell is every surface).
func (f *Shell) Clipboard() desktop.Clipboard { return f }

// Dialogs implements desktop.Shell.
func (f *Shell) Dialogs() desktop.Dialogs { return f }

// Notifier implements desktop.Shell.
func (f *Shell) Notifier() desktop.Notifier { return f }

// ReadText implements desktop.Clipboard.
func (f *Shell) ReadText(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipReads++
	return f.clipText, nil
}

// WriteText implements desktop.Clipboard.
func (f *Shell) WriteText(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipWrites = append(f.clipWrites, text)
	return nil
}

// OpenFile implements desktop.Dialogs.
func (f *Shell) OpenFile(_ context.Context, _ desktop.OpenOptions) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.openFileResult, f.openFileErr
}

// SaveFile implements desktop.Dialogs.
func (f *Shell) SaveFile(_ context.Context, _ desktop.SaveOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.saveFileResult, f.saveFileErr
}

// OpenFolder implements desktop.Dialogs.
func (f *Shell) OpenFolder(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.folderResult, f.folderErr
}

// Show implements desktop.Notifier.
func (f *Shell) Show(_ context.Context, n desktop.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notifyErr != nil {
		return f.notifyErr
	}
	f.notifications = append(f.notifications, n)
	return nil
}

// Compile-time interface checks.
var (
	_ desktop.Shell     = (*Shell)(nil)
	_ desktop.Window    = (*Window)(nil)
	_ desktop.Clipboard = (*Shell)(nil)
	_ desktop.Dialogs   = (*Shell)(nil)
	_ desktop.Notifier  = (*Shell)(nil)
)

// ----- scripting -----------------------------------------------------------

// Window returns the fake window with the given id ("main" for the one
// Run created, else the id OpenWindow received), or nil.
func (f *Shell) Window(id string) *Window {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openWindows[id]
}

// OpenWindowCalls returns every OpenWindow the shell received, in order.
func (f *Shell) OpenWindowCalls() []OpenWindowCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OpenWindowCall{}, f.openWindowCalls...)
}

// TrayTitle returns the last title SetTrayTitle received.
func (f *Shell) TrayTitle() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.trayTitle
}

// SettingsFires counts FireOnSettings activations.
func (f *Shell) SettingsFires() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settingsFires
}

// SetRunErr makes the next Run return err.
func (f *Shell) SetRunErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runErr = err
}

// SetPromptDecisions queues permission-prompt answers, consumed in order.
func (f *Shell) SetPromptDecisions(ds ...desktop.Decision) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptDecisions = append(f.promptDecisions, ds...)
}

// SetPromptErr makes Prompt return err (and records the request).
func (f *Shell) SetPromptErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptErr = err
}

// SetClipboardText is what ReadText returns.
func (f *Shell) SetClipboardText(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipText = s
}

// SetOpenFile scripts the open-file dialog.
func (f *Shell) SetOpenFile(paths []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openFileResult, f.openFileErr = paths, err
}

// SetSaveFile scripts the save-file dialog.
func (f *Shell) SetSaveFile(path string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveFileResult, f.saveFileErr = path, err
}

// SetOpenFolder scripts the folder dialog.
func (f *Shell) SetOpenFolder(path string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.folderResult, f.folderErr = path, err
}

// SetNotifyErr makes Show return err instead of recording.
func (f *Shell) SetNotifyErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifyErr = err
}

// ----- assertions ----------------------------------------------------------

// Prompts returns every PermissionRequest Prompt saw.
func (f *Shell) Prompts() []desktop.PermissionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]desktop.PermissionRequest{}, f.promptRequests...)
}

// Config returns the WindowConfig Run received and whether Run ran.
func (f *Shell) Config() (desktop.WindowConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runCfg, f.runCalled
}

// Notifications returns every Notification Show accepted.
func (f *Shell) Notifications() []desktop.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]desktop.Notification{}, f.notifications...)
}

// ClipboardWrites returns every text WriteText received.
func (f *Shell) ClipboardWrites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.clipWrites...)
}

// ClipboardReads counts ReadText calls.
func (f *Shell) ClipboardReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clipReads
}

// DialogCalls counts dialog method calls.
func (f *Shell) DialogCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dialogCalls
}
