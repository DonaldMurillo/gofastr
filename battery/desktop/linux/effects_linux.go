//go:build linux && (amd64 || arm64)

package linux

import (
	"log/slog"
	"runtime"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

// windowEffect is what the shell actually gave a window for its style's
// Transparent and Material requests. Tests read it; the page learns
// nothing new from it, since every fallback is the opaque window a
// MaterialNone window already gets.
type windowEffect int

const (
	effectOpaque      windowEffect = iota // the ordinary GTK window
	effectTransparent                     // RGBA visual, nothing painted under the page
	effectMaterial                        // RGBA visual, a translucent theme tint, compositor blur
)

func (e windowEffect) String() string {
	switch e {
	case effectTransparent:
		return "transparent"
	case effectMaterial:
		return "material"
	}
	return "opaque"
}

// Material tint opacity over the blurred desktop, per material. GTK has no
// vibrancy, so the theme's own window background at these alphas stands in:
// it follows the user's light or dark GTK theme, and the blur behind it keeps
// text readable. Measured by eye under KWin, unverified against any spec.
var materialTint = map[desktop.WindowMaterial]string{
	desktop.MaterialWindow:  "0.88",
	desktop.MaterialSidebar: "0.80",
	desktop.MaterialGlass:   "0.62",
}

// chooseEffect decides a window's effect before it is shown. Transparency
// needs a compositing manager and an RGBA visual. A material also needs
// compositor blur: a tint over the sharp desktop is harder to read than an
// opaque window, so without blur the window stays opaque, as it did before.
func chooseEffect(style desktop.WindowStyle, composited, rgba, blur bool) windowEffect {
	switch {
	case style.Material != desktop.MaterialNone && materialTint[style.Material] != "":
		if composited && rgba && blur {
			return effectMaterial
		}
		if style.Transparent && composited && rgba {
			return effectTransparent
		}
	case style.Transparent:
		if composited && rgba {
			return effectTransparent
		}
	}
	return effectOpaque
}

// kdeBlurAtom is KWin's blur-behind hint. An empty region asks for the
// whole window. KWin lists it in _NET_SUPPORTED when its blur effect is on.
const kdeBlurAtom = "_KDE_NET_WM_BLUR_BEHIND_REGION"

// compositorBlur reports whether the window manager will blur behind a
// window: KWin on X11 with the blur effect enabled. GNOME/Mutter has no blur
// protocol, and Wayland compositors are not probed, so both report false.
func compositorBlur(screen uintptr) bool {
	if gtk.Sym.GdkX11DisplayGetType == 0 || gtk.Sym.GdkX11ScreenSupportsNetWMHint == 0 {
		return false
	}
	display := call(gtk.Sym.GdkDisplayGetDefault)
	if display == 0 || call(gtk.Sym.GTypeCheckInstanceIsA, display, call(gtk.Sym.GdkX11DisplayGetType))&0xff == 0 {
		return false
	}
	var supported bool
	withString(kdeBlurAtom, func(p uintptr) {
		atom := call(gtk.Sym.GdkAtomInternStaticString, p)
		supported = call(gtk.Sym.GdkX11ScreenSupportsNetWMHint, screen, atom)&0xff != 0
	})
	return supported
}

// applyEffectBeforeShow sets the visual and the window's CSS background.
// It must run before the window is realized: GTK fixes a window's visual at
// realize time.
func (w *linuxWindow) applyEffectBeforeShow(style desktop.WindowStyle) {
	if !style.Transparent && style.Material == desktop.MaterialNone {
		return
	}
	screen := call(gtk.Sym.GtkWidgetGetScreen, w.handle)
	visual := call(gtk.Sym.GdkScreenGetRGBAVisual, screen)
	composited := call(gtk.Sym.GdkScreenIsComposited, screen)&0xff != 0
	blur := style.Material != desktop.MaterialNone && compositorBlur(screen)
	w.effect = chooseEffect(style, composited, visual != 0, blur)
	if w.effect == effectOpaque {
		slog.Default().Debug("desktop/linux: window effect unavailable; using an opaque window",
			"window", w.id, "transparent", style.Transparent, "material", string(style.Material),
			"composited", composited, "rgba", visual != 0, "blur", blur)
		return
	}
	call(gtk.Sym.GtkWidgetSetVisual, w.handle, visual)
	background := "transparent"
	if w.effect == effectMaterial {
		background = "alpha(@theme_bg_color, " + materialTint[style.Material] + ")"
	}
	w.addWindowCSS("window { background-color: " + background + "; background-image: none; }")
	// WebKit paints an opaque white page background unless told otherwise.
	clear := new([4]float64) // GdkRGBA: red, green, blue, alpha
	var pin runtime.Pinner
	pin.Pin(clear)
	call(gtk.Sym.WebkitWebViewSetBackgroundColor, w.webview, uintptr(unsafe.Pointer(clear)))
	pin.Unpin()
}

// applyEffectAfterShow sets what needs the realized GdkWindow: the blur hint.
func (w *linuxWindow) applyEffectAfterShow() {
	if w.effect != effectMaterial {
		return
	}
	gdkWindow := call(gtk.Sym.GtkWidgetGetWindow, w.handle)
	if gdkWindow == 0 {
		return
	}
	withString(kdeBlurAtom, func(name uintptr) {
		withString("CARDINAL", func(kind uintptr) {
			property := call(gtk.Sym.GdkAtomInternStaticString, name)
			cardinal := call(gtk.Sym.GdkAtomInternStaticString, kind)
			// format 32, GDK_PROP_MODE_REPLACE, no data: blur the whole window.
			call(gtk.Sym.GdkPropertyChange, gdkWindow, property, cardinal, 32, 0, 0, 0)
		})
	})
}

// addWindowCSS attaches a style provider to this window's own style
// context, so it styles the window node alone, never its children or other
// windows. Application priority keeps it above the GTK theme.
func (w *linuxWindow) addWindowCSS(css string) {
	provider := call(gtk.Sym.GtkCSSProviderNew)
	if provider == 0 {
		return
	}
	withString(css, func(p uintptr) {
		call(gtk.Sym.GtkCSSProviderLoadFromData, provider, p, uintptr(len(css)), 0)
	})
	ctx := call(gtk.Sym.GtkWidgetGetStyleContext, w.handle)
	call(gtk.Sym.GtkStyleContextAddProvider, ctx, provider, 600 /* GTK_STYLE_PROVIDER_PRIORITY_APPLICATION */)
	call(gtk.Sym.GObjectUnref, provider)
}

// Page-started window drags. The runtime's desktop module posts
// {"type":"drag"} to window.webkit.messageHandlers.gofastr on a mousedown
// inside [data-cui-window-drag]. The message arrives after the press, from
// WebKit's web process, so GTK's current event is no longer that press.
// The shell keeps the last primary-button press the web view saw and starts
// the move with it, but only while that button is still held: a message
// with no live press (a script calling startDrag on its own, a late
// message after release) does nothing.

const (
	gdkButtonPress   = 4 // GDK_BUTTON_PRESS
	gdkButtonRelease = 7 // GDK_BUTTON_RELEASE
	dragMessage      = `{"type":"drag"}`
)

// eventFields receives gdk_event_get_button and gdk_event_get_root_coords.
type eventFields struct {
	button uint32
	_      uint32
	x, y   float64
}

type buttonPress struct {
	held bool
	x, y float64 // root coordinates
	time uint32
}

var buttonCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	value, ok := nativeWindows.Load(a.Int[2])
	if !ok {
		return 0
	}
	w := value.(*linuxWindow)
	event := a.Int[1]
	kind := call(gtk.Sym.GdkEventGetEventType, event)
	out := new(eventFields)
	var pin runtime.Pinner
	pin.Pin(out)
	defer pin.Unpin()
	if call(gtk.Sym.GdkEventGetButton, event, uintptr(unsafe.Pointer(&out.button)))&0xff == 0 || out.button != 1 {
		return 0
	}
	switch int32(kind) {
	case gdkButtonPress:
		call(gtk.Sym.GdkEventGetRootCoords, event, uintptr(unsafe.Pointer(&out.x)), uintptr(unsafe.Pointer(&out.y)))
		w.press = buttonPress{held: true, x: out.x, y: out.y, time: uint32(call(gtk.Sym.GdkEventGetTime, event))}
	case gdkButtonRelease:
		w.press.held = false
	}
	return 0 // let WebKit handle the event as well
})

var scriptMessageCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	value, ok := nativeWindows.Load(a.Int[2])
	if !ok {
		return 0
	}
	w := value.(*linuxWindow)
	jsValue := call(gtk.Sym.WebkitJavascriptResultGetJSValue, a.Int[1])
	if jsValue == 0 || call(gtk.Sym.JscValueIsString, jsValue)&0xff == 0 {
		return 0
	}
	text := call(gtk.Sym.JscValueToString, jsValue)
	body, _ := readBoundedString(text, 64)
	call(gtk.Sym.GFree, text)
	if body == dragMessage {
		w.startDragMain()
	}
	return 0
})

// startDragMain starts a window move from the held press. UI thread only.
func (w *linuxWindow) startDragMain() {
	if !w.press.held || w.closed.Load() {
		return
	}
	w.press.held = false // one drag per press
	call(gtk.Sym.GtkWindowBeginMoveDrag, w.handle, 1, uintptr(int(w.press.x)), uintptr(int(w.press.y)), uintptr(w.press.time))
}

// installDragHandler wires the message channel and the press tracking for
// one window. The handler name matches what the runtime posts to.
func (w *linuxWindow) installDragHandler() {
	manager := call(gtk.Sym.WebkitWebViewGetUCM, w.webview)
	withString("gofastr", func(p uintptr) { call(gtk.Sym.WebkitUCMRegisterScriptMessageHandler, manager, p) })
	connect(manager, "script-message-received::gofastr", scriptMessageCallback, w.handle)
	connect(w.webview, "button-press-event", buttonCallback, w.handle)
	connect(w.webview, "button-release-event", buttonCallback, w.handle)
}
