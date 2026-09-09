//go:build darwin && arm64

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
)

// The darwin/arm64 desktop.Shell: a real AppKit + WKWebView host on top of
// internal/objc. Every AppKit/WebKit call happens on the main thread
// (asserted); every native callback lands there and hands off to Go
// channels or goroutines immediately so the UI thread never waits on
// Go (docs/desktop-plan.md, "The UI thread").
//
// Lazy loading: constructing this shell loads nothing. Frameworks
// dlopen on the first Run (objc's init only locks the main thread), so
// cmd/gofastr's blank import of this battery never touches AppKit.

// evalTimeout bounds one JavaScript evaluation (completion included).
const evalTimeout = 15 * time.Second

// snapTimeout bounds one window snapshot.
const snapTimeout = 30 * time.Second

// ─── desktop.Window styles ───────────────────────────────────────────────────

// NSWindowStyleMask bits (AppKit/NSWindow.h in the SDK, verified
// 2026-09-05): titled 1<<0, closable 1<<1, miniaturizable 1<<2,
// resizable 1<<3, nonactivatingPanel 1<<7 (NSPanel only),
// fullSizeContentView 1<<15.
const (
	maskTitled              uintptr = 1 << 0
	maskClosable            uintptr = 1 << 1
	maskMiniaturizable      uintptr = 1 << 2
	maskResizable           uintptr = 1 << 3
	maskNonactivatingPanel  uintptr = 1 << 7
	maskFullSizeContentView uintptr = 1 << 15
)

// desktop.Window levels (CoreGraphics/CGWindowLevel.h): NSNormalWindowLevel is
// 0 and NSFloatingWindowLevel is kCGFloatingWindowLevel, which is 3.
const (
	normalWindowLevel   = 0
	floatingWindowLevel = 3
)

// behaviorCanJoinAllSpaces is NSWindowCollectionBehaviorCanJoinAllSpaces
// (NSWindow.h), the only collection-behavior bit desktop.WindowStyle sets.
const behaviorCanJoinAllSpaces uintptr = 1 << 0

// windowStyleMask resolves a desktop.WindowStyle to the styleMask for
// initWithContentRect:styleMask:backing:defer:. desktop.ChromeNone is
// borderless (0): closable and resizable are meaningless without a
// title bar, so they drop with it.
func windowStyleMask(style desktop.WindowStyle) uintptr {
	var mask uintptr
	switch style.Chrome {
	case desktop.ChromeNone:
		// Borderless: no bits of its own.
	case desktop.ChromeHiddenTitle, desktop.ChromeUnified:
		mask = maskTitled | maskClosable | maskFullSizeContentView
	default:
		mask = maskTitled | maskClosable
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

// windowLevel resolves the window level a style asks for. A panel is
// floating even without Float set: Panel implies Float.
func windowLevel(style desktop.WindowStyle) int {
	if style.Float || style.Panel {
		return floatingWindowLevel
	}
	return normalWindowLevel
}

// frameOrigin flips the style's top-left based X/Y (screen points) to
// the bottom-left origin setFrameOrigin: wants, against a screen
// screenHeight points tall and a window height points high. ok is
// false when the style names no origin (the window centers) or only
// one of the pair.
func frameOrigin(style desktop.WindowStyle, height int, screenHeight float64) (x, y float64, ok bool) {
	if style.X == nil || style.Y == nil {
		return 0, 0, false
	}
	return float64(*style.X), screenHeight - float64(*style.Y) - float64(height), true
}

// applyWindowStyle runs every post-creation style setter. Call on the
// main thread; the mask half of the style was applied at window
// creation (windowStyleMask) and the panel class at alloc time.
func applyWindowStyle(win, webView objc.ID, style desktop.WindowStyle, height int) {
	if style.Chrome == desktop.ChromeHiddenTitle || style.Chrome == desktop.ChromeUnified {
		objc.Send(win, objc.Sel("setTitlebarAppearsTransparent:"), 1)
		objc.Send(win, objc.Sel("setTitleVisibility:"), 1) // NSWindowTitleHidden
	}
	if level := windowLevel(style); level != normalWindowLevel {
		objc.Send(win, objc.Sel("setLevel:"), uintptr(level))
	}
	if style.Chrome == desktop.ChromeUnified {
		// An empty NSToolbar must be attached for the toolbar style to
		// take effect; that is the Notes and Finder shape (the toolbar
		// strip merges with the title bar instead of drawing its own).
		toolbar := objc.Send(objc.ID(objc.Send(objc.Class("NSToolbar"), objc.Sel("alloc"))), objc.Sel("init"))
		objc.Send(win, objc.Sel("setToolbar:"), uintptr(toolbar))
		objc.Send(win, objc.Sel("setToolbarStyle:"), toolbarStyleUnified)
	}
	if style.Transparent {
		objc.Send(win, objc.Sel("setOpaque:"), 0)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
		// The web view must stop painting its own background too.
		// drawsBackground is private SPI (_drawsBackground in
		// WKWebViewPrivate.h); the KVC spelling reaches the same
		// property without naming the private selector, the way Tauri
		// does behind its macOSPrivateApi flag. There is no public
		// switch. See the phase 13 research in docs/desktop-plan.md
		// before shipping this wider.
		setWebViewBackground(webView, false)
	}
	if style.AllSpaces {
		objc.Send(win, objc.Sel("setCollectionBehavior:"), behaviorCanJoinAllSpaces)
	}
	if x, y, ok := frameOrigin(style, height, mainScreenHeight()); ok {
		// setFrameOrigin: takes an NSPoint, two doubles.
		objc.SendF(win, objc.Sel("setFrameOrigin:"), nil, []float64{x, y})
	}
}

// mainScreenHeight reads the main screen's frame height, the base the
// top-left Y flips against. 0 when there is no screen (headless); the
// caller's frameOrigin answer is then still computed, which parks the
// window below the visible area rather than crashing.
func mainScreenHeight() float64 {
	screen := objc.Send(objc.Class("NSScreen"), objc.Sel("mainScreen"))
	if screen == 0 {
		return 0
	}
	return objc.SendRectRet(objc.ID(screen), objc.Sel("frame")).H
}

// ─── desktop.Window frames ───────────────────────────────────────────────────

// The Frame contract speaks top-left screen points measured from the
// PRIMARY screen's top-left corner (the desktop.WindowStyle.X/Y convention).
// AppKit speaks global bottom-left points measured from the primary
// screen's bottom-left. The flip between them runs only in this file,
// against the primary screen's height: [NSScreen screens] index 0 is
// the screen with the origin at (0,0), while [NSScreen mainScreen] is
// whichever screen holds the key window and moves under the user, so
// it must never be the flip base.

// minVisibleFrameW/H is the overlap a remembered frame must keep with
// some screen's visible area to count as still placed: less than this
// (a monitor unplugged, a resolution change) drops the frame and the
// window centers.
const (
	minVisibleFrameW = 100.0
	minVisibleFrameH = 50.0
)

// screenInfo is one NSScreen: its full frame and its visibleFrame,
// both in global bottom-left points.
type screenInfo struct {
	frame, visible objc.Rect
}

// readScreens reads [NSScreen screens]. Main thread only.
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

// screenFlipBase is the height the top-left flip runs against: the
// primary screen's (screens[0], the origin screen) frame height. Zero
// with no screens (headless), which is also what the screens query
// would answer; the function never touches AppKit, so the pure math
// stays unit-testable before any framework loads.
func screenFlipBase(screens []screenInfo) float64 {
	if len(screens) > 0 {
		return screens[0].frame.H
	}
	return 0
}

// frameToRect converts a top-left Frame to the global bottom-left
// NSRect setFrame:display: wants.
func frameToRect(f desktop.Frame, screens []screenInfo) objc.Rect {
	base := screenFlipBase(screens)
	return objc.Rect{
		X: float64(f.X),
		Y: base - float64(f.Y) - float64(f.Height),
		W: float64(f.Width),
		H: float64(f.Height),
	}
}

// rectToFrame converts a window's global bottom-left frame rect to the
// top-left Frame contract. Values round to the nearest point; AppKit
// frames are integral in practice.
func rectToFrame(r objc.Rect, screens []screenInfo) desktop.Frame {
	base := screenFlipBase(screens)
	return desktop.Frame{
		X:      int(math.Round(r.X)),
		Y:      int(math.Round(base - r.Y - r.H)),
		Width:  int(math.Round(r.W)),
		Height: int(math.Round(r.H)),
	}
}

// frameVisibleOnScreens reports whether the frame's rectangle overlaps
// some screen's visible area by at least 100x50 points. This is the
// "screen that is gone" check a remembered frame must pass.
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

// applyRememberedFrame applies f to a freshly created window when it
// still lands on a screen; otherwise the window centers (a remembered
// frame from an unplugged monitor is dropped). Main thread only.
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

// activeShell is the one live darwin shell per process: the bridge
// class's IMPs are registered once (callback slots never free), so
// they route through this pointer. The plan scopes the PoC to a single
// window.
var activeShell atomic.Pointer[darwinShell]

// darwinShell implements desktop.Shell with AppKit + WKWebView.
type darwinShell struct {
	// desktop.Notification state (shell_darwin_caps.go Show): the two completion
	// blocks are built once and shared, so Show serializes on notifyMu.
	notifyMu   sync.Mutex
	notifyOnce sync.Once
	// notifyDelegateOnce installs the bridge as the notification
	// centre's delegate on the first Show.
	notifyDelegateOnce sync.Once
	authBlock          uintptr
	addBlock           uintptr
	authRes            chan notifyResult
	addRes             chan notifyResult

	logger *slog.Logger

	// Native objects, set by Run and read-only afterwards; webView is
	// published last (desktop.Window methods gate on it). window/webView stay
	// the MAIN window's; wkConfig is the WKWebViewConfiguration every
	// window shares, so a second web view gets the first one's cookie
	// store (the boot cookie) for free.
	mu             sync.Mutex
	nsApp          objc.ID
	bridge         objc.ID
	window         objc.ID
	webView        objc.ID
	wkConfig       objc.ID
	title          string
	actionIDs      []string
	onMenu         func(id string)
	onSettings     func()
	onWindowClosed func(id string)
	// onWindowFrame reports user moves and resizes (the windowDidMove:
	// and windowDidEndLiveResize: delegates), on a goroutine.
	onWindowFrame func(id string, f desktop.Frame)
	// onWindowFocus and onWindowBlur report the key-window delegates
	// (windowDidBecomeKey:/windowDidResignKey:), on a goroutine.
	onWindowFocus func(id string)
	onWindowBlur  func(id string)
	// onAppearance reports appearance changes the page cannot read
	// through CSS (the NSWorkspace accessibility observer), on a
	// goroutine.
	onAppearance func(desktop.Appearance)
	// reduceTransparency is the live Reduce Transparency state, read
	// at Run and kept current by the observer.
	reduceTransparency atomic.Bool
	running            bool

	// windows maps NSWindow ids to their per-window state, and
	// windowsByID maps window ids to the same, so windowShouldClose:
	windows     map[objc.ID]*darwinWindow
	windowsByID map[string]*darwinWindow
	// windowsByUCC maps each window's WKUserContentController to its
	// window: the script-message handler learns which controller fired
	// (its drag message must drag THAT window), not which web view.
	windowsByUCC map[objc.ID]*darwinWindow
	// closeHides is Config.Tray.CloseHidesWindow: the main window's
	// close button hides the window instead of quitting.
	closeHides bool

	// The tray status item (0 when no tray). Retained: a released
	// status item vanishes from the bar.
	statusItem objc.ID
	// trayTitle tracks the last title set, for SetTrayTitle before
	// Run installed the item.
	trayTitle string
	// promptQueue is the e2e/test seam (ScriptPrompts): queued
	// decisions answer the next prompts without an alert, consumed in
	// order; an empty queue means the real alert shows.
	promptQueue []desktop.Decision

	// scriptMsgs is the unexported test seam carrying the bodies of
	// window.webkit.messageHandlers.gofastr.postMessage calls. The
	// battery itself does not use the message channel (the bridge
	// rides HTTP); messages are logged at Debug and dropped.
	scriptMsgs chan string

	// eval plumbing: one completion block shared by every Eval (ffi
	// slots are process-lifetime; evals are serialized by evalMu so a
	// single result channel suffices, the spike's recipe).
	evalMu    sync.Mutex
	evalBlock uintptr
	evalRes   chan evalOutcome

	// snapshot plumbing, same shape as eval.
	snapMu    sync.Mutex
	snapBlock uintptr
	snapRes   chan snapOutcome

	// notifyLog records every desktop.Notification Show received (bundled or
	// not) for desktop.NativeDriver.NotificationLog, capped, oldest dropped.
	notifyLogMu sync.Mutex
	notifyLog   []desktop.Notification

	// asyncEval plumbing: callAsyncJavaScript's completion, the same
	// shape as eval above (one block, one result channel, serialized
	// by asyncMu) for desktop.PageEvaluator.EvalAsync.
	asyncMu    sync.Mutex
	asyncBlock uintptr
	asyncRes   chan evalOutcome
}

// evalOutcome is one evaluateJavaScript completion.
type evalOutcome struct {
	result  string
	errText string
}

// snapOutcome is one takeSnapshot completion (PNG bytes).
type snapOutcome struct {
	png     []byte
	errText string
}

// New returns the darwin/arm64 shell: AppKit + WKWebView on the pure-Go
// Objective-C bridge. It must stay lazy: no framework is loaded here
// (TestShellConstructsLazy pins it), because cmd/gofastr blank-imports
// the battery. On every other GOOS/GOARCH shell_other.go answers the
// unsupported shell instead.
func New() desktop.Shell {
	return &darwinShell{logger: slog.Default()}
}

// Main runs fn on the UI thread and waits. When the caller is already
// on the UI thread fn runs inline; dispatching and waiting from
// inside the thread that must drain the queue is the one deadlock this
// shell has to design out (the battery's ready callback navigates on
// the UI thread before the run loop starts).
func (s *darwinShell) Main(fn func()) error { return s.onMain(fn) }

// onMain is Main without the exported contract: inline on the main
// thread, objc.Main (deadline included) everywhere else.
func (s *darwinShell) onMain(fn func()) error {
	return s.onMainWithTimeout(objc.DefaultMainTimeout, fn)
}

// onMainWithTimeout is onMain with a caller-chosen deadline. Work that
// waits for a PERSON (an NSAlert's runModal blocks until the click)
// must name one: the default is ten seconds, which silently voided
// every permission decision a user was slower than that to give.
func (s *darwinShell) onMainWithTimeout(d time.Duration, fn func()) error {
	if objc.PthreadSelf() == objc.MainThreadID() {
		fn()
		return nil
	}
	return objc.MainWithTimeout(d, fn)
}

// Run brings up the window and blocks in [NSApp run].
func (s *darwinShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	objc.AssertMainThread("darwinShell.Run")
	// Frameworks load here and only here, on the main thread (their
	// +load initializers must run on it).
	objc.OpenFrameworks()

	// One process-lifetime autorelease pool (spike recipe).
	objc.Send(objc.ID(objc.Send(objc.Class("NSAutoreleasePool"), objc.Sel("alloc"))), objc.Sel("init"))
	nsApp := objc.ID(objc.Send(objc.Class("NSApplication"), objc.Sel("sharedApplication")))
	policy := uintptr(0)                      // Regular
	if cfg.Tray != nil && cfg.Tray.HideDock { // Accessory: no Dock icon
		policy = 1
	}
	objc.Send(nsApp, objc.Sel("setActivationPolicy:"), policy)

	bridge := objc.ID(objc.Send(objc.ID(objc.Send(ensureBridgeClass(), objc.Sel("alloc"))), objc.Sel("init")))
	// Deep links: the GetURL Apple Event handler must be registered
	// before [NSApp run] starts, or a cold-launch URL is lost.
	installDeepLinkHandler(bridge, cfg.OnDeepLink)

	hasSettings := cfg.Settings != nil
	plans, actionIDs := desktop.PlanMenuBar(cfg.Title, cfg.Menu, hasSettings)
	trayRows := desktop.PlanTrayMenu(cfg.Tray, cfg.Title, &actionIDs)
	s.mu.Lock()
	s.nsApp = nsApp
	s.bridge = bridge
	s.title = cfg.Title
	s.onMenu = cfg.OnMenu
	s.onSettings = cfg.OnSettings
	s.onWindowClosed = cfg.OnWindowClosed
	s.onWindowFrame = cfg.OnWindowFrame
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
	s.evalBlock = objc.NewBlock(ffi.NewCallback(s.onEvalCompletion))
	s.snapBlock = objc.NewBlock(ffi.NewCallback(s.onSnapshotCompletion))
	s.running = true
	s.mu.Unlock()
	activeShell.Store(s)

	s.buildMenuBar(plans)

	config := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("WKWebViewConfiguration"), objc.Sel("alloc"))), objc.Sel("init")))
	ucc := objc.ID(objc.Send(config, objc.Sel("userContentController")))
	objc.Send(ucc, objc.Sel("addScriptMessageHandler:name:"), uintptr(bridge), uintptr(objc.NSString(bridgeMessageHandlerName)))

	// The appearance read and its observer run after the state block:
	// installAppearanceObserver reaches the bridge through bridgeID,
	// which takes s.mu. The boot marker below needs the value, so the
	// first paint is right.
	s.readReduceTransparency()
	s.installAppearanceObserver()

	userScript := objc.Send(objc.ID(objc.Send(objc.Class("WKUserScript"), objc.Sel("alloc"))),
		objc.Sel("initWithSource:injectionTime:forMainFrameOnly:"),
		uintptr(objc.NSString(desktop.BootstrapJS(desktop.MainWindowID, s.reduceTransparency.Load()))), 0 /* atDocumentStart */, 0 /* all frames */)
	objc.Send(ucc, objc.Sel("addUserScript:"), uintptr(userScript))

	webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:configuration:"), objc.Rect{W: float64(cfg.Width), H: float64(cfg.Height)}, uintptr(config)))
	// Config.Style shapes the main window the same way a desktop.WindowSpec's
	// Style shapes a secondary one: the mask at init, the panel class
	// at alloc, the setters after.
	windowClass := "NSWindow"
	if cfg.Style.Panel {
		windowClass = "NSPanel" // same init selector as NSWindow
	}
	window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class(windowClass), objc.Sel("alloc"))),
		objc.Sel("initWithContentRect:styleMask:backing:defer:"),
		objc.Rect{W: float64(cfg.Width), H: float64(cfg.Height)},
		windowStyleMask(cfg.Style), 2 /* buffered */, 0))
	objc.Send(window, objc.Sel("setTitle:"), uintptr(objc.NSString(cfg.Title)))
	// The web view IS the window's content, and the bridge is both the
	// window delegate (windowShouldClose:, the CloseHidesWindow path)
	// and the navigation delegate. These three lines went missing in a
	// refactor and every main window opened blank: desktop.Window.Snapshot
	// renders the WKWebView on its own, so no snapshot test noticed.
	// WindowState.ContentClass is the assertion that now would.
	objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
	objc.Send(window, objc.Sel("setDelegate:"), uintptr(bridge))
	objc.Send(webView, objc.Sel("setNavigationDelegate:"), uintptr(bridge))
	s.evalBlock = objc.NewBlock(ffi.NewCallback(s.onEvalCompletion))
	s.snapBlock = objc.NewBlock(ffi.NewCallback(s.onSnapshotCompletion))
	s.asyncRes = make(chan evalOutcome, 1)
	s.asyncBlock = objc.NewBlock(ffi.NewCallback(s.onAsyncEvalCompletion))
	s.running = true
	applyWindowStyle(window, webView, cfg.Style, cfg.Height)
	// The material goes in before the window fronts, so the first
	// paint already has the effect under it.
	mainChrome := applyWindowMaterial(window, webView, cfg.Style, cfg.SidebarWidth, s.reduceTransparency.Load(), s.logger)
	// A remembered frame wins over the style origin and over the
	// centered default; a frame from a screen that is gone centers.
	framePlaced := cfg.Frame != nil
	if framePlaced {
		applyRememberedFrame(window, cfg.Frame)
	}
	objc.Send(window, objc.Sel("makeKeyAndOrderFront:"), 0)
	if !framePlaced && (cfg.Style.X == nil || cfg.Style.Y == nil) {
		// No explicit origin: the host default is centered.
		objc.Send(window, objc.Sel("center"))
	}
	// The buttons exist once the title bar is real; the inset goes on
	// after the window fronts and every delegate path re-applies it.
	applyTrafficLightInset(window, cfg.Style.TrafficLightInset)
	objc.Send(nsApp, objc.Sel("activateIgnoringOtherApps:"), 1)

	main := &darwinWindow{shell: s, id: desktop.MainWindowID, window: window, webView: webView, ucc: ucc, title: cfg.Title, chrome: mainChrome}
	s.mu.Lock()
	s.window = window
	s.webView = webView
	s.wkConfig = config
	s.windows[window] = main
	s.windowsByID[main.id] = main
	s.windowsByUCC[ucc] = main
	s.mu.Unlock()

	if cfg.Tray != nil {
		s.installTray(cfg.Tray, trayRows)
	}

	// ready runs on this (the UI) thread per the desktop.Shell contract; the
	// battery's callback navigates to the boot URL, which onMain runs
	// inline. [NSApp run] then services every later hop.
	ready(main)

	// Watch ctx: cancellation stops the loop from a goroutine (never
	// from the loop's own thread).
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
	s.running = false
	s.mu.Unlock()

	// Off the loop, still on the main thread: stop the pages and
	// close the windows so the SSE connections drop and the app's
	// Shutdown can drain the HTTP server instead of timing out on it
	// (spike recipe). close (not performClose:) bypasses
	// windowShouldClose:, so a CloseHidesWindow config cannot turn the
	// teardown into a hide and leave the process's pages alive.
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

// killPage stops a web view's loads and navigates it to an empty
// page: stopLoading alone does not drop an open EventSource, and a
// closed window's web view can linger in the process-lifetime
// autorelease pool with its page (and its SSE) still running. The
// empty navigation tears the page down deterministically.
func killPage(wv objc.ID) {
	if wv == 0 {
		return
	}
	objc.Send(wv, objc.Sel("stopLoading"))
	objc.Send(wv, objc.Sel("loadHTMLString:baseURL:"), uintptr(objc.NSString("")), 0)
}

// stopLoop stops [NSApp run] and posts a dummy application-defined
// event so the loop wakes immediately (the spike's recipe). Safe
// before Run starts and after it returns: both leave running false.
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

// Quit ends the event loop Run blocks in.
func (s *darwinShell) Quit() { s.stopLoop() }

// webViewID returns the live WKWebView, 0 before Run published it.
func (s *darwinShell) webViewID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.webView
}

// windowID returns the live NSWindow, 0 before Run published it.
func (s *darwinShell) windowID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.window
}

// onEvalCompletion is the shared evaluateJavaScript completion block:
// (result, NSError). Runs on the main thread.
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

// onAsyncEvalCompletion is the shared callAsyncJavaScript completion
// block: (result, NSError). The wrapper EvalAsync sends always returns
// a string, so the result object is converted to a Go string here,
// inside the block, and the NSError text is read here too: an NSError
// handed to a completion block is only alive for the block's duration.
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

// jsErrorText digs the JavaScript error's message out of the NSError
// WebKit hands a callAsyncJavaScript completion. The localized
// description is generic ("A JavaScript exception occurred"); the
// thrown text lives in userInfo under WKJavaScriptExceptionMessage
// (observed 2026-09-05; the key is not in a public header).
func jsErrorText(e objc.ID) string {
	if ui := objc.ID(objc.Send(e, objc.Sel("userInfo"))); ui != 0 {
		if msg := objc.Send(ui, objc.Sel("objectForKey:"), uintptr(objc.NSString("WKJavaScriptExceptionMessage"))); msg != 0 {
			return objc.GoString(objc.ID(msg))
		}
	}
	return objc.GoString(objc.ID(objc.Send(e, objc.Sel("localizedDescription"))))
}

// onSnapshotCompletion is the shared takeSnapshot completion block:
// (NSImage, NSError). The PNG conversion happens here, on the main
// thread (spike recipe).
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

// bridgeMessageHandlerName is the WKScriptMessageHandler name the
// shell registers ("gofastr"). The battery's transport is HTTP; this
// channel exists for the Debug log and the e2e seam.
const bridgeMessageHandlerName = "gofastr"

// errWindowClosed is the fixed refusal for desktop.Window methods before Run.
func errWindowClosed() *desktop.Error {
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "window is not open"}
}

// desktop.MainWindowID lives in shell.go: the test double and the portable
// code need it on every GOOS/GOARCH, not only darwin/arm64.

// darwinWindow implements desktop.Window around one NSWindow + WKWebView
// pair. The main window and every secondary window carry their own
// ids; the shared eval/snapshot completion blocks on the shell do not
// care which web view fired them (their mutexes serialize callers).
// ucc is this window's own WKUserContentController: every window gets
// one (own user script carrying its id, own message-handler
// registration) so the page learns which window it lives in.
type darwinWindow struct {
	shell   *darwinShell
	id      string
	mu      sync.Mutex
	window  objc.ID
	webView objc.ID
	ucc     objc.ID
	title   string
	// chrome is this window's material/inset state (main-thread-only
	// fields inside; the struct itself is swapped under mu).
	chrome windowChrome
}

// appID returns the NSApplication object (0 before Run).
func (s *darwinShell) appID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nsApp
}

// bridgeID returns the bridge instance (0 before Run).
func (s *darwinShell) bridgeID() objc.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bridge
}

// ID implements desktop.Window.
func (w *darwinWindow) ID() string { return w.id }

// ids snapshots the window's live NSWindow/WKWebView pair.
func (w *darwinWindow) ids() (objc.ID, objc.ID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.window, w.webView
}

// Navigate loads url in this window's web view.
func (w *darwinWindow) Navigate(url string) error {
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

// Eval evaluates js and waits for its completion. On the UI thread it
// submits and returns without waiting (the completion fires there;
// waiting would deadlock).
func (w *darwinWindow) Eval(js string) error {
	s := w.shell
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	_, wv := w.ids()
	if wv == 0 || s.evalBlock == 0 {
		return errWindowClosed()
	}
	// Drop a stale completion from an earlier fire-and-forget eval.
	select {
	case <-s.evalRes:
	default:
	}
	submit := func() {
		objc.Send(wv, objc.Sel("evaluateJavaScript:completionHandler:"), uintptr(objc.NSString(js)), s.evalBlock)
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		submit()
		return nil
	}
	if err := s.onMain(submit); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
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

// evalAsyncHardCap bounds one EvalAsync wait whatever the caller's
// context says: a hung page cannot hold the caller forever.
const evalAsyncHardCap = 20 * time.Second

// evalAsyncWrap builds the function body EvalAsync sends: everything
// the page returns crosses as an NSString (JSON.stringify of the
// value, null for undefined), so the completion always receives a
// string or an error, never an arbitrary object.
func evalAsyncWrap(body string) string {
	return "try { const __r = await (async () => { " + body + " })(); " +
		"return JSON.stringify(__r === undefined ? null : __r); } catch (e) { throw e; }"
}

// EvalAsync implements desktop.PageEvaluator through WKWebView's
// callAsyncJavaScript, which awaits promises natively. The caller's
// ctx bounds the wait (20 s hard cap); a page that never answers is
// "page evaluation timed out", a throw or rejection is an *desktop.Error with
// desktop.CodeInternal carrying the error's message.
func (w *darwinWindow) EvalAsync(ctx context.Context, body string) (json.RawMessage, error) {
	s := w.shell
	s.asyncMu.Lock()
	defer s.asyncMu.Unlock()
	_, wv := w.ids()
	if wv == 0 || s.asyncBlock == 0 {
		return nil, errWindowClosed()
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		// The completion block fires on this thread; waiting for it
		// here would deadlock.
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation cannot run on the UI thread"}
	}
	// Drop a stale completion from an earlier timed-out eval.
	select {
	case <-s.asyncRes:
	default:
	}
	src := objc.NSString(evalAsyncWrap(body))
	if err := s.onMain(func() {
		args := objc.Send(objc.Class("NSDictionary"), objc.Sel("dictionary"))
		world := objc.Send(objc.Class("WKContentWorld"), objc.Sel("pageWorld"))
		objc.Send(wv, objc.Sel("callAsyncJavaScript:arguments:inFrame:inContentWorld:completionHandler:"),
			uintptr(src), uintptr(args), 0, uintptr(world), s.asyncBlock)
	}); err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
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

// drainAsync waits out the completion of an eval whose caller already
// received its timeout: the completion still fires later, and the one
// result channel is shared, so an undrained late completion would
// masquerade as the next eval's answer. asyncMu is still held (the
// caller has not returned), so nothing else waits on the channel; the
// hard cap bounds the drain when nothing ever fires.
func (s *darwinShell) drainAsync(deadline time.Time) {
	select {
	case <-s.asyncRes:
	case <-time.After(time.Until(deadline)):
		// Nothing fired (the page died mid-eval); there is nothing
		// left to protect.
	}
}

// Snapshot captures this window's rendered page as PNG bytes.
func (w *darwinWindow) Snapshot(ctx context.Context) ([]byte, error) {
	s := w.shell
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	_, wv := w.ids()
	if wv == 0 || s.snapBlock == 0 {
		return nil, errWindowClosed()
	}
	if objc.PthreadSelf() == objc.MainThreadID() {
		// The completion block fires on this thread; waiting for it
		// here would deadlock.
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot cannot run on the UI thread"}
	}
	if err := s.onMain(func() {
		objc.Send(wv, objc.Sel("takeSnapshotWithConfiguration:completionHandler:"), 0, s.snapBlock)
	}); err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	select {
	case out := <-s.snapRes:
		if out.errText != "" {
			s.logger.Error("desktop: window snapshot failed", "error", out.errText)
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
		}
		return out.png, nil
	case <-ctx.Done():
		return nil, desktop.ErrCancelled
	case <-time.After(snapTimeout):
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot timed out"}
	}
}

// Title reads the live NSWindow title (cached copy on hop failure).
func (w *darwinWindow) Title() string {
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

// SetTitle changes this window's title.
func (w *darwinWindow) SetTitle(title string) error {
	w.mu.Lock()
	w.title = title
	w.mu.Unlock()
	err := w.shell.onMain(func() {
		if win, _ := w.ids(); win != 0 {
			objc.Send(win, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		}
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

// Focus brings this window to front and activates the app.
func (w *darwinWindow) Focus() error {
	s := w.shell
	err := s.onMain(func() {
		win, _ := w.ids()
		if win == 0 {
			return
		}
		objc.Send(win, objc.Sel("makeKeyAndOrderFront:"), 0)
		objc.Send(s.appID(), objc.Sel("activateIgnoringOtherApps:"), 1)
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

// Native returns this window's WKWebView object pointer.
func (w *darwinWindow) Native() uintptr {
	_, wv := w.ids()
	return uintptr(wv)
}

// Frame implements desktop.Window: the live frame, top-left screen points.
func (w *darwinWindow) Frame() (desktop.Frame, error) {
	var f desktop.Frame
	var ok bool
	err := w.shell.onMain(func() { f, ok = w.readFrameOnMain() })
	if err != nil {
		return desktop.Frame{}, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if !ok {
		return desktop.Frame{}, errWindowClosed()
	}
	return f, nil
}

// SetFrame implements desktop.Window: setFrame:display: with the bottom-left
// flip. setFrame:display: does not post windowDidMove: (observed on
// this host: the delegate hears user drags, not programmatic frames),
// so the report runs here too, through the same path the delegate
// uses: the store should hear every frame change, whatever moved the
// window.
func (w *darwinWindow) SetFrame(f desktop.Frame) error {
	if f.Width <= 0 || f.Height <= 0 {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "frame width and height must be positive"}
	}
	err := w.shell.onMain(func() {
		win, _ := w.ids()
		if win == 0 {
			return
		}
		objc.SendRect(win, objc.Sel("setFrame:display:"), frameToRect(f, readScreens()), 1)
		w.shell.reportWindowFrame(w)
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

// readFrameOnMain reads the live frame on the main thread (the
// windowDidMove:/windowDidEndLiveResize: delegates and SetFrame run
// there). The report to OnWindowFrame crosses to a goroutine so the
// UI thread never waits on Go.
func (w *darwinWindow) readFrameOnMain() (desktop.Frame, bool) {
	win, _ := w.ids()
	if win == 0 {
		return desktop.Frame{}, false
	}
	return rectToFrame(objc.SendRectRet(win, objc.Sel("frame")), readScreens()), true
}

// reportWindowFrame hands the window's current frame to OnWindowFrame
// on a goroutine. Main thread only (it reads the frame inline).
func (s *darwinShell) reportWindowFrame(w *darwinWindow) {
	s.mu.Lock()
	cb := s.onWindowFrame
	s.mu.Unlock()
	if cb == nil {
		return
	}
	if f, ok := w.readFrameOnMain(); ok {
		go cb(w.id, f)
	}
}

// Close closes the window. The main window's close goes through
// windowShouldClose: like the red button's (Quit, or hide with
// CloseHidesWindow); a secondary window's close unregisters it and
// reports through OnWindowClosed. performClose: is a NO-OP on a
// window whose styleMask lacks the closable bit (AppKit drops the
// close box with the title bar), so a borderless window drives the
// delegate by hand: the bridge's windowShouldClose: IMP runs the same
// routing AppKit would, then close (which bypasses the delegate, as
// always) tears the window down.
func (w *darwinWindow) Close() error {
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

// OpenWindow opens a secondary window navigated to url. The window
// gets its OWN WKWebViewConfiguration: same WKWebsiteDataStore as the
// main window (the boot cookie set by /enter applies), fresh
// WKUserContentController with its own user script from desktop.BootstrapJS(id)
// and its own message-handler registration, so the page learns which
// window it lives in. spec.Style shapes the chrome (mask at init,
// NSPanel class for Panel, setters through applyWindowStyle).
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
			uintptr(objc.NSString(desktop.BootstrapJS(id, s.reduceTransparency.Load()))), 0 /* atDocumentStart */, 0 /* all frames */)
		objc.Send(ucc, objc.Sel("addUserScript:"), uintptr(userScript))

		webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
			objc.Sel("initWithFrame:configuration:"), objc.Rect{W: float64(width), H: float64(height)}, uintptr(config)))
		window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class(windowClass), objc.Sel("alloc"))),
			objc.Sel("initWithContentRect:styleMask:backing:defer:"),
			objc.Rect{W: float64(width), H: float64(height)},
			mask, 2 /* buffered */, 0))
		objc.Send(window, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(window, objc.Sel("setDelegate:"), uintptr(bridge))
		objc.Send(webView, objc.Sel("setNavigationDelegate:"), uintptr(bridge))
		applyWindowStyle(window, webView, spec.Style, height)
		// The material before the window fronts, like the main window.
		chrome := applyWindowMaterial(window, webView, spec.Style, spec.SidebarWidth, s.reduceTransparency.Load(), s.logger)
		// A remembered (or explicit) frame wins over the style origin;
		// one from a screen that is gone centers.
		applyRememberedFrame(window, spec.Frame)
		if spec.Style.Panel {
			// A non-activating panel fronts without taking key or
			// activating the app: a floating widget must not steal
			// focus from whatever the user was doing.
			objc.Send(window, objc.Sel("orderFront:"), 0)
		} else {
			objc.Send(window, objc.Sel("makeKeyAndOrderFront:"), 0)
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

// lookupWindow finds the per-window state for a native NSWindow (the
// windowShouldClose: sender).
func (s *darwinShell) lookupWindow(win objc.ID) (*darwinWindow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[win]
	return w, ok
}

// windowByID is the e2e/test seam: the live window with a given id.
func (s *darwinShell) windowByID(id string) (*darwinWindow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windowsByID[id]
	return w, ok
}

// forgetWindow drops a secondary window's registration.
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

// startWindowDrag begins a native drag on the window that owns the
// ucc whose controller fired the script message. WKWebView invokes
// the handler on the main thread while the mouse-down is still the
// current event, which is the whole reason the drag rides this
// channel and not the HTTP bridge. setMovableByWindowBackground: is
// no substitute: the web view covers the window's background.
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
