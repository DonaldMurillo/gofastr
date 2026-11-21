//go:build darwin && (arm64 || amd64)

package macos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const evalTimeout = 15 * time.Second

const snapTimeout = 30 * time.Second

func deliverOnGoroutine(fn func()) {
	go func() {
		defer func() {
			if v := recover(); v != nil {
				if s := activeShell.Load(); s != nil && s.logger != nil {
					s.logger.Error("desktop: callback panicked", "panic", textsafe.Recovered(v))
				} else {
					slog.Default().Error("desktop: callback panicked", "panic", textsafe.Recovered(v))
				}
			}
		}()
		fn()
	}()
}

const (
	maskTitled              uintptr = 1 << 0
	maskClosable            uintptr = 1 << 1
	maskMiniaturizable      uintptr = 1 << 2
	maskResizable           uintptr = 1 << 3
	maskNonactivatingPanel  uintptr = 1 << 7
	maskFullSizeContentView uintptr = 1 << 15
)

const (
	normalWindowLevel   = 0
	floatingWindowLevel = 3
)

const behaviorCanJoinAllSpaces uintptr = 1 << 0

func windowStyleMask(style desktop.WindowStyle) uintptr {
	var mask uintptr
	switch style.Chrome {
	case desktop.ChromeNone:
	case desktop.ChromeHiddenTitle, desktop.ChromeUnified:
		mask = maskTitled | maskClosable | maskMiniaturizable | maskFullSizeContentView
	default:
		mask = maskTitled | maskClosable | maskMiniaturizable
	}
	if style.Chrome != desktop.ChromeNone {
		if style.Resizable == nil || *style.Resizable {
			mask |= maskResizable
		}
	}
	if style.Panel {
		mask |= maskNonactivatingPanel
	}
	return mask
}

func windowLevel(style desktop.WindowStyle) int {
	if style.Float || style.Panel {
		return floatingWindowLevel
	}
	return normalWindowLevel
}

func frameOrigin(style desktop.WindowStyle, height int, screenHeight float64) (x, y float64, ok bool) {
	if style.X == nil || style.Y == nil {
		return 0, 0, false
	}
	return float64(*style.X), screenHeight - float64(*style.Y) - float64(height), true
}

type windowStylePlacement struct {
	screens   func() []screenInfo
	setOrigin func(objc.ID, float64, float64)
}

func nativeStylePlacement() windowStylePlacement {
	return windowStylePlacement{screens: readScreens, setOrigin: func(window objc.ID, left, top float64) {
		objc.SendF(window, objc.Sel("setFrameOrigin:"), nil, []float64{left, top})
	}}
}

func applyWindowStyle(win, webView objc.ID, style desktop.WindowStyle, height int, placement windowStylePlacement) {
	if style.Chrome == desktop.ChromeHiddenTitle || style.Chrome == desktop.ChromeUnified {
		objc.Send(win, objc.Sel("setTitlebarAppearsTransparent:"), 1)
	}
	if level := windowLevel(style); level != normalWindowLevel {
		objc.Send(win, objc.Sel("setLevel:"), uintptr(level))
	}
	if style.Chrome == desktop.ChromeUnified {
		toolbar := objc.Send(objc.ID(objc.Send(objc.Class("NSToolbar"), objc.Sel("alloc"))), objc.Sel("init"))
		objc.Send(win, objc.Sel("setToolbar:"), uintptr(toolbar))
		objc.Send(win, objc.Sel("setToolbarStyle:"), toolbarStyleUnified)
	}
	if style.Chrome == desktop.ChromeHiddenTitle || style.Chrome == desktop.ChromeUnified {
		objc.Send(win, objc.Sel("setTitleVisibility:"), 1) // NSWindowTitleHidden
	}
	if style.Transparent {
		objc.Send(win, objc.Sel("setOpaque:"), 0)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
		setWebViewBackground(webView, false)
	}
	if style.AllSpaces {
		objc.Send(win, objc.Sel("setCollectionBehavior:"), behaviorCanJoinAllSpaces)
	}
	if x, y, ok := frameOrigin(style, height, screenFlipBase(placement.screens())); ok {
		placement.setOrigin(win, x, y)
	}
}

const (
	minVisibleFrameW = 100.0
	minVisibleFrameH = 50.0
)

type screenInfo struct {
	frame, visible objc.Rect
}

func readScreens() []screenInfo {
	list := objc.Send(objc.Class("NSScreen"), objc.Sel("screens"))
	if list == 0 {
		return nil
	}
	n := int(objc.Send(objc.ID(list), objc.Sel("count")))
	if n > 16 {
		n = 16 // sanity bound; nobody has more screens than that
	}
	out := make([]screenInfo, 0, n)
	for i := range n {
		sc := objc.ID(objc.Send(objc.ID(list), objc.Sel("objectAtIndex:"), uintptr(i)))
		out = append(out, screenInfo{
			frame:   objc.SendRectRet(sc, objc.Sel("frame")),
			visible: objc.SendRectRet(sc, objc.Sel("visibleFrame")),
		})
	}
	return out
}

func screenFlipBase(screens []screenInfo) float64 {
	if len(screens) > 0 {
		return screens[0].frame.H
	}
	return 0
}

func frameToRect(f desktop.Frame, screens []screenInfo) objc.Rect {
	base := screenFlipBase(screens)
	return objc.Rect{
		X: float64(f.X),
		Y: base - float64(f.Y) - float64(f.Height),
		W: float64(f.Width),
		H: float64(f.Height),
	}
}

func rectToFrame(r objc.Rect, screens []screenInfo) desktop.Frame {
	base := screenFlipBase(screens)
	return desktop.Frame{
		X:      int(math.Round(r.X)),
		Y:      int(math.Round(base - r.Y - r.H)),
		Width:  int(math.Round(r.W)),
		Height: int(math.Round(r.H)),
	}
}

func frameVisibleOnScreens(f desktop.Frame, screens []screenInfo) bool {
	if len(screens) == 0 || f.Width <= 0 || f.Height <= 0 {
		return false
	}
	r := frameToRect(f, screens)
	for _, s := range screens {
		w := math.Min(r.X+r.W, s.visible.X+s.visible.W) - math.Max(r.X, s.visible.X)
		h := math.Min(r.Y+r.H, s.visible.Y+s.visible.H) - math.Max(r.Y, s.visible.Y)
		if w >= minVisibleFrameW && h >= minVisibleFrameH {
			return true
		}
	}
	return false
}

func applyRememberedFrame(win objc.ID, f *desktop.Frame) {
	if f == nil {
		return
	}
	screens := readScreens()
	if frameVisibleOnScreens(*f, screens) {
		objc.SendRect(win, objc.Sel("setFrame:display:"), frameToRect(*f, screens), 1)
	} else {
		objc.Send(win, objc.Sel("center"))
	}
}

var activeShell atomic.Pointer[darwinShell]

type darwinShell struct {
	notifyMu           sync.Mutex
	notifyOnce         sync.Once
	notifyDelegateOnce sync.Once
	authBlock          uintptr
	addBlock           uintptr
	authRes            chan notifyResult
	addRes             chan notifyResult

	logger *slog.Logger

	mu                 sync.Mutex
	nsApp              objc.ID
	bridge             objc.ID
	window             objc.ID
	webView            objc.ID
	wkConfig           objc.ID
	title              string
	actionIDs          []string
	onMenu             func(id string)
	onSettings         func(context.Context)
	textCtx            context.Context
	menuNames          map[objc.ID]menuName
	onWindowClosed     func(id string)
	allowNavigation    func(url string) bool
	onWindowFrame      func(desktop.Window, desktop.Frame)
	onWindowFocus      func(id string)
	onWindowBlur       func(id string)
	onAppearance       func(desktop.Appearance)
	reduceTransparency atomic.Bool
	running            bool

	windows      map[objc.ID]*darwinWindow
	windowsByID  map[string]*darwinWindow
	windowsByUCC map[objc.ID]*darwinWindow
	closeHides   bool

	statusItem  objc.ID
	trayTitle   string
	promptQueue []desktop.Decision

	scriptMsgs chan string

	evalMu    sync.Mutex
	evalBlock uintptr
	evalRes   chan evalOutcome

	snapMu    sync.Mutex
	snapBlock uintptr
	snapRes   chan snapOutcome

	notifyLogMu sync.Mutex
	notifyLog   []desktop.Notification

	asyncMu    sync.Mutex
	asyncBlock uintptr
	asyncRes   chan evalOutcome
}

type evalOutcome struct {
	result  string
	errText string
}

type snapOutcome struct {
	png     []byte
	errText string
}

// New returns the macOS shell: AppKit + WKWebView on the pure-Go Objective-C bridge.
func New() desktop.Shell {
	return &darwinShell{logger: slog.Default()}
}

func (s *darwinShell) Main(fn func()) error { return s.onMain(fn) }

func (s *darwinShell) onMain(fn func()) error {
	return s.onMainWithTimeout(objc.DefaultMainTimeout, fn)
}

func (s *darwinShell) onMainWithTimeout(d time.Duration, fn func()) error {
	if objc.PthreadSelf() == objc.MainThreadID() {
		fn()
		return nil
	}
	return objc.MainWithTimeout(d, fn)
}

func (s *darwinShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	objc.AssertMainThread("darwinShell.Run")
	objc.OpenFrameworks()

	objc.Send(objc.ID(objc.Send(objc.Class("NSAutoreleasePool"), objc.Sel("alloc"))), objc.Sel("init"))
	nsApp := objc.ID(objc.Send(objc.Class("NSApplication"), objc.Sel("sharedApplication")))
	policy := uintptr(0)                      // Regular
	if cfg.Tray != nil && cfg.Tray.HideDock { // Accessory: no Dock icon
		policy = 1
	}
	objc.Send(nsApp, objc.Sel("setActivationPolicy:"), policy)

	bridge := objc.ID(objc.Send(objc.ID(objc.Send(ensureBridgeClass(), objc.Sel("alloc"))), objc.Sel("init")))
	installDeepLinkHandler(bridge, cfg.OnDeepLink)

	hasSettings := cfg.Settings != nil
	textCtx := desktop.ShellContext(ctx, preferredLanguages())
	plans, actionIDs := desktop.PlanMenuBar(textCtx, cfg.Title, cfg.Menu, hasSettings)
	trayRows := desktop.PlanTrayMenu(textCtx, cfg.Tray, cfg.Title, &actionIDs)
	evalBlock := objc.NewBlock(ffi.NewCallback(s.onEvalCompletion))
	snapBlock := objc.NewBlock(ffi.NewCallback(s.onSnapshotCompletion))
	asyncBlock := objc.NewBlock(ffi.NewCallback(s.onAsyncEvalCompletion))
	s.mu.Lock()
	s.nsApp = nsApp
	s.bridge = bridge
	s.title = cfg.Title
	s.onMenu = cfg.OnMenu
	s.onSettings = cfg.OnSettings
	s.textCtx = textCtx
	s.onWindowClosed = cfg.OnWindowClosed
	s.allowNavigation = cfg.AllowNavigation
	s.onWindowFrame = cfg.WindowFrameReporter()
	s.onWindowFocus = cfg.OnWindowFocus
	s.onWindowBlur = cfg.OnWindowBlur
	s.onAppearance = cfg.OnAppearance
	s.actionIDs = actionIDs
	s.closeHides = cfg.Tray != nil && cfg.Tray.CloseHidesWindow
	s.windows = make(map[objc.ID]*darwinWindow)
	s.windowsByID = make(map[string]*darwinWindow)
	s.windowsByUCC = make(map[objc.ID]*darwinWindow)
	s.scriptMsgs = make(chan string, 16)
	s.evalRes = make(chan evalOutcome, 1)
	s.snapRes = make(chan snapOutcome, 1)
	s.evalBlock = evalBlock
	s.snapBlock = snapBlock
	s.asyncRes = make(chan evalOutcome, 1)
	s.asyncBlock = asyncBlock
	s.running = true
	s.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			panic(r)
		}
	}()
	activeShell.Store(s)

	s.buildMenuBar(plans)

	config := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("WKWebViewConfiguration"), objc.Sel("alloc"))), objc.Sel("init")))
	ucc := objc.ID(objc.Send(config, objc.Sel("userContentController")))
	objc.Send(ucc, objc.Sel("addScriptMessageHandler:name:"), uintptr(bridge), uintptr(objc.NSString(bridgeMessageHandlerName)))

	s.readReduceTransparency()
	s.installAppearanceObserver()

	userScript := objc.Send(objc.ID(objc.Send(objc.Class("WKUserScript"), objc.Sel("alloc"))),
		objc.Sel("initWithSource:injectionTime:forMainFrameOnly:"),
		uintptr(objc.NSString(desktop.BootstrapJS(desktop.MainWindowID, s.reduceTransparency.Load()))), 0 /* atDocumentStart */, 1 /* main frame only */)
	objc.Send(ucc, objc.Sel("addUserScript:"), uintptr(userScript))

	webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:configuration:"), objc.Rect{W: float64(cfg.Width), H: float64(cfg.Height)}, uintptr(config)))
	windowClass := "NSWindow"
	if cfg.Style.Panel {
		windowClass = "NSPanel" // same init selector as NSWindow
	}
	window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class(windowClass), objc.Sel("alloc"))),
		objc.Sel("initWithContentRect:styleMask:backing:defer:"),
		objc.Rect{W: float64(cfg.Width), H: float64(cfg.Height)},
		windowStyleMask(cfg.Style), 2 /* buffered */, 0))
	objc.Send(window, objc.Sel("setTitle:"), uintptr(objc.NSString(cfg.Title)))
	objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
	objc.Send(window, objc.Sel("setDelegate:"), uintptr(bridge))
	objc.Send(webView, objc.Sel("setNavigationDelegate:"), uintptr(bridge))
	applyWindowStyle(window, webView, cfg.Style, cfg.Height, nativeStylePlacement())
	mainChrome := applyWindowMaterial(window, webView, cfg.Style, cfg.SidebarWidth, s.reduceTransparency.Load(), s.logger)
	framePlaced := cfg.Frame != nil
	if framePlaced {
		applyRememberedFrame(window, cfg.Frame)
	}
	main := &darwinWindow{shell: s, id: desktop.MainWindowID, window: window, webView: webView, ucc: ucc, title: cfg.Title, chrome: mainChrome}
	s.mu.Lock()
	s.window = window
	s.webView = webView
	s.wkConfig = config
	s.windows[window] = main
	s.windowsByID[main.id] = main
	s.windowsByUCC[ucc] = main
	s.mu.Unlock()

	order, activate := windowLaunchOrder(cfg.Style)
	front := func() {
		if win, _ := main.ids(); win == 0 {
			return
		}
		objc.Send(window, objc.Sel(order), 0)
		if !framePlaced && (cfg.Style.X == nil || cfg.Style.Y == nil) {
			objc.Send(window, objc.Sel("center"))
		}
		applyTrafficLightInset(window, cfg.Style.TrafficLightInset)
		if activate {
			objc.Send(nsApp, objc.Sel("activateIgnoringOtherApps:"), 1)
		}
	}
	if cfg.Style.Panel {
		go func() {
			if err := s.onMain(front); err != nil {
				s.logger.Warn("desktop: showing the main panel failed", "error", err)
			}
		}()
	} else {
		front()
	}

	if cfg.Tray != nil {
		s.installTray(cfg.Tray, trayRows)
	}

	ready(main)

	loopStopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.stopLoop()
		case <-loopStopped:
		}
	}()

	objc.Send(nsApp, objc.Sel("run")) // blocks until stopLoop fires

	close(loopStopped)
	s.mu.Lock()
	//gofastr:allow(deferreset) the reset follows the process-lifetime AppKit run loop on the main thread; any panic between the set and this reset is an unrecovered crash that ends the process, so no later caller can observe the stale flag
	s.running = false
	s.mu.Unlock()

	s.mu.Lock()
	wins := make([]*darwinWindow, 0, len(s.windows))
	for _, dw := range s.windows {
		wins = append(wins, dw)
	}
	s.mu.Unlock()
	for _, dw := range wins {
		_, wv := dw.ids()
		killPage(wv)
		if win, _ := dw.ids(); win != 0 {
			objc.Send(win, objc.Sel("close"))
		}
	}
	activeShell.Store(nil)
	return nil
}

func windowLaunchOrder(style desktop.WindowStyle) (selector string, activate bool) {
	if style.Panel {
		return "orderFront:", false
	}
	return "makeKeyAndOrderFront:", true
}

func killPage(wv objc.ID) {
	if wv == 0 {
		return
	}
	objc.Send(wv, objc.Sel("stopLoading"))
	objc.Send(wv, objc.Sel("loadHTMLString:baseURL:"), uintptr(objc.NSString("")), 0)
}

func (s *darwinShell) stopLoop() {
	s.mu.Lock()
	nsApp, running := s.nsApp, s.running
	s.mu.Unlock()
	if nsApp == 0 || !running {
		return
	}
	_ = s.onMain(func() {
		objc.Send(nsApp, objc.Sel("stop:"), 0)
		ev := objc.SendF(objc.Class("NSEvent"),
			objc.Sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			[]uintptr{15 /* NSEventTypeApplicationDefined */, 0, 0, 0, 0, 0, 0},
			[]float64{0, 0, 0 /* location + timestamp */})
		objc.Send(nsApp, objc.Sel("postEvent:atStart:"), ev, 1)
	})
}

func (s *darwinShell) Quit() { s.stopLoop() }

func (s *darwinShell) windowID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.window
}

func (s *darwinShell) onEvalCompletion(a *ffi.Args) uintptr {
	var out evalOutcome
	if r := objc.ID(a.Int[1]); r != 0 {
		out.result = objc.GoString(objc.ID(objc.Send(r, objc.Sel("description"))))
	}
	if e := objc.ID(a.Int[2]); e != 0 {
		out.errText = objc.GoString(objc.ID(objc.Send(e, objc.Sel("localizedDescription"))))
	}
	select {
	case s.evalRes <- out:
	default:
	}
	return 0
}

func (s *darwinShell) onAsyncEvalCompletion(a *ffi.Args) uintptr {
	var out evalOutcome
	if r := objc.ID(a.Int[1]); r != 0 {
		out.result = objc.GoString(r)
	}
	if e := objc.ID(a.Int[2]); e != 0 {
		out.errText = jsErrorText(e)
	}
	select {
	case s.asyncRes <- out:
	default:
	}
	return 0
}

func jsErrorText(e objc.ID) string {
	if ui := objc.ID(objc.Send(e, objc.Sel("userInfo"))); ui != 0 {
		if msg := objc.Send(ui, objc.Sel("objectForKey:"), uintptr(objc.NSString("WKJavaScriptExceptionMessage"))); msg != 0 {
			return objc.GoString(objc.ID(msg))
		}
	}
	return objc.GoString(objc.ID(objc.Send(e, objc.Sel("localizedDescription"))))
}

func (s *darwinShell) onSnapshotCompletion(a *ffi.Args) uintptr {
	var out snapOutcome
	image := objc.ID(a.Int[1])
	if image == 0 {
		if e := objc.ID(a.Int[2]); e != 0 {
			out.errText = objc.GoString(objc.ID(objc.Send(e, objc.Sel("localizedDescription"))))
		} else {
			out.errText = "takeSnapshot returned no image and no error"
		}
		s.sendSnap(out)
		return 0
	}
	tiff := objc.Send(image, objc.Sel("TIFFRepresentation"))
	if tiff == 0 {
		out.errText = "snapshot TIFFRepresentation was nil"
		s.sendSnap(out)
		return 0
	}
	rep := objc.ID(objc.Send(objc.Class("NSBitmapImageRep"), objc.Sel("imageRepWithData:"), tiff))
	if rep == 0 {
		out.errText = "snapshot imageRepWithData was nil"
		s.sendSnap(out)
		return 0
	}
	data := objc.ID(objc.Send(rep, objc.Sel("representationUsingType:properties:"), 4 /* PNG */, 0))
	if data == 0 {
		out.errText = "snapshot PNG encoding failed"
		s.sendSnap(out)
		return 0
	}
	n := objc.Send(data, objc.Sel("length"))
	p := objc.Send(data, objc.Sel("bytes"))
	if p == 0 || n == 0 {
		out.errText = "snapshot PNG data was empty"
		s.sendSnap(out)
		return 0
	}
	out.png = objc.CopyBytes(p, n)
	s.sendSnap(out)
	return 0
}

func (s *darwinShell) sendSnap(out snapOutcome) {
	select {
	case s.snapRes <- out:
	default:
	}
}

const bridgeMessageHandlerName = "gofastr"

type darwinWindow struct {
	shell   *darwinShell
	id      string
	mu      sync.Mutex
	window  objc.ID
	webView objc.ID
	ucc     objc.ID
	title   string
	chrome  windowChrome
}

func (s *darwinShell) appID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nsApp
}

func (s *darwinShell) bridgeID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bridge
}

func (w *darwinWindow) ID() string { return w.id }

func (w *darwinWindow) IsClosed() bool {
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	return w.shell.windowsByID[w.id] != w
}

func (w *darwinWindow) ids() (objc.ID, objc.ID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.window, w.webView
}

func (w *darwinWindow) Navigate(url string) error {
	if _, webview := w.ids(); webview == 0 {
		return errWindowClosed()
	}
	s := w.shell
	var callErr error
	err := s.onMain(func() {
		_, wv := w.ids()
		if wv == 0 {
			callErr = errWindowClosed()
			return
		}
		nsurl := objc.Send(objc.Class("NSURL"), objc.Sel("URLWithString:"), uintptr(objc.NSString(url)))
		if nsurl == 0 {
			callErr = errors.New("desktop: invalid URL for navigation")
			return
		}
		req := objc.Send(objc.Class("NSURLRequest"), objc.Sel("requestWithURL:"), nsurl)
		objc.Send(wv, objc.Sel("loadRequest:"), req)
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return callErr
}

func (w *darwinWindow) Eval(js string) error {
	s := w.shell
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	_, wv := w.ids()
	if wv == 0 || s.evalBlock == 0 {
		return errWindowClosed()
	}
	select {
	case <-s.evalRes:
	default:
	}
	closed := false
	submit := func() {
		if _, wv = w.ids(); wv == 0 {
			closed = true
			return
		}
		objc.Send(wv, objc.Sel("evaluateJavaScript:completionHandler:"), uintptr(objc.NSString(js)), s.evalBlock)
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		submit()
		if closed {
			return errWindowClosed()
		}
		return nil
	}
	if err := s.onMain(submit); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return errWindowClosed()
	}
	select {
	case out := <-s.evalRes:
		if out.errText != "" {
			s.logger.Warn("desktop: JavaScript evaluation failed", "error", out.errText)
			return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
		}
		return nil
	case <-time.After(evalTimeout):
		return &desktop.Error{Code: desktop.CodeInternal, Message: "evaluation timed out"}
	}
}

const evalAsyncHardCap = 20 * time.Second

func evalAsyncWrap(body string) string {
	return "try { const __r = await (async () => { " + body + " })(); " +
		"return JSON.stringify(__r === undefined ? null : __r); } catch (e) { throw e; }"
}

func (w *darwinWindow) EvalAsync(ctx context.Context, body string) (json.RawMessage, error) {
	s := w.shell
	s.asyncMu.Lock()
	defer s.asyncMu.Unlock()
	_, wv := w.ids()
	if wv == 0 || s.asyncBlock == 0 {
		return nil, errWindowClosed()
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation cannot run on the UI thread"}
	}
	select {
	case <-s.asyncRes:
	default:
	}
	src := objc.NSString(evalAsyncWrap(body))
	closed := false
	if err := s.onMain(func() {
		if _, wv = w.ids(); wv == 0 {
			closed = true
			return
		}
		args := objc.Send(objc.Class("NSDictionary"), objc.Sel("dictionary"))
		world := objc.Send(objc.Class("WKContentWorld"), objc.Sel("pageWorld"))
		objc.Send(wv, objc.Sel("callAsyncJavaScript:arguments:inFrame:inContentWorld:completionHandler:"),
			uintptr(src), uintptr(args), 0, uintptr(world), s.asyncBlock)
	}); err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return nil, errWindowClosed()
	}
	limit := evalAsyncHardCap
	if d, ok := ctx.Deadline(); ok {
		if remain := time.Until(d); remain < limit {
			limit = remain
		}
	}
	start := time.Now()
	hardDeadline := start.Add(evalAsyncHardCap)
	select {
	case out := <-s.asyncRes:
		if out.errText != "" {
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: out.errText}
		}
		return json.RawMessage(out.result), nil
	case <-ctx.Done():
		s.drainAsync(hardDeadline)
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
	case <-time.After(limit):
		s.drainAsync(hardDeadline)
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
	}
}

func (s *darwinShell) drainAsync(deadline time.Time) {
	select {
	case <-s.asyncRes:
	case <-time.After(time.Until(deadline)):
	}
}

func (w *darwinWindow) Snapshot(ctx context.Context) ([]byte, error) {
	s := w.shell
	s.snapMu.Lock()
	held := true
	defer func() {
		if held {
			s.snapMu.Unlock()
		}
	}()
	_, wv := w.ids()
	if wv == 0 || s.snapBlock == 0 {
		return nil, errWindowClosed()
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot cannot run on the UI thread"}
	}
	closed := false
	if err := s.onMain(func() {
		if _, wv = w.ids(); wv == 0 {
			closed = true
			return
		}
		objc.Send(wv, objc.Sel("takeSnapshotWithConfiguration:completionHandler:"), 0, s.snapBlock)
	}); err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return nil, errWindowClosed()
	}
	var err error
	select {
	case out := <-s.snapRes:
		if out.errText != "" {
			s.logger.Error("desktop: window snapshot failed", "error", out.errText)
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
		}
		return out.png, nil
	case <-ctx.Done():
		err = desktop.ErrCancelled
	case <-time.After(snapTimeout):
		err = &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot timed out"}
	}
	held = false
	go s.drainSnap()
	return nil, err
}

func (s *darwinShell) drainSnap() {
	defer s.snapMu.Unlock()
	select {
	case <-s.snapRes:
	case <-time.After(snapTimeout):
	}
}

func (w *darwinWindow) Title() string {
	if win, _ := w.ids(); win == 0 {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.title
	}
	var title string
	if err := w.shell.onMain(func() {
		if win, _ := w.ids(); win != 0 {
			title = objc.GoString(objc.ID(objc.Send(win, objc.Sel("title"))))
		}
	}); err != nil || title == "" {
		w.mu.Lock()
		defer w.mu.Unlock()
		if title == "" {
			title = w.title
		}
	}
	return title
}

func (w *darwinWindow) SetTitle(title string) error {
	return w.withLiveWindow(func(win objc.ID) {
		objc.Send(win, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		w.mu.Lock()
		w.title = title
		w.mu.Unlock()
	})
}

func (w *darwinWindow) withLiveWindow(fn func(objc.ID)) error {
	if win, _ := w.ids(); win == 0 {
		return errWindowClosed()
	}
	closed := false
	err := w.shell.onMain(func() {
		win, _ := w.ids()
		if win == 0 {
			closed = true
			return
		}
		fn(win)
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return errWindowClosed()
	}
	return nil
}

func (w *darwinWindow) Focus() error {
	return w.withLiveWindow(func(win objc.ID) {
		objc.Send(win, objc.Sel("makeKeyAndOrderFront:"), 0)
		objc.Send(w.shell.appID(), objc.Sel("activateIgnoringOtherApps:"), 1)
	})
}

func (w *darwinWindow) Native() uintptr {
	_, wv := w.ids()
	return uintptr(wv)
}

func (w *darwinWindow) Frame() (desktop.Frame, error) {
	var f desktop.Frame
	if err := w.withLiveWindow(func(win objc.ID) { f = readFrameOnMain(win) }); err != nil {
		return desktop.Frame{}, err
	}
	return f, nil
}

func (w *darwinWindow) SetFrame(f desktop.Frame) error {
	if f.Width <= 0 || f.Height <= 0 {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "frame width and height must be positive"}
	}
	return w.withLiveWindow(func(win objc.ID) {
		objc.SendRect(win, objc.Sel("setFrame:display:"), frameToRect(f, readScreens()), 1)
		w.shell.reportWindowFrame(w)
	})
}

func readFrameOnMain(win objc.ID) desktop.Frame {
	return rectToFrame(objc.SendRectRet(win, objc.Sel("frame")), readScreens())
}

func (s *darwinShell) reportWindowFrame(w *darwinWindow) {
	s.mu.Lock()
	cb := s.onWindowFrame
	s.mu.Unlock()
	if cb == nil {
		return
	}
	if win, _ := w.ids(); win != 0 {
		f := readFrameOnMain(win)
		deliverOnGoroutine(func() { cb(w, f) })
	}
}

func (w *darwinWindow) Close() error {
	if win, _ := w.ids(); win == 0 {
		return nil
	}
	err := w.shell.onMain(func() {
		win, _ := w.ids()
		if win == 0 {
			return
		}
		if objc.Send(win, objc.Sel("styleMask"))&maskClosable == 0 {
			if objc.Send(w.shell.bridgeID(), objc.Sel("windowShouldClose:"), uintptr(win)) != 0 {
				objc.Send(win, objc.Sel("close"))
			}
			return
		}
		objc.Send(win, objc.Sel("performClose:"), 0)
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

func (s *darwinShell) OpenWindow(id string, spec desktop.WindowSpec, url string) (desktop.Window, error) {
	s.mu.Lock()
	mainConfig := s.wkConfig
	running := s.running
	s.mu.Unlock()
	if mainConfig == 0 || !running {
		return nil, errWindowClosed()
	}
	width, height := spec.Width, spec.Height
	if width <= 0 {
		width = 480
	}
	if height <= 0 {
		height = 360
	}
	title := spec.Title
	if title == "" {
		title = id
	}
	mask := windowStyleMask(spec.Style)
	windowClass := "NSWindow"
	if spec.Style.Panel {
		windowClass = "NSPanel"
	}
	w := &darwinWindow{shell: s, id: id, title: title}
	err := s.onMain(func() {
		bridge := s.bridgeID()

		store := objc.Send(mainConfig, objc.Sel("websiteDataStore"))
		config := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("WKWebViewConfiguration"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(config, objc.Sel("setWebsiteDataStore:"), store)
		ucc := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("WKUserContentController"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(config, objc.Sel("setUserContentController:"), uintptr(ucc))
		objc.Send(ucc, objc.Sel("addScriptMessageHandler:name:"), uintptr(bridge), uintptr(objc.NSString(bridgeMessageHandlerName)))
		userScript := objc.Send(objc.ID(objc.Send(objc.Class("WKUserScript"), objc.Sel("alloc"))),
			objc.Sel("initWithSource:injectionTime:forMainFrameOnly:"),
			uintptr(objc.NSString(desktop.BootstrapJS(id, s.reduceTransparency.Load()))), 0 /* atDocumentStart */, 1 /* main frame only */)
		objc.Send(ucc, objc.Sel("addUserScript:"), uintptr(userScript))

		webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
			objc.Sel("initWithFrame:configuration:"), objc.Rect{W: float64(width), H: float64(height)}, uintptr(config)))
		objc.Send(config, objc.Sel("release"))
		objc.Send(objc.ID(userScript), objc.Sel("release"))
		window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class(windowClass), objc.Sel("alloc"))),
			objc.Sel("initWithContentRect:styleMask:backing:defer:"),
			objc.Rect{W: float64(width), H: float64(height)},
			mask, 2 /* buffered */, 0))
		objc.Send(window, objc.Sel("setReleasedWhenClosed:"), 0)
		objc.Send(window, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(window, objc.Sel("setDelegate:"), uintptr(bridge))
		objc.Send(webView, objc.Sel("setNavigationDelegate:"), uintptr(bridge))
		applyWindowStyle(window, webView, spec.Style, height, nativeStylePlacement())
		chrome := applyWindowMaterial(window, webView, spec.Style, spec.SidebarWidth, s.reduceTransparency.Load(), s.logger)
		applyRememberedFrame(window, spec.Frame)
		order, activate := windowLaunchOrder(spec.Style)
		objc.Send(window, objc.Sel(order), 0)
		if activate {
			objc.Send(s.appID(), objc.Sel("activateIgnoringOtherApps:"), 1)
		}
		applyTrafficLightInset(window, spec.Style.TrafficLightInset)
		w.mu.Lock()
		w.window, w.webView, w.ucc, w.chrome = window, webView, ucc, chrome
		w.mu.Unlock()
		nsurl := objc.Send(objc.Class("NSURL"), objc.Sel("URLWithString:"), uintptr(objc.NSString(url)))
		if nsurl == 0 {
			s.logger.Error("desktop: secondary window got an invalid URL", "id", id, "url", url)
		} else {
			req := objc.Send(objc.Class("NSURLRequest"), objc.Sel("requestWithURL:"), nsurl)
			objc.Send(webView, objc.Sel("loadRequest:"), req)
		}
	})
	if err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	w.mu.Lock()
	win, ucc := w.window, w.ucc
	w.mu.Unlock()
	if win == 0 {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	s.mu.Lock()
	s.windows[win] = w
	s.windowsByID[id] = w
	if ucc != 0 {
		s.windowsByUCC[ucc] = w
	}
	s.mu.Unlock()
	return w, nil
}

func (s *darwinShell) lookupWindow(win objc.ID) (*darwinWindow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[win]
	return w, ok
}

func (s *darwinShell) windowByID(id string) (*darwinWindow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windowsByID[id]
	return w, ok
}

func (s *darwinShell) forgetWindow(w *darwinWindow) {
	w.mu.Lock()
	win, ucc := w.window, w.ucc
	w.mu.Unlock()
	s.mu.Lock()
	delete(s.windows, win)
	delete(s.windowsByID, w.id)
	delete(s.windowsByUCC, ucc)
	s.mu.Unlock()
}

func releaseSecondaryWindow(w *darwinWindow) {
	objc.AssertMainThread("releaseSecondaryWindow")
	w.mu.Lock()
	win, wv, ucc := w.window, w.webView, w.ucc
	w.window, w.webView, w.ucc = 0, 0, 0
	w.mu.Unlock()
	if ucc != 0 {
		objc.Send(ucc, objc.Sel("removeScriptMessageHandlerForName:"), uintptr(objc.NSString(bridgeMessageHandlerName)))
		objc.Send(ucc, objc.Sel("release"))
	}
	if wv != 0 {
		objc.Send(wv, objc.Sel("setNavigationDelegate:"), 0)
		objc.Send(wv, objc.Sel("release"))
	}
	if win != 0 {
		objc.Send(win, objc.Sel("setDelegate:"), 0)
		objc.Send(win, objc.Sel("autorelease"))
	}
}

func (s *darwinShell) startWindowDrag(ucc objc.ID) {
	objc.AssertMainThread("startWindowDrag")
	if ucc == 0 {
		return
	}
	s.mu.Lock()
	w, ok := s.windowsByUCC[ucc]
	s.mu.Unlock()
	if !ok {
		return
	}
	win, _ := w.ids()
	if win == 0 {
		return
	}
	event := objc.Send(s.appID(), objc.Sel("currentEvent"))
	if event == 0 {
		return
	}
	objc.Send(win, objc.Sel("performWindowDragWithEvent:"), event)
}
