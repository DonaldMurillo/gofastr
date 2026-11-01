//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const mainTimeout = 15 * time.Second

// GTK has one process-global default main context, so one shell runs at a
// time. A shell holds this from Run's start until its cleanup finishes; the
// next shell lifetime can then run on the same toolkit thread.
var toolkitOwner atomic.Pointer[linuxShell]

var errToolkitBusy = errors.New("desktop/linux: another GTK shell is running in this process")
var nativeWindows sync.Map // GtkWindow pointer -> *linuxWindow
var workSequence atomic.Uint64
var mainJobs sync.Map // integer token -> *mainJob; no Go pointers escape to C

type mainJob struct {
	shell *linuxShell
	fn    func()
	done  chan struct{}
	state atomic.Int32 // 0 queued, 1 running, 2 cancelled
}

var idleCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	value, ok := mainJobs.LoadAndDelete(a.Int[0])
	if !ok {
		return 0
	}
	job := value.(*mainJob)
	if job.state.CompareAndSwap(0, 1) {
		if job.shell.running.Load() {
			job.fn()
		}
		close(job.done)
	}
	return 0 // G_SOURCE_REMOVE
})

var destroyCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if value, ok := nativeWindows.LoadAndDelete(a.Int[0]); ok {
		w := value.(*linuxWindow)
		w.closed.Store(true)
		s := w.shell
		s.mu.Lock()
		delete(s.windows, w.id)
		s.mu.Unlock()
		w.releaseMenuActions()
		if w.id == desktop.MainWindowID {
			s.quitting.Store(true)
			if s.inLoop && call(gtk.Sym.GtkMainLevel) != 0 {
				call(gtk.Sym.GtkMainQuit)
			}
		} else if cb := s.cfg.OnWindowClosed; cb != nil {
			go cb(w.id)
		}
	}
	return 0
})

var configureCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if value, ok := nativeWindows.Load(a.Int[0]); ok {
		w := value.(*linuxWindow)
		if cb := w.shell.cfg.OnWindowFrame; cb != nil {
			f := w.frameMain()
			go cb(w.id, f)
		}
	}
	return 0
})

var focusCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if value, ok := nativeWindows.Load(a.Int[0]); ok {
		w := value.(*linuxWindow)
		cb := w.shell.cfg.OnWindowFocus
		if a.Int[2] != 0 {
			cb = w.shell.cfg.OnWindowBlur
		}
		if cb != nil {
			go cb(w.id)
		}
	}
	return 0
})

// decidePolicyCallback is a web view's "decide-policy" handler. A
// navigation action carries no main-frame flag in WebKitGTK, so the
// check runs on the response: a refused main-frame document is ignored
// before it commits. The request has reached the other host by then,
// but nothing it returns renders in the window. Subframes load as on
// the other shells. A panic refuses.
var decidePolicyCallback = ffi.NewCallback(func(a *ffi.Args) (handled uintptr) {
	decision := a.Int[1]
	defer func() {
		if v := recover(); v != nil {
			slog.Default().Error("desktop/linux: navigation policy panicked", "panic", textsafe.Recovered(v))
			call(gtk.Sym.WebkitPolicyDecisionIgnore, decision)
			handled = 1
		}
	}()
	if a.Int[2] != 2 /* WEBKIT_POLICY_DECISION_TYPE_RESPONSE */ ||
		call(gtk.Sym.WebkitResponsePolicyDecisionIsMainFrameMainResource, decision)&0xff == 0 {
		return 0
	}
	s := toolkitOwner.Load()
	if s == nil || s.cfg.AllowNavigation == nil {
		return 0
	}
	response := call(gtk.Sym.WebkitResponsePolicyDecisionGetResponse, decision)
	raw := capsGoString(call(gtk.Sym.WebkitURIResponseGetURI, response))
	if s.cfg.AllowNavigation(raw) {
		return 0
	}
	slog.Default().Warn("desktop/linux: refused a navigation off the app's origin", "host", desktop.NavigationHost(raw))
	call(gtk.Sym.WebkitPolicyDecisionIgnore, decision)
	return 1
})

type linuxShell struct {
	desktop.Shell // intentionally unsupported optional surfaces
	mu            sync.RWMutex
	started       bool
	running       atomic.Bool
	quitting      atomic.Bool
	uiThread      atomic.Int64
	done          chan struct{}
	cfg           desktop.WindowConfig
	textCtx       context.Context
	windows       map[string]*linuxWindow
	webContext    uintptr
	activation    *activationBroker
	profile       *browserProfile
	inLoop        bool // UI thread only
	capGate       chan struct{}
	promptQueue   []desktop.Decision
}

type linuxWindow struct {
	shell      *linuxShell
	id         string
	handle     uintptr
	webview    uintptr
	closed     atomic.Bool
	titleMu    sync.RWMutex
	title      string
	menuTokens []uintptr // UI thread only
	effect     windowEffect
	press      buttonPress // UI thread only
}

// New constructs a lazy, CGO-free GTK shell. No libraries are loaded here.
func New() desktop.Shell {
	return &linuxShell{Shell: desktop.NewUnsupportedShell(), done: make(chan struct{}), windows: make(map[string]*linuxWindow)}
}

func (s *linuxShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("desktop/linux: Run called twice")
	}
	s.started = true
	s.cfg = cfg
	s.mu.Unlock()
	// Registered first so it runs last: ownership passes to the next shell
	// only after this one's profile, launch and done channel are released.
	owned := false
	defer func() {
		if owned {
			toolkitOwner.CompareAndSwap(s, nil)
		}
	}()
	defer close(s.done)
	defer s.ReleaseLaunch()
	defer s.ReleaseProfile()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Linux's desktop language preferences come from LANGUAGE/LC_*/LANG.
	s.textCtx = desktop.ShellContext(ctx, nil)
	if s.quitting.Load() {
		return nil
	}
	if !toolkitOwner.CompareAndSwap(nil, s) {
		return errToolkitBusy
	}
	owned = true
	var err error
	if terr := onToolkitThread(func() { err = s.runToolkit(ctx, cfg, ready) }); terr != nil {
		return terr
	}
	return err
}

// runToolkit is one shell lifetime on the toolkit thread: GTK init (a no-op
// after the first lifetime), a fresh web context, the main window, the main
// loop, and teardown of everything this lifetime created.
func (s *linuxShell) runToolkit(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	s.uiThread.Store(int64(syscall.Gettid()))
	defer s.uiThread.Store(0)
	if err := gtk.Load(); err != nil {
		return err
	}
	if call(gtk.Sym.GtkInitCheck, 0, 0) == 0 {
		return errors.New("desktop/linux: GTK cannot open a display; run in an X11 or Wayland desktop session")
	}
	webContext, err := s.createWebContext(ctx)
	if err != nil {
		return err
	}
	s.webContext = webContext
	defer call(gtk.Sym.GObjectUnref, s.webContext)
	s.running.Store(true)
	defer func() {
		s.closeCapabilities()
		s.mu.RLock()
		windows := make([]*linuxWindow, 0, len(s.windows))
		for _, w := range s.windows {
			windows = append(windows, w)
		}
		s.mu.RUnlock()
		for _, w := range windows {
			if !w.closed.Load() {
				call(gtk.Sym.GtkWidgetDestroy, w.handle)
			}
		}
		s.running.Store(false)
		cancelShellJobs(s)
		drainToolkitEvents()
	}()
	w, err := s.createWindow(desktop.MainWindowID, desktop.WindowSpec{Title: cfg.Title, Width: cfg.Width, Height: cfg.Height, Style: cfg.Style, Frame: cfg.Frame})
	if err != nil {
		return err
	}
	if err := startLinuxTray(w); err != nil {
		slog.Default().Warn("desktop/linux: tray unavailable; keeping normal window close behavior", "error", err)
	}
	// Remove native endpoints before destroying their window or web context.
	defer stopLinuxTray(s)
	go func() {
		select {
		case <-ctx.Done():
			s.Quit()
		case <-s.done:
		}
	}()
	if ready != nil {
		ready(w)
	}
	s.attachActivation(cfg.OnDeepLink, func() { _ = w.Focus() })
	if !s.quitting.Load() {
		s.inLoop = true
		call(gtk.Sym.GtkMain)
		s.inLoop = false
	}
	return nil
}

// drainToolkitEvents dispatches what is already pending on the default main
// context after a lifetime's windows are destroyed: unmap and destroy events,
// cancelled idle jobs, and WebKit's own teardown sources. It never blocks, so
// the next lifetime starts from a quiet context instead of inheriting this
// one's backlog.
func drainToolkitEvents() {
	for i := 0; i < 1000 && call(gtk.Sym.GtkEventsPending)&0xff != 0; i++ {
		call(gtk.Sym.GtkMainIterationDo, 0)
	}
}

func cancelShellJobs(s *linuxShell) {
	mainJobs.Range(func(key, value any) bool {
		job := value.(*mainJob)
		if job.shell == s {
			mainJobs.Delete(key)
			if job.state.CompareAndSwap(0, 2) {
				close(job.done)
			}
		}
		return true
	})
}

// Main dispatches via the default GTK context. Expired work is removed before
// it can execute, so a timed-out call cannot mutate the window later.
func (s *linuxShell) Main(fn func()) error {
	if fn == nil {
		return nil
	}
	if !s.running.Load() {
		return errStopped()
	}
	if s.uiThread.Load() == int64(syscall.Gettid()) {
		fn()
		return nil
	}
	job := &mainJob{shell: s, fn: fn, done: make(chan struct{})}
	id := uintptr(workSequence.Add(1))
	mainJobs.Store(id, job)
	source := call(gtk.Sym.GIdleAdd, idleCallback, id)
	if source == 0 {
		mainJobs.Delete(id)
		return errors.New("desktop/linux: cannot dispatch to GTK")
	}
	timer := time.NewTimer(mainTimeout)
	defer timer.Stop()
	select {
	case <-job.done:
		if job.state.Load() == 2 || !s.running.Load() {
			return errStopped()
		}
		return nil
	case <-s.done:
	case <-timer.C:
	}
	if job.state.CompareAndSwap(0, 2) {
		mainJobs.Delete(id)
		call(gtk.Sym.GSourceRemove, source)
		return errors.New("desktop/linux: GTK dispatch timed out or stopped")
	}
	// A callback already running cannot be preempted. Return a bounded error;
	// callers must discard callback results on dispatch failure.
	return errors.New("desktop/linux: GTK operation timed out or stopped")
}

// Quit is never queued through timeout-cancellable Main: a busy UI must not
// lose the only shutdown request when it later becomes responsive again.
var quitCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if s := toolkitOwner.Load(); s != nil && s.running.Load() && s.quitting.Load() && s.inLoop && call(gtk.Sym.GtkMainLevel) != 0 {
		call(gtk.Sym.GtkMainQuit)
	}
	return 0
})

func (s *linuxShell) Quit() {
	s.quitting.Store(true)
	if !s.running.Load() {
		return
	}
	if s.uiThread.Load() == int64(syscall.Gettid()) {
		if s.inLoop && call(gtk.Sym.GtkMainLevel) != 0 {
			call(gtk.Sym.GtkMainQuit)
		}
		return
	}
	call(gtk.Sym.GIdleAdd, quitCallback, 0)
}

func (s *linuxShell) SetTrayTitle(title string) error {
	return setLinuxTrayTitle(s, title)
}

func (s *linuxShell) OpenWindow(id string, spec desktop.WindowSpec, url string) (desktop.Window, error) {
	var w *linuxWindow
	var err error
	if id == "" || id == desktop.MainWindowID {
		return nil, errors.New("desktop/linux: invalid secondary window id")
	}
	if e := s.Main(func() {
		s.mu.RLock()
		_, exists := s.windows[id]
		s.mu.RUnlock()
		if exists {
			err = errors.New("desktop/linux: duplicate window id")
			return
		}
		w, err = s.createWindow(id, spec)
		if err == nil {
			err = w.Navigate(url)
		}
		if err != nil && w != nil {
			call(gtk.Sym.GtkWidgetDestroy, w.handle)
		}
	}); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *linuxShell) createWindow(id string, spec desktop.WindowSpec) (*linuxWindow, error) {
	if err := validateString(spec.Title); err != nil {
		return nil, err
	}
	width, height := spec.Width, spec.Height
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}
	title := spec.Title
	if title == "" {
		title = s.cfg.Title
	}
	handle := call(gtk.Sym.GtkWindowNew, 0)
	if handle == 0 {
		return nil, errors.New("desktop/linux: create GTK window")
	}
	webview := call(gtk.Sym.WebkitWebViewNewWithContext, s.webContext)
	if webview == 0 {
		call(gtk.Sym.GtkWidgetDestroy, handle)
		return nil, errors.New("desktop/linux: create WebKit view")
	}
	w := &linuxWindow{shell: s, id: id, handle: handle, webview: webview, title: title}
	// Bootstrap runs only in the top-level frame, before any application script.
	withString(desktop.BootstrapJS(id, false), func(p uintptr) {
		script := call(gtk.Sym.WebkitUserScriptNew, p, 1 /* TOP_FRAME */, 0 /* START */, 0, 0)
		manager := call(gtk.Sym.WebkitWebViewGetUCM, webview)
		call(gtk.Sym.WebkitUCMAddScript, manager, script)
		call(gtk.Sym.WebkitUserScriptUnref, script)
	})
	withString(title, func(p uintptr) { call(gtk.Sym.GtkWindowSetTitle, handle, p) })
	call(gtk.Sym.GtkWindowSetDefaultSize, handle, uintptr(width), uintptr(height))
	call(gtk.Sym.GtkWindowSetPosition, handle, 1 /* CENTER */)
	if spec.Style.Resizable != nil {
		call(gtk.Sym.GtkWindowSetResizable, handle, boolean(*spec.Style.Resizable))
	}
	if spec.Style.Chrome == desktop.ChromeNone {
		call(gtk.Sym.GtkWindowSetDecorated, handle, 0)
	}
	if spec.Style.Float || spec.Style.Panel {
		call(gtk.Sym.GtkWindowSetKeepAbove, handle, 1)
	}
	if spec.Style.Panel {
		call(gtk.Sym.GtkWindowSetAcceptFocus, handle, 0)
	}
	if spec.Style.AllSpaces {
		call(gtk.Sym.GtkWindowStick, handle)
	}
	if spec.Frame != nil {
		w.setFrameMain(*spec.Frame)
	} else if spec.Style.X != nil && spec.Style.Y != nil {
		call(gtk.Sym.GtkWindowMove, handle, uintptr(*spec.Style.X), uintptr(*spec.Style.Y))
	}
	box := call(gtk.Sym.GtkBoxNew, 1 /* VERTICAL */, 0)
	call(gtk.Sym.GtkContainerAdd, handle, box)
	if id == desktop.MainWindowID {
		w.installMenus(box)
	}
	call(gtk.Sym.GtkBoxPackStart, box, webview, 1, 1, 0)
	s.mu.Lock()
	s.windows[id] = w
	s.mu.Unlock()
	nativeWindows.Store(handle, w)
	connect(handle, "destroy", destroyCallback, 0)
	connect(handle, "configure-event", configureCallback, 0)
	connect(handle, "focus-in-event", focusCallback, 0)
	connect(handle, "focus-out-event", focusCallback, 1)
	connect(webview, "decide-policy", decidePolicyCallback, 0)
	w.installDragHandler()
	w.applyEffectBeforeShow(spec.Style)
	call(gtk.Sym.GtkWidgetShowAll, handle)
	w.applyEffectAfterShow()
	return w, nil
}

func connect(object uintptr, name string, callback, data uintptr) {
	withString(name, func(p uintptr) { call(gtk.Sym.GSignalConnectData, object, p, callback, data, 0, 0) })
}
func call(fn uintptr, args ...uintptr) uintptr { r, _, _ := ffi.Call(fn, args, nil); return r }
func boolean(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}
func validateString(s string) error {
	if strings.IndexByte(s, 0) >= 0 {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "text contains NUL"}
	}
	return nil
}
func withString(s string, fn func(uintptr)) {
	b := make([]byte, len(s)+1)
	copy(b, s)
	// Native calls can reenter Go and grow its stack; pinning forces the
	// byte storage onto the nonmoving heap for the entire C call.
	var pin runtime.Pinner
	pin.Pin(&b[0])
	defer pin.Unpin()
	fn(uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
}
func readString(p uintptr) string {
	text, _ := readBoundedString(p, 4<<20)
	return text
}
func readBoundedString(p uintptr, limit int) (string, bool) {
	if p == 0 {
		return "", true
	}
	b := unsafe.Slice((*byte)(nativePointer(p)), limit)
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n]), n < len(b)
}

func errStopped() error {
	return &desktop.Error{Code: desktop.CodeCancelled, Message: "Linux desktop shell is not running"}
}
func errClosed() error {
	return &desktop.Error{Code: desktop.CodeNotFound, Message: "window is closed"}
}
func (w *linuxWindow) withMain(fn func()) error {
	var err error
	if w.closed.Load() {
		return errClosed()
	}
	e := w.shell.Main(func() {
		if w.closed.Load() {
			err = errClosed()
			return
		}
		fn()
	})
	if e != nil {
		return e
	}
	return err
}
func (w *linuxWindow) ID() string     { return w.id }
func (w *linuxWindow) IsClosed() bool { return w.closed.Load() }
func (w *linuxWindow) Native() uintptr {
	if w.closed.Load() {
		return 0
	}
	return w.webview
}
func (w *linuxWindow) Title() string { w.titleMu.RLock(); defer w.titleMu.RUnlock(); return w.title }
func (w *linuxWindow) SetTitle(title string) error {
	if err := validateString(title); err != nil {
		return err
	}
	return w.withMain(func() {
		withString(title, func(p uintptr) { call(gtk.Sym.GtkWindowSetTitle, w.handle, p) })
		w.titleMu.Lock()
		w.title = title
		w.titleMu.Unlock()
	})
}
func (w *linuxWindow) Navigate(url string) error {
	if err := validateString(url); err != nil {
		return err
	}
	return w.withMain(func() { withString(url, func(p uintptr) { call(gtk.Sym.WebkitWebViewLoadURI, w.webview, p) }) })
}
func (w *linuxWindow) Focus() error {
	return w.withMain(func() { call(gtk.Sym.GtkWindowPresent, w.handle) })
}
func (w *linuxWindow) Close() error {
	return w.withMain(func() { call(gtk.Sym.GtkWindowClose, w.handle) })
}
func (w *linuxWindow) SetSidebarWidth(points int) error {
	if points < 0 || points > desktop.MaxSidebarWidth {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "invalid sidebar width"}
	}
	if w.closed.Load() {
		return errClosed()
	}
	return nil // no compositor material on Linux
}
func (w *linuxWindow) Frame() (desktop.Frame, error) {
	var f desktop.Frame
	err := w.withMain(func() { f = w.frameMain() })
	if err != nil {
		return desktop.Frame{}, err
	}
	return f, nil
}
func (w *linuxWindow) frameMain() desktop.Frame {
	p := new([4]int32)
	var pin runtime.Pinner
	pin.Pin(p)
	defer pin.Unpin()
	call(gtk.Sym.GtkWindowGetPosition, w.handle, uintptr(unsafe.Pointer(&p[0])), uintptr(unsafe.Pointer(&p[1])))
	call(gtk.Sym.GtkWindowGetSize, w.handle, uintptr(unsafe.Pointer(&p[2])), uintptr(unsafe.Pointer(&p[3])))
	return desktop.Frame{X: int(p[0]), Y: int(p[1]), Width: int(p[2]), Height: int(p[3])}
}
func (w *linuxWindow) SetFrame(f desktop.Frame) error {
	if f.Width <= 0 || f.Height <= 0 || f.Width > 32767 || f.Height > 32767 {
		return fmt.Errorf("desktop/linux: invalid window frame")
	}
	return w.withMain(func() { w.setFrameMain(f) })
}
func (w *linuxWindow) setFrameMain(f desktop.Frame) {
	if f.Width > 0 && f.Height > 0 {
		call(gtk.Sym.GtkWindowResize, w.handle, uintptr(f.Width), uintptr(f.Height))
	}
	call(gtk.Sym.GtkWindowMove, w.handle, uintptr(f.X), uintptr(f.Y))
}

var _ desktop.Shell = (*linuxShell)(nil)
var _ desktop.Window = (*linuxWindow)(nil)

// Native addresses point into C-managed memory, never the Go heap. Reinterpret
// their bits without the uintptr-to-Go-pointer arithmetic forbidden by vet.
func nativePointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }
