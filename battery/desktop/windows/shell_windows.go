//go:build windows && amd64

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

const (
	windowClassName = "GofastrDesktopWebView2"
	wmRunOnMain     = win32.WM_APP + 71
	wmTrayNotify    = win32.WM_APP + 72
	wmQuitApp       = win32.WM_APP + 73
	wmCancelModal   = win32.WM_APP + 74
	wmClose         = 0x0010
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmMove          = 0x0003
	wmMoving        = 0x0216
	wmMouseActivate = 0x0021
	wmActivate      = 0x0006
	wmDPIChanged    = 0x02E0
	wmCommand       = 0x0111
	wmNCCalcSize    = 0x0083
	wmNCHitTest     = 0x0084
	wmNCPaint       = 0x0085
	wmNCActivate    = 0x0086
	wmNCMouseMove   = 0x00A0
	wmNCMouseLeave  = 0x02A2
	wmNCLButtonDown = 0x00A1
	wmNCLButtonUp   = 0x00A2
	wmNCLButtonDbl  = 0x00A3
	wmNCRButtonDown = 0x00A4
	wmNCRButtonUp   = 0x00A5
	wmNCRButtonDbl  = 0x00A6
	waInactive      = 0
	waActive        = 1
	waClickActive   = 2
	sizeMinimized   = 1
	webviewTimeout  = 20 * time.Second
)

type win32Rect struct{ Left, Top, Right, Bottom int32 }

type nccalcSizeParams struct {
	Rects     [3]win32Rect
	WindowPos uintptr
}

const (
	iidCreateEnvironmentHandler = "4E8A3389-C9D8-4BD2-B6B5-124FEE6CC14D"
	iidCreateControllerHandler  = "6C4819F3-C9B7-4260-8127-C9F5BDE7F68C"
	iidWebViewController2       = "C979903E-D4CA-4228-92EB-47EE3FA96EAB"
	iidWebViewSettings9         = "0528A73B-E92D-49F4-927A-E547DDDAA37D"
	iidWebMessageHandler        = "57213F19-00E6-49FA-8E07-898EA01ECBD2"
	iidAddScriptHandler         = "B99369F3-9B11-47B5-BC6F-8E7895FCEA17"
	iidExecuteScriptHandler     = "49511172-CC67-4BCA-9923-137112F4C4CC"
	iidCapturePreviewHandler    = "697E05E9-3D8F-45FA-96F4-8FFE1EDEDAF5"
)

var (
	activeWindows sync.Map // HWND -> *winWindow
	activeShells  sync.Map // HWND -> *winShell
	windowProc    = syscall.NewCallback(wndProc)
)

type evalResult struct {
	json string
	err  error
}

type winShell struct {
	mu sync.RWMutex

	hwnd          uintptr
	uiThreadID    uint32
	title         string
	environment   uintptr
	loader        uintptr
	started       bool
	running       bool
	quitBeforeRun atomic.Bool
	closeHides    bool
	trayTitle     string
	tray          *trayState
	appearance    desktop.Appearance

	windowsByID map[string]*winWindow
	windowsHWND map[uintptr]*winWindow
	menuActions map[uint16]menuAction
	actionIDs   []string
	onMenu      func(string)
	onSettings  func()
	onClosed    func(string)
	onFrame     func(string, desktop.Frame)
	onFocus     func(string)
	onBlur      func(string)
	onDeepLink  func(string)
	ready       func(desktop.Window)
	runErr      error

	promptMu     sync.Mutex
	promptQueue  []desktop.Decision
	promptGate   chan struct{}
	modalMu      sync.Mutex
	modalCancels map[uintptr]func()
	notifyMu     sync.Mutex
	notifyLog    []desktop.Notification

	workMu sync.Mutex
	workID atomic.Uintptr
	work   map[uintptr]func()
}

type winWindow struct {
	shell               *winShell
	id                  string
	hwnd                uintptr
	controller          uintptr
	webview             uintptr
	title               string
	style               desktop.WindowStyle
	titlebarTransparent bool
	frame               desktop.Frame
	sidebarWidth        int
	material            string
	pendingURL          string
	maximized           bool
	closed              atomic.Bool
}

type menuAction struct {
	role           string
	id             string
	path           []string
	label          string
	key            string
	mask           uintptr
	firstResponder string
}

// New returns a lazy WebView2 shell. Loading the WebView2 loader and
// initializing COM are deferred until Run owns the locked UI thread.
func New() desktop.Shell { return &winShell{} }

func (s *winShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	if s.quitBeforeRun.Swap(false) {
		return nil
	}
	_ = win32.EnablePerMonitorV2DPI()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s.uiThreadID = win32.CurrentThreadID()
	if err := win32.CoInitializeSTA(); err != nil {
		return fmt.Errorf("desktop/windows: initialize COM STA: %w", err)
	}
	defer win32.CoUninitialize()

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("desktop/windows: Run called twice")
	}
	s.started = true
	s.running = true
	s.title = cfg.Title
	s.closeHides = cfg.Tray != nil && cfg.Tray.CloseHidesWindow
	s.ready = ready
	s.onMenu = cfg.OnMenu
	s.onSettings = cfg.OnSettings
	s.onClosed = cfg.OnWindowClosed
	s.onFrame = cfg.OnWindowFrame
	s.onFocus = cfg.OnWindowFocus
	s.onBlur = cfg.OnWindowBlur
	s.onDeepLink = cfg.OnDeepLink
	s.windowsByID = make(map[string]*winWindow)
	s.windowsHWND = make(map[uintptr]*winWindow)
	s.menuActions = make(map[uint16]menuAction)
	s.work = make(map[uintptr]func())
	s.mu.Unlock()

	if err := win32.RegisterWindowClass(win32.WindowClass{WndProc: windowProc, ClassName: windowClassName, Cursor: win32.LoadCursor(32512)}); err != nil {
		return err
	}
	width, height := cfg.Width, cfg.Height
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}
	style := windowStyleBits(cfg.Style)
	main, err := s.createNativeWindow(desktop.MainWindowID, cfg.Title, width, height, cfg.Style, cfg.Frame, cfg.SidebarWidth, style)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.windowsByID[main.id] = main
	s.windowsHWND[main.hwnd] = main
	s.hwnd = main.hwnd
	s.mu.Unlock()
	activeWindows.Store(main.hwnd, main)
	activeShells.Store(main.hwnd, s)
	if err := s.installMenus(main.hwnd, cfg.Title, cfg.Menu, cfg.Settings != nil); err != nil {
		s.closeRunError(err)
	}
	if main.customFrame() {
		// Recalculate after attaching the menu so the custom frame can
		// reserve the native caption and menu bands above the WebView.
		win32.FrameChanged(main.hwnd)
	}
	if cfg.Tray != nil {
		if s.trayTitle == "" {
			s.trayTitle = cfg.Tray.Title
		}
		s.installTray(cfg.Tray)
	}
	showNativeWindow(main)
	win32.UpdateWindow(main.hwnd)

	loader, err := findWebView2Loader()
	if err != nil {
		s.closeRunError(err)
	} else {
		s.loader = loader
		userData := webviewUserDataDir(cfg.Title)
		if err := os.MkdirAll(userData, 0o700); err != nil {
			s.closeRunError(fmt.Errorf("desktop/windows: create WebView2 data folder: %w", err))
		} else {
			h, e := win32.NewCOMHandler(iidCreateEnvironmentHandler, func(hr, env uintptr) uintptr {
				if int32(hr) < 0 || env == 0 {
					s.closeRunError(win32.HRESULTError(int32(hr)))
					return 0
				}
				_, _ = win32.COMCall(env, 1) // AddRef: keep the environment for secondary windows.
				s.mu.Lock()
				s.environment = env
				s.mu.Unlock()
				s.createController(main, env)
				return 0
			})
			if e != nil {
				s.closeRunError(e)
			} else {
				e = win32.CreateCoreWebView2EnvironmentWithOptions(loader, userData, h)
				win32.Release(h)
				if e != nil {
					s.closeRunError(e)
				}
			}
		}
	}

	go func() {
		select {
		case <-ctx.Done():
			s.Quit()
		case <-s.runEnded():
		}
	}()
	var msg win32.Message
	for {
		ok, e := win32.GetMessage(&msg)
		if e != nil {
			s.closeRunError(e)
			break
		}
		if !ok {
			break
		}
		if s.handleAccelerator(&msg) {
			continue
		}
		win32.TranslateDispatch(&msg)
		s.mu.RLock()
		runErr := s.runErr
		s.mu.RUnlock()
		if runErr != nil {
			break
		}
	}
	s.mu.Lock()
	s.running = false
	mainHwnd := s.hwnd
	wins := make([]*winWindow, 0, len(s.windowsByID))
	for _, w := range s.windowsByID {
		wins = append(wins, w)
	}
	env := s.environment
	s.environment = 0
	s.mu.Unlock()
	for _, w := range wins {
		s.destroyWebView(w)
	}
	if env != 0 {
		win32.Release(env)
	}
	if s.tray != nil {
		s.removeTrayIcon()
	}
	if mainHwnd != 0 {
		activeWindows.Delete(mainHwnd)
		activeShells.Delete(mainHwnd)
	}
	s.mu.RLock()
	err = s.runErr
	s.mu.RUnlock()
	return err
}

func (s *winShell) runEnded() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for {
			s.mu.RLock()
			running := s.running
			s.mu.RUnlock()
			if !running {
				close(ch)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return ch
}

func (s *winShell) closeRunError(err error) {
	if err == nil {
		err = errors.New("desktop/windows: WebView2 initialization failed")
	}
	s.mu.Lock()
	if s.runErr == nil {
		s.runErr = err
	}
	hwnd := s.hwnd
	s.mu.Unlock()
	if hwnd != 0 {
		_ = win32.PostMessage(hwnd, wmQuitApp, 0, 0)
	}
}

func (s *winShell) Quit() {
	s.mu.RLock()
	hwnd, running := s.hwnd, s.running
	s.mu.RUnlock()
	if !running {
		s.quitBeforeRun.Store(true)
		return
	}
	if hwnd != 0 {
		_ = win32.PostMessage(hwnd, wmQuitApp, 0, 0)
	}
}

func (s *winShell) Main(fn func()) error {
	if fn == nil {
		return nil
	}
	if win32.CurrentThreadID() == s.uiThreadID && s.uiThreadID != 0 {
		fn()
		return nil
	}
	s.mu.RLock()
	hwnd, running := s.hwnd, s.running
	s.mu.RUnlock()
	if !running || hwnd == 0 {
		return errors.New("desktop/windows: UI thread is not running")
	}
	id := s.workID.Add(1)
	done := make(chan struct{})
	s.workMu.Lock()
	s.work[id] = func() { defer close(done); fn() }
	s.workMu.Unlock()
	if err := win32.PostMessage(hwnd, wmRunOnMain, id, 0); err != nil {
		s.workMu.Lock()
		delete(s.work, id)
		s.workMu.Unlock()
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(15 * time.Second):
		s.workMu.Lock()
		delete(s.work, id)
		s.workMu.Unlock()
		return &desktop.Error{Code: desktop.CodeInternal, Message: "UI thread operation timed out"}
	}
}

func (s *winShell) dispatch(id uintptr) {
	s.workMu.Lock()
	fn := s.work[id]
	delete(s.work, id)
	s.workMu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *winShell) registerModalCancel(id uintptr, cancel func()) {
	s.modalMu.Lock()
	if s.modalCancels == nil {
		s.modalCancels = make(map[uintptr]func())
	}
	s.modalCancels[id] = cancel
	s.modalMu.Unlock()
}

func (s *winShell) unregisterModalCancel(id uintptr) {
	s.modalMu.Lock()
	delete(s.modalCancels, id)
	s.modalMu.Unlock()
}

func (s *winShell) cancelModal(id uintptr) {
	s.modalMu.Lock()
	cancel := s.modalCancels[id]
	s.modalMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if msg == wmRunOnMain {
		if x, ok := activeShells.Load(hwnd); ok {
			x.(*winShell).dispatch(wparam)
		}
		return 0
	}
	if msg == wmTrayNotify {
		if x, ok := activeShells.Load(hwnd); ok {
			x.(*winShell).onTrayMessage(lparam)
		}
		return 0
	}
	if msg == wmDPIChanged && lparam != 0 {
		rect := (*win32Rect)(unsafe.Pointer(lparam))
		_ = win32.SetWindowPos(hwnd, rect.Left, rect.Top, rect.Right-rect.Left, rect.Bottom-rect.Top, win32.SWP_NOZORDER|win32.SWP_NOACTIVATE)
		return 0
	}
	if value, ok := activeWindows.Load(hwnd); ok {
		w := value.(*winWindow)
		if w.customFrame() {
			if msg == wmNCCalcSize && wparam != 0 {
				params := (*nccalcSizeParams)(unsafe.Pointer(lparam))
				if win32.IsZoomed(hwnd) {
					// Let Windows retain the maximized monitor insets and native
					// menu band. Only menu-less windows extend into the caption.
					result := win32.DefWindowProc(hwnd, msg, wparam, lparam)
					if win32.Menu(hwnd) == 0 {
						params.Rects[0].Top -= customCaptionHeight(windowDPI(hwnd))
					}
					return result
				}
				// Keep the DWM caption and native menu bands outside the
				// WebView client area. Without this inset, the child WebView
				// covers the menu bar when the standard frame is removed.
				if win32.Menu(hwnd) != 0 {
					params.Rects[0].Top += customCaptionHeight(windowDPI(hwnd)) +
						win32.SystemMetricForDPI(15 /* SM_CYMENU */, windowDPI(hwnd))
				}
				return 0
			}
			if isDWMFrameMessage(msg) {
				if result, handled := win32.DwmDefWindowProc(hwnd, msg, wparam, lparam); handled {
					return result
				}
			}
			if msg == wmNCHitTest {
				return hitTestCustomFrame(w, lparam)
			}
		}
	}
	if msg == wmMouseActivate {
		if value, ok := activeWindows.Load(hwnd); ok && value.(*winWindow).style.Panel {
			return win32.MA_NOACTIVATE
		}
	}
	if msg == wmCancelModal {
		if x, ok := activeShells.Load(hwnd); ok {
			x.(*winShell).cancelModal(wparam)
		}
		return 0
	}
	if msg == wmQuitApp {
		win32.DestroyWindow(hwnd)
		return 0
	}
	if msg == wmSize {
		if x, ok := activeWindows.Load(hwnd); ok {
			w := x.(*winWindow)
			minimized := wparam == sizeMinimized
			if w.controller != 0 {
				visible := uintptr(1)
				if minimized {
					visible = 0
				}
				_, _ = win32.COMCall(w.controller, 4, visible)
			}
			maximized := win32.IsZoomed(hwnd)
			if w.customFrame() && maximized && !w.maximized {
				// The maximize state is reliable by WM_SIZE. Re-run non-client
				// calculation so DefWindowProc can apply monitor edge insets.
				win32.FrameChanged(hwnd)
			}
			w.maximized = maximized
			if !minimized {
				w.resizeWebView()
			}
		}
	}
	if msg == wmMove || msg == wmMoving {
		if x, ok := activeWindows.Load(hwnd); ok {
			w := x.(*winWindow)
			w.notifyParentPositionChanged()
			if msg == wmMove {
				w.reportFrame()
			}
		}
	}
	if msg == wmSize {
		if x, ok := activeWindows.Load(hwnd); ok {
			x.(*winWindow).reportFrame()
		}
	}
	if msg == wmActivate {
		if x, ok := activeWindows.Load(hwnd); ok {
			w := x.(*winWindow)
			if byte(wparam) == waInactive {
				if w.shell.onBlur != nil {
					go w.shell.onBlur(w.id)
				}
			} else if w.shell.onFocus != nil {
				go w.shell.onFocus(w.id)
			}
		}
	}
	if msg == wmCommand {
		if x, ok := activeShells.Load(hwnd); ok {
			x.(*winShell).handleMenuCommand(uint16(wparam & 0xffff))
		}
		return 0
	}
	if msg == wmClose {
		if x, ok := activeWindows.Load(hwnd); ok {
			w := x.(*winWindow)
			w.shell.mu.RLock()
			runErr := w.shell.runErr
			w.shell.mu.RUnlock()
			if w.id == desktop.MainWindowID && w.shell.closeHides && runErr == nil {
				showWindow(hwnd, 0)
				return 0
			}
		}
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	if msg == wmDestroy {
		if x, ok := activeWindows.Load(hwnd); ok {
			w := x.(*winWindow)
			w.closed.Store(true)
			w.shell.mu.Lock()
			isMain := w.id == desktop.MainWindowID
			cb := w.shell.onClosed
			if !isMain {
				delete(w.shell.windowsByID, w.id)
				delete(w.shell.windowsHWND, hwnd)
			}
			w.shell.mu.Unlock()
			activeWindows.Delete(hwnd)
			if !isMain {
				activeShells.Delete(hwnd)
				if w.webview != 0 {
					win32.Release(w.webview)
					w.webview = 0
				}
				if w.controller != 0 {
					_, _ = win32.COMCall(w.controller, 24)
					win32.Release(w.controller)
					w.controller = 0
				}
				if cb != nil {
					go cb(w.id)
				}
			}
			if isMain {
				win32.PostQuitMessage(0)
			}
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func showWindow(hwnd uintptr, cmd uintptr) { win32.ShowWindowCmd(hwnd, int(cmd)) }

func showNativeWindow(w *winWindow) {
	if w.style.Panel {
		win32.ShowWindowCmd(w.hwnd, win32.SW_SHOWNOACTIVATE)
		return
	}
	win32.ShowWindow(w.hwnd)
}

func (s *winShell) handleAccelerator(msg *win32.Message) bool {
	if msg.Message != 0x0100 && msg.Message != 0x0104 {
		return false
	}
	key := uint16(msg.WParam)
	ctrl := win32.KeyDown(0x11)
	alt := win32.KeyDown(0x12)
	shift := win32.KeyDown(0x10)
	s.mu.RLock()
	actions := make([]menuAction, 0, len(s.menuActions))
	for _, action := range s.menuActions {
		if action.key != "" && action.firstResponder == "" {
			actions = append(actions, action)
		}
	}
	s.mu.RUnlock()
	for _, action := range actions {
		wantCtrl := action.mask&(1<<20) != 0 || action.mask&(1<<18) != 0
		wantAlt := action.mask&(1<<19) != 0
		wantShift := action.mask&(1<<17) != 0
		if ctrl == wantCtrl && alt == wantAlt && shift == wantShift && virtualKey(action.key) == key {
			s.invokeMenuAction(action)
			return true
		}
	}
	return false
}

func virtualKey(key string) uint16 {
	if len(key) == 1 {
		c := key[0]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			return uint16(c)
		}
		switch c {
		case ',':
			return 0xBC
		case '.':
			return 0xBE
		case '/':
			return 0xBF
		case ';':
			return 0xBA
		case '\'':
			return 0xDE
		case '[':
			return 0xDB
		case ']':
			return 0xDD
		case '-':
			return 0xBD
		case '=':
			return 0xBB
		case '`':
			return 0xC0
		case '\\':
			return 0xDC
		}
	}
	return 0
}

func windowStyleBits(style desktop.WindowStyle) uint32 {
	if style.Chrome == desktop.ChromeNone {
		return 0x80000000 | win32.WS_CLIPCHILDREN | win32.WS_CLIPSIBLINGS
	}
	bits := uint32(win32.WS_OVERLAPPEDWINDOW | win32.WS_CLIPCHILDREN | win32.WS_CLIPSIBLINGS)
	if style.Resizable != nil && !*style.Resizable {
		bits &^= win32.WS_THICKFRAME | win32.WS_MAXIMIZEBOX
	}
	return bits
}

func (w *winWindow) customFrame() bool {
	return w.style.Chrome == desktop.ChromeHiddenTitle || w.style.Chrome == desktop.ChromeUnified
}

func isDWMFrameMessage(msg uint32) bool {
	switch msg {
	case wmNCHitTest, wmNCPaint, wmNCActivate, wmNCMouseMove, wmNCMouseLeave,
		wmNCLButtonDown, wmNCLButtonUp, wmNCLButtonDbl,
		wmNCRButtonDown, wmNCRButtonUp, wmNCRButtonDbl:
		return true
	default:
		return false
	}
}

func hitTestCustomFrame(w *winWindow, lparam uintptr) uintptr {
	x := int32(int16(uint16(lparam)))
	y := int32(int16(uint16(lparam >> 16)))
	left, top, right, bottom, ok := win32.GetWindowRect(w.hwnd)
	if !ok {
		return win32.HTClient
	}
	dpi := windowDPI(w.hwnd)
	borderX := win32.SystemMetricForDPI(32 /* SM_CXSIZEFRAME */, dpi) + win32.SystemMetricForDPI(92 /* SM_CXPADDEDBORDER */, dpi)
	borderY := win32.SystemMetricForDPI(33 /* SM_CYSIZEFRAME */, dpi) + win32.SystemMetricForDPI(92 /* SM_CXPADDEDBORDER */, dpi)
	if w.style.Resizable == nil || *w.style.Resizable {
		if !win32.IsZoomed(w.hwnd) {
			leftEdge, rightEdge := x < left+borderX, x >= right-borderX
			topEdge, bottomEdge := y < top+borderY, y >= bottom-borderY
			switch {
			case leftEdge && topEdge:
				return win32.HTTopLeft
			case rightEdge && topEdge:
				return win32.HTTopRight
			case leftEdge && bottomEdge:
				return win32.HTBottomLeft
			case rightEdge && bottomEdge:
				return win32.HTBottomRight
			case leftEdge:
				return win32.HTLeft
			case rightEdge:
				return win32.HTRight
			case topEdge:
				return win32.HTTop
			case bottomEdge:
				return win32.HTBottom
			}
		}
	}
	captionHeight := customCaptionHeight(dpi)
	if y < top+captionHeight {
		return win32.HTCaption
	}
	if win32.Menu(w.hwnd) != 0 && y < top+captionHeight+win32.SystemMetricForDPI(15 /* SM_CYMENU */, dpi) {
		return win32.HTMenu
	}
	return win32.HTClient
}

func customCaptionHeight(dpi uint32) int32 {
	border := win32.SystemMetricForDPI(33 /* SM_CYSIZEFRAME */, dpi) +
		win32.SystemMetricForDPI(92 /* SM_CXPADDEDBORDER */, dpi)
	return win32.SystemMetricForDPI(4 /* SM_CYCAPTION */, dpi) + border
}

func (s *winShell) createNativeWindow(id, title string, width, height int, style desktop.WindowStyle, frame *desktop.Frame, sidebar int, styleBits uint32) (*winWindow, error) {
	const useDefault int32 = -2147483648
	screenDPI := win32.SystemDPI()
	x, y := useDefault, useDefault
	if frame != nil {
		x, y = pointsToPixels(frame.X, screenDPI), pointsToPixels(frame.Y, screenDPI)
		width, height = frame.Width, frame.Height
	} else if style.X != nil && style.Y != nil {
		x, y = pointsToPixels(*style.X, screenDPI), pointsToPixels(*style.Y, screenDPI)
	}
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}
	widthPixels, heightPixels := pointsToPixels(width, screenDPI), pointsToPixels(height, screenDPI)
	exStyle := uint32(0)
	if style.Panel {
		exStyle |= win32.WS_EX_NOACTIVATE
	}
	hwnd, err := win32.CreateWindowEx(exStyle, windowClassName, title, styleBits, x, y, widthPixels, heightPixels, 0)
	if err != nil {
		return nil, err
	}
	actualDPI := windowDPI(hwnd)
	desiredWidth, desiredHeight := pointsToPixels(width, actualDPI), pointsToPixels(height, actualDPI)
	if int32(widthPixels) != desiredWidth || int32(heightPixels) != desiredHeight {
		if err := win32.SetWindowPos(hwnd, 0, 0, desiredWidth, desiredHeight, win32.SWP_NOMOVE|win32.SWP_NOZORDER|win32.SWP_NOACTIVATE); err != nil {
			win32.DestroyWindow(hwnd)
			return nil, err
		}
	}
	w := &winWindow{shell: s, id: id, hwnd: hwnd, title: title, style: style, sidebarWidth: sidebar}
	if left, top, right, bottom, ok := win32.GetWindowRect(hwnd); ok {
		w.frame = frameFromWindowRect(hwnd, left, top, right, bottom)
	}
	w.material = applyWindowMaterial(hwnd, style.Material)
	w.titlebarTransparent = applyWindowChrome(hwnd, style)
	if style.Float || style.Panel {
		setTopMost(hwnd)
	}
	return w, nil
}

func (s *winShell) windowInitializationFailed(w *winWindow, err error) {
	if err == nil {
		err = errors.New("desktop/windows: WebView2 initialization failed")
	}
	if w == nil || w.id == desktop.MainWindowID {
		s.closeRunError(err)
		return
	}
	if w.hwnd != 0 && win32.IsWindow(w.hwnd) {
		if win32.CurrentThreadID() == s.uiThreadID {
			win32.DestroyWindow(w.hwnd)
		} else {
			_ = win32.PostMessage(w.hwnd, wmQuitApp, 0, 0)
		}
	}
}

func (s *winShell) createController(w *winWindow, env uintptr) error {
	cb, err := win32.NewCOMHandler(iidCreateControllerHandler, func(hr, controller uintptr) uintptr {
		if w.closed.Load() || w.hwnd == 0 || !win32.IsWindow(w.hwnd) {
			if controller != 0 {
				_, _ = win32.COMCall(controller, 24) // Close a late-created controller.
			}
			return 0
		}
		if int32(hr) < 0 || controller == 0 {
			failure := win32.HRESULTError(int32(hr))
			if controller == 0 && int32(hr) >= 0 {
				failure = errors.New("desktop/windows: WebView2 controller creation returned no controller")
			} else if controller != 0 {
				_, _ = win32.COMCall(controller, 24)
			}
			s.windowInitializationFailed(w, failure)
			return 0
		}
		_, _ = win32.COMCall(controller, 1)
		var webview uintptr
		_, err := win32.COMCall(controller, 25, uintptr(unsafe.Pointer(&webview)))
		if err != nil || webview == 0 {
			if webview != 0 {
				win32.Release(webview)
			}
			_, _ = win32.COMCall(controller, 24)
			win32.Release(controller)
			if err == nil {
				err = errors.New("desktop/windows: WebView2 controller returned no web view")
			}
			s.windowInitializationFailed(w, fmt.Errorf("desktop/windows: get WebView2 view: %w", err))
			return 0
		}
		w.controller, w.webview = controller, webview
		if w.style.Transparent || w.style.Material != desktop.MaterialNone || w.style.Chrome == desktop.ChromeHiddenTitle || w.style.Chrome == desktop.ChromeUnified {
			if c2, e := win32.QueryInterface(controller, iidWebViewController2); e == nil {
				_, _ = win32.COMCall(c2, 27, 0) // transparent COREWEBVIEW2_COLOR (A=0)
				win32.Release(c2)
			}
		}
		s.enableWebViewChrome(w)
		w.resizeWebView()
		_, _ = win32.COMCall(controller, 4, 1)
		s.installBootstrap(w)
		return 0
	})
	if err != nil {
		s.windowInitializationFailed(w, err)
		return err
	}
	_, err = win32.COMCall(env, 3, w.hwnd, cb)
	win32.Release(cb)
	if err != nil {
		err = fmt.Errorf("desktop/windows: create WebView2 controller: %w", err)
		s.windowInitializationFailed(w, err)
		return err
	}
	return nil
}

func (s *winShell) enableWebViewChrome(w *winWindow) {
	if w.webview == 0 || w.style.Chrome == desktop.ChromeDefault {
		return
	}
	var settings uintptr
	_, settingsErr := win32.COMCall(w.webview, 3 /* get_Settings */, uintptr(unsafe.Pointer(&settings)))
	if settingsErr == nil && settings != 0 {
		if settings9, queryErr := win32.QueryInterface(settings, iidWebViewSettings9); queryErr == nil {
			_, _ = win32.COMCall(settings9, 38 /* put_IsNonClientRegionSupportEnabled */, 1)
			win32.Release(settings9)
		}
	}
	if settings != 0 {
		win32.Release(settings)
	}

	cb, err := win32.NewCOMHandler(iidWebMessageHandler, func(sender, args uintptr) uintptr {
		if sender != w.webview || args == 0 {
			return 0
		}
		var message uintptr
		if _, err := win32.COMCall(args, 5 /* TryGetWebMessageAsString */, uintptr(unsafe.Pointer(&message))); err != nil {
			return 0
		}
		text := win32.ReadUTF16(message, 1024)
		if message != 0 {
			win32.CoTaskMemFree(message)
		}
		if text == `{"type":"drag"}` && w.hwnd != 0 && !w.closed.Load() {
			win32.ReleaseCapture()
			// Let this WebView2 callback return before DefWindowProc starts
			// the native move loop; nested loops in WebView2 callbacks hang it.
			_ = win32.PostMessage(w.hwnd, wmNCLButtonDown, win32.HTCaption, 0)
		}
		return 0
	})
	if err != nil {
		s.windowInitializationFailed(w, err)
		return
	}
	var token int64
	_, addErr := win32.COMCall(w.webview, 34 /* add_WebMessageReceived */, cb, uintptr(unsafe.Pointer(&token)))
	win32.Release(cb)
	if addErr != nil {
		s.windowInitializationFailed(w, addErr)
	}
}

func (s *winShell) installBootstrap(w *winWindow) {
	if w.closed.Load() {
		return
	}
	if w.webview == 0 {
		s.windowInitializationFailed(w, errors.New("desktop/windows: cannot install page bootstrap without a WebView"))
		return
	}
	script, err := win32.UTF16Ptr(desktop.BootstrapJS(w.id, false))
	if err != nil {
		s.windowInitializationFailed(w, err)
		return
	}
	cb, err := win32.NewCOMHandler(iidAddScriptHandler, func(hr, _ uintptr) uintptr {
		if w.closed.Load() {
			return 0
		}
		if int32(hr) < 0 {
			s.windowInitializationFailed(w, win32.HRESULTError(int32(hr)))
			return 0
		}
		if w.id == desktop.MainWindowID {
			if s.ready != nil {
				s.ready(w)
			}
		} else if w.pendingURL != "" {
			if err := w.Navigate(w.pendingURL); err != nil {
				s.windowInitializationFailed(w, err)
			}
		}
		return 0
	})
	if err != nil {
		s.windowInitializationFailed(w, err)
		return
	}
	_, err = win32.COMCall(w.webview, 27, uintptr(unsafe.Pointer(script)), cb)
	win32.Release(cb)
	runtime.KeepAlive(script)
	if err != nil {
		s.windowInitializationFailed(w, fmt.Errorf("desktop/windows: install page bootstrap: %w", err))
	}
}

func (w *winWindow) resizeWebView() {
	if w.controller == 0 || w.closed.Load() {
		return
	}
	width, height := win32.GetClientSize(w.hwnd)
	bounds := struct{ Left, Top, Right, Bottom int32 }{Right: int32(width), Bottom: int32(height)}
	_, _ = win32.COMCall(w.controller, 6, uintptr(unsafe.Pointer(&bounds)))
}

func (w *winWindow) notifyParentPositionChanged() {
	if w.controller != 0 && !w.closed.Load() {
		_, _ = win32.COMCall(w.controller, 23 /* NotifyParentWindowPositionChanged */)
	}
}

func (w *winWindow) reportFrame() {
	if w.closed.Load() || win32.IsIconic(w.hwnd) || win32.IsZoomed(w.hwnd) {
		return
	}
	l, t, r, b, ok := win32.GetWindowRect(w.hwnd)
	if !ok {
		return
	}
	f := frameFromWindowRect(w.hwnd, l, t, r, b)
	w.frame = f
	if w.shell.onFrame != nil {
		go w.shell.onFrame(w.id, f)
	}
}

func (s *winShell) openWebViewWindow(id string, spec desktop.WindowSpec, url string) (desktop.Window, error) {
	s.mu.RLock()
	if existing := s.windowsByID[id]; existing != nil {
		s.mu.RUnlock()
		return existing, nil
	}
	env := s.environment
	s.mu.RUnlock()
	if env == 0 {
		return nil, errors.New("desktop/windows: WebView2 environment is not ready")
	}
	var opened *winWindow
	var createErr, controllerErr error
	err := s.Main(func() {
		width, height := spec.Width, spec.Height
		if width <= 0 {
			width = 800
		}
		if height <= 0 {
			height = 600
		}
		title := spec.Title
		if title == "" {
			title = s.trayTitle
		}
		opened, createErr = s.createNativeWindow(id, title, width, height, spec.Style, spec.Frame, spec.SidebarWidth, windowStyleBits(spec.Style))
		if createErr != nil || opened == nil {
			return
		}
		opened.pendingURL = url
		s.mu.Lock()
		s.windowsByID[id] = opened
		s.windowsHWND[opened.hwnd] = opened
		s.mu.Unlock()
		activeWindows.Store(opened.hwnd, opened)
		activeShells.Store(opened.hwnd, s)
		if opened.customFrame() {
			win32.FrameChanged(opened.hwnd)
		}
		showNativeWindow(opened)
		win32.UpdateWindow(opened.hwnd)
		controllerErr = s.createController(opened, env)
	})
	if err != nil {
		return nil, err
	}
	if createErr != nil {
		return nil, createErr
	}
	if controllerErr != nil {
		return nil, controllerErr
	}
	if opened == nil {
		return nil, errors.New("desktop/windows: could not create secondary window")
	}
	return opened, nil
}

func (s *winShell) OpenWindow(id string, spec desktop.WindowSpec, url string) (desktop.Window, error) {
	return s.openWebViewWindow(id, spec, url)
}

func (s *winShell) destroyWebView(w *winWindow) {
	if w.webview != 0 {
		win32.Release(w.webview)
		w.webview = 0
	}
	if w.controller != 0 {
		_, _ = win32.COMCall(w.controller, 24)
		win32.Release(w.controller)
		w.controller = 0
	}
	if w.hwnd != 0 && win32.IsWindow(w.hwnd) {
		win32.DestroyWindow(w.hwnd)
	}
	w.hwnd = 0
}

func (s *winShell) Appearance() desktop.Appearance { return s.appearance }

func findWebView2Loader() (uintptr, error) {
	var candidates []string
	if p := os.Getenv("GOFASTR_WEBVIEW2_LOADER"); p != "" {
		candidates = append(candidates, p)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "WebView2Loader.dll"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "WebView2Loader.dll"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return win32.LoadLibrary(p)
		}
	}
	return 0, errors.New("desktop/windows: WebView2Loader.dll was not found; run `gofastr desktop build` or set GOFASTR_WEBVIEW2_LOADER")
}

func webviewUserDataDir(title string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, title)
	if name == "" {
		name = "gofastr"
	}
	return filepath.Join(base, "gofastr", name, "WebView2")
}

// Interface assertions keep the platform build honest.
var (
	_ desktop.Shell         = (*winShell)(nil)
	_ desktop.Window        = (*winWindow)(nil)
	_ desktop.PageEvaluator = (*winWindow)(nil)
	_ desktop.NativeDriver  = (*winShell)(nil)
)
