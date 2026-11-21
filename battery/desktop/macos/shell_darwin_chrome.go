//go:build darwin && (arm64 || amd64)

package macos

import (
	"log/slog"
	"math"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

const (
	effectMaterialSidebar     uintptr = 7
	effectMaterialWindow      uintptr = 12
	effectMaterialUnderWindow uintptr = 21
)

const effectBlendBehindWindow uintptr = 0

const (
	viewWidthSizable  uintptr = 2
	viewHeightSizable uintptr = 16
)

const toolbarStyleUnified uintptr = 3

func toolbarStyleName(v uintptr) string {
	switch v {
	case 0:
		return "automatic"
	case 1:
		return "expanded"
	case 2:
		return "preference"
	case 3:
		return "unified"
	case 4:
		return "unifiedCompact"
	}
	return ""
}

func titleVisibilityName(v uintptr) string {
	switch v {
	case 0:
		return "visible"
	case 1:
		return "hidden"
	}
	return ""
}

const glassStyleRegular uintptr = 0

const appKitGlassVersion = 2685.0

func glassAvailable() bool {
	v, ok := objc.GlobalDouble("NSAppKitVersionNumber")
	return ok && v >= appKitGlassVersion
}

func resolveMaterial(m desktop.WindowMaterial, glass bool) desktop.WindowMaterial {
	switch m {
	case desktop.MaterialSidebar:
		return desktop.MaterialSidebar
	case desktop.MaterialWindow, desktop.MaterialGlass:
		if glass {
			return desktop.MaterialGlass
		}
		return desktop.MaterialWindow
	default:
		return desktop.MaterialNone
	}
}

type windowChrome struct {
	resolved  desktop.WindowMaterial
	inset     *desktop.Inset
	glassView objc.ID
	zoneView  objc.ID
	underView objc.ID
	sidebarW  int
}

func applyWindowMaterial(win, webView objc.ID, style desktop.WindowStyle, sidebarWidth int, reduced bool, logger *slog.Logger) windowChrome {
	var ch windowChrome
	ch.inset = style.TrafficLightInset
	ch.sidebarW = sidebarWidth
	if style.Material == desktop.MaterialNone {
		return ch
	}
	glass := glassAvailable()
	if style.Material == desktop.MaterialGlass && !glass {
		logger.Info("desktop: MaterialGlass needs macOS 26 (AppKit 2685.0); applying the window material instead")
	}
	ch.resolved = resolveMaterial(style.Material, glass)
	switch ch.resolved {
	case desktop.MaterialGlass:
		glassView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSGlassEffectView"), objc.Sel("alloc"))),
			objc.Sel("initWithFrame:"), contentBounds(win)))
		objc.Send(glassView, objc.Sel("setStyle:"), glassStyleRegular)
		objc.Send(glassView, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(win, objc.Sel("setContentView:"), uintptr(glassView))
		ch.glassView = glassView
	case desktop.MaterialWindow:
		ch.underView = newEffectView(contentBounds(win), effectMaterialUnderWindow, viewWidthSizable|viewHeightSizable)
		insertBelow(webView, ch.underView)
	case desktop.MaterialSidebar:
		ch.underView = newEffectView(contentBounds(win), effectMaterialUnderWindow, viewWidthSizable|viewHeightSizable)
		insertBelow(webView, ch.underView)
		if sidebarWidth > 0 {
			ch.zoneView = newEffectView(objc.Rect{W: float64(sidebarWidth), H: contentBounds(win).H}, effectMaterialSidebar, viewHeightSizable)
			insertBelow(webView, ch.zoneView)
		}
	}
	if ch.resolved != desktop.MaterialNone {
		objc.Send(win, objc.Sel("setOpaque:"), 0)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
		setWebViewBackground(webView, false)
	}
	if reduced {
		ch.setReduced(win, webView, true)
	}
	return ch
}

func contentBounds(win objc.ID) objc.Rect {
	cv := objc.ID(objc.Send(win, objc.Sel("contentView")))
	if cv == 0 {
		return objc.Rect{}
	}
	return objc.SendRectRet(cv, objc.Sel("bounds"))
}

func newEffectView(frame objc.Rect, material, autoresizing uintptr) objc.ID {
	view := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSVisualEffectView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:"), frame))
	objc.Send(view, objc.Sel("setMaterial:"), material)
	objc.Send(view, objc.Sel("setBlendingMode:"), effectBlendBehindWindow)
	objc.Send(view, objc.Sel("setAutoresizingMask:"), autoresizing)
	return view
}

func insertBelow(webView, view objc.ID) {
	cv := objc.ID(objc.Send(webView, objc.Sel("superview")))
	if cv == 0 {
		return
	}
	objc.Send(cv, objc.Sel("addSubview:positioned:relativeTo:"), uintptr(view), ^uintptr(0) /* NSWindowBelow */, uintptr(webView))
}

func setWebViewBackground(webView objc.ID, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	objc.Send(webView, objc.Sel("setValue:forKey:"),
		objc.Send(objc.Class("NSNumber"), objc.Sel("numberWithBool:"), v),
		uintptr(objc.NSString("drawsBackground")))
}

func (ch *windowChrome) setReduced(win, webView objc.ID, on bool) {
	switch {
	case ch.resolved == desktop.MaterialGlass && on:
		objc.Send(win, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(win, objc.Sel("setOpaque:"), 1)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("windowBackgroundColor")))
		setWebViewBackground(webView, true)
	case ch.resolved == desktop.MaterialGlass && !on:
		objc.Send(ch.glassView, objc.Sel("setContentView:"), 0)
		objc.Send(ch.glassView, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(win, objc.Sel("setContentView:"), uintptr(ch.glassView))
		objc.Send(win, objc.Sel("setOpaque:"), 0)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
		setWebViewBackground(webView, false)
	case ch.resolved != desktop.MaterialNone:
		if on {
			objc.Send(win, objc.Sel("setOpaque:"), 1)
			objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("windowBackgroundColor")))
			setWebViewBackground(webView, true)
		} else {
			objc.Send(win, objc.Sel("setOpaque:"), 0)
			objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
			setWebViewBackground(webView, false)
		}
		if ch.zoneView != 0 {
			objc.Send(ch.zoneView, objc.Sel("setHidden:"), boolToIMP(on))
		}
		if ch.underView != 0 {
			objc.Send(ch.underView, objc.Sel("setHidden:"), boolToIMP(on))
		}
	}
}

func boolToIMP(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func (ch *windowChrome) setSidebarWidth(win objc.ID, points int) {
	ch.sidebarW = points
	if ch.zoneView == 0 {
		if ch.resolved != desktop.MaterialSidebar || points <= 0 {
			return
		}
		ch.zoneView = newEffectView(objc.Rect{W: float64(points), H: contentBounds(win).H}, effectMaterialSidebar, viewHeightSizable)
		insertBelow(objc.ID(objc.Send(win, objc.Sel("contentView"))), ch.zoneView)
		objc.Send(ch.zoneView, objc.Sel("setHidden:"), objc.Send(win, objc.Sel("isOpaque")))
	}
	objc.SendRect(ch.zoneView, objc.Sel("setFrame:"), objc.Rect{W: float64(points), H: contentBounds(win).H})
}

func applyTrafficLightInset(win objc.ID, inset *desktop.Inset) {
	if inset == nil {
		return
	}
	for i := range 3 {
		btn := objc.ID(objc.Send(win, objc.Sel("standardWindowButton:"), uintptr(i)))
		if btn == 0 {
			continue
		}
		objc.SendF(btn, objc.Sel("setFrameOrigin:"), nil, []float64{float64(inset.X), float64(inset.Y)})
	}
}

func materialState(win objc.ID) (material string, sidebarWidth int) {
	cv := objc.ID(objc.Send(win, objc.Sel("contentView")))
	if cv == 0 {
		return "none", 0
	}
	if classDescription(cv) == "NSGlassEffectView" {
		return "glass", 0
	}
	holder := objc.ID(objc.Send(cv, objc.Sel("superview")))
	if holder == 0 {
		return "none", 0
	}
	material, sidebarWidth = "none", 0
	subs := objc.ID(objc.Send(holder, objc.Sel("subviews")))
	if subs == 0 {
		return material, sidebarWidth
	}
	n := int(objc.Send(subs, objc.Sel("count")))
	if n > 32 {
		n = 32
	}
	for i := range n {
		sub := objc.ID(objc.Send(subs, objc.Sel("objectAtIndex:"), uintptr(i)))
		if sub == 0 || classDescription(sub) != "NSVisualEffectView" {
			continue
		}
		if objc.Send(sub, objc.Sel("material")) == effectMaterialSidebar {
			material = "vibrancy-sidebar"
			sidebarWidth = int(math.Round(objc.SendRectRet(sub, objc.Sel("frame")).W))
			continue
		}
		if material == "none" {
			material = "vibrancy-window"
		}
	}
	return material, sidebarWidth
}

func classDescription(obj objc.ID) string {
	return objc.GoString(objc.ID(objc.Send(objc.ID(objc.Send(obj, objc.Sel("class"))), objc.Sel("description"))))
}

const reduceTransparencyNote = "NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification"

func (s *darwinShell) readReduceTransparency() {
	ws := objc.Send(objc.Class("NSWorkspace"), objc.Sel("sharedWorkspace"))
	s.reduceTransparency.Store(objc.SendBool(objc.ID(ws), objc.Sel("accessibilityDisplayShouldReduceTransparency")))
}

func (s *darwinShell) installAppearanceObserver() {
	ws := objc.Send(objc.Class("NSWorkspace"), objc.Sel("sharedWorkspace"))
	nc := objc.Send(objc.Class("NSNotificationCenter"), objc.Sel("defaultCenter"))
	objc.Send(objc.ID(nc), objc.Sel("addObserver:selector:name:object:"),
		uintptr(s.bridgeID()), uintptr(objc.Sel("workspaceAccessibilityChanged:")),
		uintptr(objc.NSString(reduceTransparencyNote)), uintptr(ws))
}

func (s *darwinShell) Appearance() desktop.Appearance {
	return desktop.Appearance{ReduceTransparency: s.reduceTransparency.Load()}
}

func (s *darwinShell) appearanceChanged(on bool) {
	if s.reduceTransparency.Swap(on) == on {
		return
	}
	s.mu.Lock()
	wins := make([]*darwinWindow, 0, len(s.windows))
	for _, w := range s.windows {
		wins = append(wins, w)
	}
	cb := s.onAppearance
	s.mu.Unlock()
	for _, w := range wins {
		w.applyReduced(on)
	}
	if cb != nil {
		deliverOnGoroutine(func() { cb(desktop.Appearance{ReduceTransparency: on}) })
	}
}

func (w *darwinWindow) applyReduced(on bool) {
	_ = w.shell.onMain(func() {
		win, wv := w.ids()
		if win == 0 {
			return
		}
		w.mu.Lock()
		ch := w.chrome
		w.mu.Unlock()
		ch.setReduced(win, wv, on)
	})
}

func (w *darwinWindow) SetSidebarWidth(points int) error {
	if err := desktop.ValidateSidebarWidth(points); err != nil {
		return err
	}
	return w.withLiveWindow(func(win objc.ID) {
		w.mu.Lock()
		w.chrome.setSidebarWidth(win, points)
		w.mu.Unlock()
	})
}

func (w *darwinWindow) reapplyTrafficInset() {
	win, _ := w.ids()
	if win == 0 {
		return
	}
	w.mu.Lock()
	inset := w.chrome.inset
	w.mu.Unlock()
	applyTrafficLightInset(win, inset)
}

func (s *darwinShell) reportWindowActivity(w *darwinWindow, focus bool) {
	s.mu.Lock()
	focusCB, blurCB := s.onWindowFocus, s.onWindowBlur
	s.mu.Unlock()
	id := w.ID()
	if focus && focusCB != nil {
		deliverOnGoroutine(func() { focusCB(id) })
	}
	if !focus && blurCB != nil {
		deliverOnGoroutine(func() { blurCB(id) })
	}
}
