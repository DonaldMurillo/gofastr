//go:build darwin && arm64

package macos

import (
	"log/slog"
	"math"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The phase 13 window chrome: native materials under the page, the
// unified title bar, the traffic-light inset, and the Reduce
// Transparency handling. Every enum value below was verified against
// the SDK headers on this machine (NSVisualEffectView.h, NSWindow.h,
// NSGlassEffectView.h), not copied from memory.

// NSVisualEffectMaterial (NSVisualEffectView.h): sidebar is the
// material Apple names for window sidebars, underWindowBackground the
// one for the rest of a window, windowBackground the opaque fallback.
const (
	effectMaterialSidebar     uintptr = 7
	effectMaterialWindow      uintptr = 12
	effectMaterialUnderWindow uintptr = 21
)

// NSVisualEffectBlendingMode: behindWindow blends with the desktop
// behind the window (what a window material wants).
const effectBlendBehindWindow uintptr = 0

// NSViewAutoresizingMask bits (NSView.h): the under-window view tracks
// both axes; the sidebar zone tracks the height only (the PAGE owns its
// width through window.setChrome, so a window resize must not stretch
// the zone).
const (
	viewWidthSizable  uintptr = 2
	viewHeightSizable uintptr = 16
)

// NSWindowToolbarStyleUnified (NSWindow.h).
const toolbarStyleUnified uintptr = 3

// toolbarStyleName maps NSWindowToolbarStyle values for WindowState.
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

// NSGlassEffectViewStyleRegular (NSGlassEffectView.h). Clear is the
// other case; the plan keeps it out (private in practice per the
// window-vibrancy source, and it needs a dimming layer).
const glassStyleRegular uintptr = 0

// appKitGlassVersion is NSAppKitVersionNumber at which NSGlassEffectView
// exists; window-vibrancy gates apply_liquid_glass on the same number.
const appKitGlassVersion = 2685.0

// glassAvailable reports whether this AppKit has NSGlassEffectView
// (macOS 26). The gate reads NSAppKitVersionNumber, the exported AppKit
// double, through dlsym: it names the AppKit actually loaded, which is
// the availability that matters, and it is one pointer read (the
// alternative, NSProcessInfo operatingSystemVersion, answers the OS
// build and returns a struct this bridge has no path for).
func glassAvailable() bool {
	v, ok := objc.GlobalDouble("NSAppKitVersionNumber")
	return ok && v >= appKitGlassVersion
}

// resolveMaterial maps the contract's Material onto the mechanism this
// AppKit has: the sidebar zone always answers the zone vibrancy view
// (a partial-zone glass view behind the page is the sibling placement
// Apple says to avoid, and the public glass API has no zone shape),
// while window and glass wrap the page in an NSGlassEffectView on 26
// and fall back to a full-bounds vibrancy view below it.
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

// windowChrome holds the native views one window's material installed.
// The zero value is the opaque window; every field is main-thread-only.
type windowChrome struct {
	// resolved is the mechanism in effect after the platform gate.
	resolved desktop.WindowMaterial
	// inset is the style's TrafficLightInset (nil: system position),
	// kept for the delegate re-applications.
	inset *desktop.Inset
	// glassView is the NSGlassEffectView (resolved glass), 0 otherwise.
	glassView objc.ID
	// zoneView is the sidebar-zone NSVisualEffectView, 0 without a zone.
	zoneView objc.ID
	// underView is the full-bounds NSVisualEffectView, 0 when none.
	underView objc.ID
	// sidebarW is the zone width in points as last set.
	sidebarW int
}

// applyWindowMaterial installs the material style asked for under the
// web view and returns the chrome state. Main thread only; call after
// setContentView made the web view the window's content (the glass
// path replaces the content view with the glass view).
//
// The page must paint html and body transparent for any of it to show;
// that rule is the page contract (docs/desktop-sections/13-chrome.md),
// not something the shell can enforce.
func applyWindowMaterial(win, webView objc.ID, style desktop.WindowStyle, sidebarWidth int, reduced bool, logger *slog.Logger) windowChrome {
	var ch windowChrome
	ch.inset = style.TrafficLightInset
	ch.sidebarW = sidebarWidth
	if style.Material == desktop.MaterialNone {
		return ch
	}
	glass := glassAvailable()
	if style.Material == desktop.MaterialGlass && !glass {
		// One Info line per window, the documented degradation.
		logger.Info("desktop: MaterialGlass needs macOS 26 (AppKit 2685.0); applying the window material instead")
	}
	ch.resolved = resolveMaterial(style.Material, glass)
	switch ch.resolved {
	case desktop.MaterialGlass:
		glassView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSGlassEffectView"), objc.Sel("alloc"))),
			objc.Sel("initWithFrame:"), contentBounds(win)))
		objc.Send(glassView, objc.Sel("setStyle:"), glassStyleRegular)
		// cornerRadius stays at the view's default: AppKit exposes no
		// window radius to read (NSWindow answers NO to cornerRadius/
		// cornerRadii on this host), and a full-content glass view is
		// corner-shaped by the window's own rounded mask.
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
		// The window itself must stop painting behind the effect: the
		// behind-window blending samples through it. The web view's own
		// background goes off through the same private KVC path the
		// Transparent style uses (there is no public switch; see the
		// applyWindowStyle note).
		objc.Send(win, objc.Sel("setOpaque:"), 0)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("clearColor")))
		setWebViewBackground(webView, false)
	}
	if reduced {
		ch.setReduced(win, webView, true)
	}
	return ch
}

// contentBounds is the window's content view bounds. Main thread only.
func contentBounds(win objc.ID) objc.Rect {
	cv := objc.ID(objc.Send(win, objc.Sel("contentView")))
	if cv == 0 {
		return objc.Rect{}
	}
	return objc.SendRectRet(cv, objc.Sel("bounds"))
}

// newEffectView builds one NSVisualEffectView: the material, behind-
// window blending, and the autoresizing mask that keeps it placed as
// the window resizes. The state stays at its default, following the
// window's active state (the material dims itself on deactivate).
func newEffectView(frame objc.Rect, material, autoresizing uintptr) objc.ID {
	view := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSVisualEffectView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:"), frame))
	objc.Send(view, objc.Sel("setMaterial:"), material)
	objc.Send(view, objc.Sel("setBlendingMode:"), effectBlendBehindWindow)
	objc.Send(view, objc.Sel("setAutoresizingMask:"), autoresizing)
	return view
}

// insertBelow adds view to the window's content view, directly below
// the web view (AppKit keeps later inserts nearer the reference view,
// so the sidebar zone lands above the under-window view). Main thread
// only.
func insertBelow(webView, view objc.ID) {
	cv := objc.ID(objc.Send(webView, objc.Sel("superview")))
	if cv == 0 {
		return
	}
	objc.Send(cv, objc.Sel("addSubview:positioned:relativeTo:"), uintptr(view), ^uintptr(0) /* NSWindowBelow */, uintptr(webView))
}

// setWebViewBackground turns the web view's own background painting on
// or off. Off is the private drawsBackground KVC (the applyWindowStyle
// note covers why); on restores the default.
func setWebViewBackground(webView objc.ID, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	objc.Send(webView, objc.Sel("setValue:forKey:"),
		objc.Send(objc.Class("NSNumber"), objc.Sel("numberWithBool:"), v),
		uintptr(objc.NSString("drawsBackground")))
}

// setReduced applies the Reduce Transparency state to one window's
// material views. The effect views are HIDDEN, not set state-inactive:
// an inactive material still renders a flattened translucent surface
// behind a page that paints transparent, while hidden plus a restored
// opaque window background gives exactly the MaterialNone window, one
// code path, no half-flattened look. The glass view swaps out for the
// plain web view the same way. Main thread only.
func (ch *windowChrome) setReduced(win, webView objc.ID, on bool) {
	switch {
	case ch.resolved == desktop.MaterialGlass && on:
		// Unwrap: the web view goes back to being the content view and
		// the window paints its own opaque background.
		objc.Send(win, objc.Sel("setContentView:"), uintptr(webView))
		objc.Send(win, objc.Sel("setOpaque:"), 1)
		objc.Send(win, objc.Sel("setBackgroundColor:"), objc.Send(objc.Class("NSColor"), objc.Sel("windowBackgroundColor")))
		setWebViewBackground(webView, true)
	case ch.resolved == desktop.MaterialGlass && !on:
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

// boolToIMP is the objc BOOL spelling for an argument slot.
func boolToIMP(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// setSidebarWidth resizes the zone view. Main thread only. A width of
// 0 keeps the view at zero size, which is "no zone".
func (ch *windowChrome) setSidebarWidth(win objc.ID, points int) {
	ch.sidebarW = points
	if ch.zoneView == 0 {
		return
	}
	objc.SendRect(ch.zoneView, objc.Sel("setFrame:"), objc.Rect{W: float64(points), H: contentBounds(win).H})
}

// applyTrafficLightInset moves the three standard window buttons to the
// inset (Electron's repositioning recipe; nil keeps the system
// position). AppKit re-lays the buttons out when the window resizes,
// when it becomes key, and on title-bar changes (the macOS 26 quirk
// Electron's proxy comments about), so every one of those paths
// re-applies this. Main thread only.
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

// materialState reads what the OS actually has under the page: glass
// when the content view IS the glass view, otherwise the effect views
// sitting beside the content view in the window's frame view (where
// insertBelow puts them), with the sidebar zone's live width.
// Assertions against this are against AppKit's own objects, not the
// shell's bookkeeping. Main thread only.
func materialState(win objc.ID) (material string, sidebarWidth int) {
	cv := objc.ID(objc.Send(win, objc.Sel("contentView")))
	if cv == 0 {
		return "none", 0
	}
	if classDescription(cv) == "NSGlassEffectView" {
		return "glass", 0
	}
	// The effect views are siblings of the content view (added to its
	// superview, below it), never subviews of the web view itself.
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

// classDescription is the object's class name (the existing
// WindowState spelling).
func classDescription(obj objc.ID) string {
	return objc.GoString(objc.ID(objc.Send(objc.ID(objc.Send(obj, objc.Sel("class"))), objc.Sel("description"))))
}

// ─── the shell's appearance surface ──────────────────────────────────────────

// reduceTransparencyNote is the NSWorkspace notification name (the
// exported NSString* constant spelled as its string; notification
// names compare as strings).
const reduceTransparencyNote = "NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification"

// readReduceTransparency loads the current state from NSWorkspace.
// Main thread only (AppKit).
func (s *darwinShell) readReduceTransparency() {
	ws := objc.Send(objc.Class("NSWorkspace"), objc.Sel("sharedWorkspace"))
	s.reduceTransparency.Store(objc.SendBool(objc.ID(ws), objc.Sel("accessibilityDisplayShouldReduceTransparency")))
}

// installAppearanceObserver watches the workspace accessibility
// notification (it fires for contrast and motion changes too; the
// handler re-reads and only acts when the transparency flag itself
// changed). Main thread only; the bridge must already be installed.
func (s *darwinShell) installAppearanceObserver() {
	ws := objc.Send(objc.Class("NSWorkspace"), objc.Sel("sharedWorkspace"))
	nc := objc.Send(objc.Class("NSNotificationCenter"), objc.Sel("defaultCenter"))
	objc.Send(objc.ID(nc), objc.Sel("addObserver:selector:name:object:"),
		uintptr(s.bridgeID()), uintptr(objc.Sel("workspaceAccessibilityChanged:")),
		uintptr(objc.NSString(reduceTransparencyNote)), uintptr(ws))
}

// Appearance implements desktop.Shell: the live Reduce Transparency
// state, readable before Run returns and before it starts (false
// until the first read).
func (s *darwinShell) Appearance() desktop.Appearance {
	return desktop.Appearance{ReduceTransparency: s.reduceTransparency.Load()}
}

// appearanceChanged is the notification path: re-read, flip every
// window's material views when the flag moved, report on a goroutine.
// Runs on the main thread (the notification arrives there).
func (s *darwinShell) appearanceChanged(on bool) {
	if s.reduceTransparency.Swap(on) == on {
		// A contrast or motion change moved: not ours to push.
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
		go cb(desktop.Appearance{ReduceTransparency: on})
	}
}

// applyReduced applies the Reduce Transparency state to this window's
// material views on the main thread. No-op for the opaque window.
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

// SetSidebarWidth implements desktop.Window: resize this window's
// sidebar zone. The capability has already bounded the value; the
// bound is re-checked because in-process callers skip it.
func (w *darwinWindow) SetSidebarWidth(points int) error {
	if points < 0 || points > desktop.MaxSidebarWidth {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "sidebarWidth must be between 0 and 4096"}
	}
	err := w.shell.onMain(func() {
		win, _ := w.ids()
		if win == 0 {
			return
		}
		w.mu.Lock()
		w.chrome.setSidebarWidth(win, points)
		w.mu.Unlock()
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

// reapplyTrafficInset re-applies the traffic-light geometry after a
// path AppKit re-lays the buttons out on. Main thread only.
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

// reportWindowActivity hands one key-window transition to the
// WindowConfig callbacks on a goroutine (the snapshot-under-lock rule;
// the UI thread never waits on Go). Main thread only.
func (s *darwinShell) reportWindowActivity(w *darwinWindow, focus bool) {
	s.mu.Lock()
	focusCB, blurCB := s.onWindowFocus, s.onWindowBlur
	s.mu.Unlock()
	id := w.ID()
	if focus && focusCB != nil {
		go focusCB(id)
	}
	if !focus && blurCB != nil {
		go blurCB(id)
	}
}

// snapshotWindows copies the live window list under the lock.
func (s *darwinShell) snapshotWindows() []*darwinWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	wins := make([]*darwinWindow, 0, len(s.windows))
	for _, w := range s.windows {
		wins = append(wins, w)
	}
	return wins
}
